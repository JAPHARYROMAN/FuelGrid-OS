package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
	"github.com/japharyroman/fuelgrid-os/internal/identity/ratelimit"
	"github.com/japharyroman/fuelgrid-os/internal/identity/repo"
	"github.com/japharyroman/fuelgrid-os/internal/identity/session"
	"github.com/japharyroman/fuelgrid-os/internal/identity/totp"
)

// ServiceConfig captures the knobs that affect authentication behavior.
// Values come from API config and are usually env-driven.
type ServiceConfig struct {
	SessionTTL          time.Duration
	LoginLockAfter      int
	LoginLockFor        time.Duration
	LoginRateMax        int64
	LoginRateWindow     time.Duration
	PasswordResetTTL    time.Duration
	PasswordResetPrefix string // Redis key prefix; defaults to "pwd_reset:"
}

// SafeDefaults returns reasonable defaults for missing values.
func (c ServiceConfig) SafeDefaults() ServiceConfig {
	if c.SessionTTL <= 0 {
		c.SessionTTL = 12 * time.Hour
	}
	if c.LoginLockAfter <= 0 {
		c.LoginLockAfter = 10
	}
	if c.LoginLockFor <= 0 {
		c.LoginLockFor = 30 * time.Minute
	}
	if c.LoginRateMax <= 0 {
		c.LoginRateMax = 5
	}
	if c.LoginRateWindow <= 0 {
		c.LoginRateWindow = 15 * time.Minute
	}
	if c.PasswordResetTTL <= 0 {
		c.PasswordResetTTL = 1 * time.Hour
	}
	if c.PasswordResetPrefix == "" {
		c.PasswordResetPrefix = "pwd_reset:"
	}
	return c
}

// Service is the high-level identity API consumed by HTTP handlers and
// (later) gRPC, CLIs, and background jobs.
type Service struct {
	cfg      ServiceConfig
	hasher   *password.Hasher
	users    *repo.UserRepo
	sessions *repo.SessionRepo
	store    session.Store
	limiter  *ratelimit.Limiter
	redis    *redis.Client
	logger   *slog.Logger
	now      func() time.Time
}

// NewService wires the identity service. Callers own the underlying
// dependencies and should close the Redis client / pool on shutdown.
func NewService(
	cfg ServiceConfig,
	hasher *password.Hasher,
	users *repo.UserRepo,
	sessions *repo.SessionRepo,
	store session.Store,
	limiter *ratelimit.Limiter,
	redisClient *redis.Client,
	logger *slog.Logger,
) *Service {
	return &Service{
		cfg:      cfg.SafeDefaults(),
		hasher:   hasher,
		users:    users,
		sessions: sessions,
		store:    store,
		limiter:  limiter,
		redis:    redisClient,
		logger:   logger,
		now:      time.Now,
	}
}

// LoginRequest carries all the inputs a login attempt needs.
type LoginRequest struct {
	TenantSlug string
	Email      string
	Password   string
	MfaCode    string // optional; required when the user has MFA enabled
	IP         string
	UserAgent  string
	DeviceID   *uuid.UUID
}

// LoginResult is what the HTTP handler returns to the client.
type LoginResult struct {
	Token       string
	Session     *session.Session
	MfaRequired bool
}

// Login validates credentials and issues a session. Failure modes are
// uniform — the caller cannot distinguish "user not found" from "bad
// password" so attackers can't enumerate users.
func (s *Service) Login(ctx context.Context, req LoginRequest) (*LoginResult, error) {
	rateBucket := "login:" + strings.ToLower(req.IP)
	if err := s.limiter.Allow(ctx, rateBucket, s.cfg.LoginRateMax, s.cfg.LoginRateWindow); err != nil {
		if errors.Is(err, ratelimit.ErrLimited) {
			return nil, ErrRateLimited
		}
		return nil, err
	}

	user, err := s.users.FindForLogin(ctx, req.TenantSlug, req.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if user.Status == "suspended" {
		return nil, ErrUserSuspended
	}
	if !user.IsActive() {
		return nil, ErrInvalidCredentials
	}
	if user.IsLocked(s.now()) {
		return nil, ErrUserLocked
	}
	if user.PasswordHash == nil {
		// Invited user without a set password.
		return nil, ErrInvalidCredentials
	}

	match, needsRehash, err := s.hasher.Verify(req.Password, *user.PasswordHash)
	if err != nil {
		return nil, err
	}
	if !match {
		count, mErr := s.users.MarkLoginFailure(ctx, user.ID, s.cfg.LoginLockAfter, s.cfg.LoginLockFor)
		if mErr != nil {
			s.logger.Error("mark login failure", "error", mErr, "user_id", user.ID)
		}
		s.logger.Info("audit",
			"event", "UserLoginFailed",
			"user_id", user.ID,
			"tenant_id", user.TenantID,
			"failure_count", count,
			"ip", req.IP,
		)
		return nil, ErrInvalidCredentials
	}

	if user.MfaEnabled {
		if req.MfaCode == "" {
			return &LoginResult{MfaRequired: true}, nil
		}
		if user.MfaSecret == nil || !totp.Verify(*user.MfaSecret, req.MfaCode, s.now()) {
			s.logger.Info("audit",
				"event", "UserMfaFailed",
				"user_id", user.ID,
				"tenant_id", user.TenantID,
				"ip", req.IP,
			)
			return nil, ErrMfaInvalid
		}
	}

	if needsRehash {
		// Fire-and-forget upgrade. We deliberately don't reuse the request
		// context: rehashing should outlive the response so that a slow
		// client disconnect doesn't strand a half-upgraded hash. The
		// 5-second budget is plenty for argon2id at our parameters.
		go func(uid uuid.UUID, pw string) { //nolint:gosec // G118: detach is intentional
			bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if newHash, err := s.hasher.Hash(pw); err == nil {
				_ = s.users.SetPassword(bg, uid, newHash)
			}
		}(user.ID, req.Password)
	}

	if err := s.users.MarkLoginSuccess(ctx, user.ID); err != nil {
		s.logger.Error("mark login success", "error", err, "user_id", user.ID)
	}
	_ = s.limiter.Reset(ctx, rateBucket)

	raw, hash, err := session.NewToken()
	if err != nil {
		return nil, err
	}
	expiresAt := s.now().Add(s.cfg.SessionTTL)

	sessID, err := s.sessions.Insert(ctx, hash, user.ID, user.TenantID, req.DeviceID, req.IP, req.UserAgent, expiresAt)
	if err != nil {
		return nil, err
	}

	sess := &session.Session{
		ID:           sessID,
		UserID:       user.ID,
		TenantID:     user.TenantID,
		DeviceID:     req.DeviceID,
		IP:           req.IP,
		UserAgent:    req.UserAgent,
		IssuedAt:     s.now(),
		LastSeenAt:   s.now(),
		ExpiresAt:    expiresAt,
		MfaSatisfied: user.MfaEnabled,
		RawToken:     raw,
	}
	if err := s.store.Put(ctx, raw, sess); err != nil {
		// Roll back the durable row so we don't have a phantom session.
		_ = s.sessions.Revoke(ctx, sessID, "redis put failed")
		return nil, err
	}

	s.logger.Info("audit",
		"event", "UserLoggedIn",
		"user_id", user.ID,
		"tenant_id", user.TenantID,
		"session_id", sessID,
		"mfa", user.MfaEnabled,
		"ip", req.IP,
	)

	return &LoginResult{Token: raw, Session: sess}, nil
}

// Logout revokes the session associated with the raw token. Missing
// sessions are a no-op so logout is safe to retry.
func (s *Service) Logout(ctx context.Context, rawToken string) error {
	sess, err := s.store.Get(ctx, rawToken)
	if err != nil {
		if errors.Is(err, session.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := s.store.Delete(ctx, rawToken); err != nil {
		return err
	}
	if err := s.sessions.Revoke(ctx, sess.ID, "logout"); err != nil {
		return err
	}
	s.logger.Info("audit",
		"event", "UserLoggedOut",
		"user_id", sess.UserID,
		"tenant_id", sess.TenantID,
		"session_id", sess.ID,
	)
	return nil
}

// Refresh extends a session's TTL in both Redis and Postgres.
func (s *Service) Refresh(ctx context.Context, rawToken string) (*session.Session, error) {
	sess, err := s.store.Get(ctx, rawToken)
	if err != nil {
		if errors.Is(err, session.ErrNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	newExpiry := s.now().Add(s.cfg.SessionTTL)
	sess.ExpiresAt = newExpiry
	sess.LastSeenAt = s.now()
	if err := s.store.Put(ctx, rawToken, sess); err != nil {
		return nil, err
	}
	if err := s.sessions.TouchExpiry(ctx, sess.ID, newExpiry); err != nil {
		return nil, err
	}
	return sess, nil
}

// Resolve looks up an active session by raw token. Used by the auth
// middleware on every protected request. The hot path is a single Redis
// GET; nothing in Postgres is touched.
func (s *Service) Resolve(ctx context.Context, rawToken string) (*session.Session, error) {
	sess, err := s.store.Get(ctx, rawToken)
	if err != nil {
		if errors.Is(err, session.ErrNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if s.now().After(sess.ExpiresAt) {
		_ = s.store.Delete(ctx, rawToken)
		return nil, ErrSessionExpired
	}
	return sess, nil
}

// ChangePassword updates the password for an already-authenticated user.
// Verifies the old password first so a stolen session can't pivot to
// account takeover by setting a fresh password.
func (s *Service) ChangePassword(ctx context.Context, tenantID, userID uuid.UUID, oldPassword, newPassword string) error {
	if len(newPassword) < 12 {
		return ErrPasswordWeak
	}
	user, err := s.users.FindByID(ctx, tenantID, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		return err
	}
	if user.PasswordHash != nil {
		match, _, err := s.hasher.Verify(oldPassword, *user.PasswordHash)
		if err != nil {
			return err
		}
		if !match {
			return ErrInvalidCredentials
		}
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	if err := s.users.SetPassword(ctx, userID, hash); err != nil {
		return err
	}
	s.logger.Info("audit",
		"event", "UserPasswordChanged",
		"user_id", userID,
		"tenant_id", tenantID,
	)
	return nil
}

// RequestPasswordReset issues a one-time reset token and stores it in
// Redis keyed by token hash. The caller is responsible for delivering
// the token to the user (email in production, log line in dev).
//
// Always returns nil for "user not found" so the endpoint can't be used
// to enumerate accounts.
func (s *Service) RequestPasswordReset(ctx context.Context, tenantSlug, email string) (token string, delivered bool, err error) {
	user, err := s.users.FindByEmail(ctx, tenantSlug, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}

	raw, hash, err := session.NewToken()
	if err != nil {
		return "", false, err
	}
	key := s.cfg.PasswordResetPrefix + base64Hash(hash)
	if err := s.redis.Set(ctx, key, user.ID.String(), s.cfg.PasswordResetTTL).Err(); err != nil {
		return "", false, err
	}
	s.logger.Info("audit",
		"event", "UserPasswordResetRequested",
		"user_id", user.ID,
		"tenant_id", user.TenantID,
	)
	return raw, true, nil
}

// ConfirmPasswordReset trades a reset token for a fresh password.
func (s *Service) ConfirmPasswordReset(ctx context.Context, token, newPassword string) error {
	if len(newPassword) < 12 {
		return ErrPasswordWeak
	}
	hash := session.HashToken(token)
	key := s.cfg.PasswordResetPrefix + base64Hash(hash)

	userIDStr, err := s.redis.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return ErrResetTokenInvalid
	}
	if err != nil {
		return err
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return ErrResetTokenInvalid
	}
	pwHash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	if err := s.users.SetPassword(ctx, userID, pwHash); err != nil {
		return err
	}
	if err := s.sessions.RevokeAllForUser(ctx, userID, "password reset"); err != nil {
		s.logger.Error("revoke sessions after reset", "error", err, "user_id", userID)
	}
	_ = s.redis.Del(ctx, key).Err()

	s.logger.Info("audit",
		"event", "UserPasswordReset",
		"user_id", userID,
	)
	return nil
}

// EnrollMfa generates a TOTP secret for the user, stores it disabled, and
// returns the otpauth URL clients should render as a QR code.
func (s *Service) EnrollMfa(ctx context.Context, userID uuid.UUID, tenantID uuid.UUID) (*totp.Enrollment, error) {
	user, err := s.users.FindByID(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	if user.MfaEnabled {
		return nil, ErrMfaAlreadyEnabled
	}
	e, err := totp.Enroll(user.Email)
	if err != nil {
		return nil, err
	}
	if err := s.users.EnrollMfa(ctx, userID, e.Secret); err != nil {
		return nil, err
	}
	s.logger.Info("audit",
		"event", "UserMfaEnrolled",
		"user_id", userID,
		"tenant_id", tenantID,
	)
	return &e, nil
}

// VerifyMfa flips mfa_enabled to true after the user proves they hold
// the freshly enrolled secret.
func (s *Service) VerifyMfa(ctx context.Context, userID, tenantID uuid.UUID, code string) error {
	user, err := s.users.FindByID(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	if user.MfaSecret == nil {
		return errors.New("identity: no MFA secret enrolled")
	}
	if !totp.Verify(*user.MfaSecret, code, s.now()) {
		return ErrMfaInvalid
	}
	if err := s.users.ActivateMfa(ctx, userID); err != nil {
		return err
	}
	s.logger.Info("audit",
		"event", "UserMfaActivated",
		"user_id", userID,
		"tenant_id", tenantID,
	)
	return nil
}

// base64Hash returns the URL-safe base64 form of a token hash, suitable
// for use as a Redis key.
func base64Hash(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
