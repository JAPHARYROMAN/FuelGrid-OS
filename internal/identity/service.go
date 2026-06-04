package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
	"github.com/japharyroman/fuelgrid-os/internal/identity/ratelimit"
	"github.com/japharyroman/fuelgrid-os/internal/identity/repo"
	"github.com/japharyroman/fuelgrid-os/internal/identity/secretcrypto"
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
	cfg       ServiceConfig
	pool      *database.Pool
	hasher    *password.Hasher
	users     *repo.UserRepo
	mfa       *repo.MfaRepo
	sessions  *repo.SessionRepo
	store     session.Store
	limiter   *ratelimit.Limiter
	redis     *redis.Client
	totpGuard *totp.Guard
	logger    *slog.Logger
	mfaCipher *secretcrypto.Cipher
	now       func() time.Time
}

// redisConsumeStore adapts *redis.Client to totp.ConsumeStore. go-redis's
// SetNX returns a *BoolCmd; the guard wants the (bool, error) pair directly so
// it stays decoupled from go-redis (and trivially fakeable in tests).
type redisConsumeStore struct{ client *redis.Client }

func (r redisConsumeStore) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	return r.client.SetNX(ctx, key, value, ttl).Result()
}

// NewService wires the identity service. Callers own the underlying
// dependencies and should close the Redis client / pool on shutdown.
//
// The pool is used directly (rather than only via the repos) so the
// service can open transactions that wrap a state change together with
// its audit_logs + outbox_events rows — the Stage-7 durability pattern,
// now applied to auth events too.
func NewService(
	cfg ServiceConfig,
	pool *database.Pool,
	hasher *password.Hasher,
	users *repo.UserRepo,
	sessions *repo.SessionRepo,
	store session.Store,
	limiter *ratelimit.Limiter,
	redisClient *redis.Client,
	logger *slog.Logger,
	authPepper string,
) *Service {
	return &Service{
		cfg:       cfg.SafeDefaults(),
		pool:      pool,
		hasher:    hasher,
		users:     users,
		mfa:       repo.NewMfaRepo(pool),
		sessions:  sessions,
		store:     store,
		limiter:   limiter,
		redis:     redisClient,
		totpGuard: totp.NewGuard(redisConsumeStore{client: redisClient}, "totp_used:"),
		logger:    logger,
		mfaCipher: secretcrypto.New(authPepper),
		now:       time.Now,
	}
}

// auditAuth writes an audit_logs row + outbox_events row for an auth
// event inside tx. Keeps every call site to a single line.
func (s *Service) auditAuth(ctx context.Context, tx pgx.Tx, tenantID, actorID uuid.UUID, action, eventType string, payload any) error {
	return audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID:      tenantID,
		ActorID:       actorID,
		Action:        action,
		EventType:     eventType,
		EntityType:    "user",
		AggregateType: "user",
		EntityID:      actorID.String(),
		NewValue:      payload,
	})
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
	// Rate-limit by source IP and, independently, by the targeted account
	// (tenant + email). The IP bucket caps a single origin; the per-account
	// bucket caps a distributed credential-stuffing attack on one account that
	// rotates IPs — neither alone covers the other (AUTH-09).
	rateBuckets := []string{
		"login:ip:" + strings.ToLower(req.IP),
		"login:acct:" + strings.ToLower(req.TenantSlug) + ":" + strings.ToLower(req.Email),
	}
	for _, bucket := range rateBuckets {
		if err := s.limiter.Allow(ctx, bucket, s.cfg.LoginRateMax, s.cfg.LoginRateWindow); err != nil {
			if errors.Is(err, ratelimit.ErrLimited) {
				return nil, ErrRateLimited
			}
			return nil, err
		}
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
		// Failed attempt mutates state (failed_login_count, maybe
		// locked_until) so it rides a tx with its audit + outbox rows.
		if err := s.inTx(ctx, func(tx pgx.Tx) error {
			count, err := s.users.MarkLoginFailure(ctx, tx, user.ID, s.cfg.LoginLockAfter, s.cfg.LoginLockFor)
			if err != nil {
				return err
			}
			return s.auditAuth(ctx, tx, user.TenantID, user.ID,
				"user.login_failed", "UserLoginFailed",
				map[string]any{"failure_count": count, "ip": req.IP})
		}); err != nil {
			s.logger.Error("record login failure", "error", err, "user_id", user.ID)
		}
		return nil, ErrInvalidCredentials
	}

	if user.MfaEnabled {
		if req.MfaCode == "" {
			return &LoginResult{MfaRequired: true}, nil
		}
		// Replay-guarded TOTP check: a cryptographically valid code is consumed
		// (single-use) so a re-presentation within its acceptance window fails.
		totpOK := false
		if user.MfaSecret != nil {
			switch err := s.checkTOTP(ctx, user.ID, *user.MfaSecret, req.MfaCode); {
			case err == nil:
				totpOK = true
			case errors.Is(err, ErrTotpReplay):
				// A valid-but-reused code: do NOT fall through to the backup-code
				// path (it isn't a backup code) and do NOT count it as a generic
				// MFA failure — surface the replay distinctly so the handler can
				// 401 without burning a lockout slot on a benign double-submit.
				return nil, ErrTotpReplay
			case errors.Is(err, ErrMfaInvalid):
				// Not a valid TOTP code — fall through to the backup-code path.
			default:
				// Store/decrypt failure: fail closed.
				return nil, err
			}
		}
		// Fall back to a one-time backup recovery code when the TOTP code
		// doesn't match — the same mfa_code field carries either. A matched
		// backup code is consumed (single-use) inside the success tx below.
		backupOK := false
		if !totpOK {
			if ok, cerr := s.consumeBackupCode(ctx, user.ID, req.MfaCode); cerr != nil {
				return nil, cerr
			} else {
				backupOK = ok
			}
		}
		if !totpOK && !backupOK {
			// A bad MFA code is a failed authentication: count it toward the
			// account lockout (AUTH-10) so the second factor can't be
			// brute-forced once the password is known — the per-IP login rate
			// limit alone is evadable by rotating source IPs. Rides a tx with
			// its audit + outbox rows, mirroring a bad password. A successful
			// login clears the counter (MarkLoginSuccess), so an occasional
			// fumble before a correct code does not accumulate.
			if err := s.inTx(ctx, func(tx pgx.Tx) error {
				count, ferr := s.users.MarkLoginFailure(ctx, tx, user.ID, s.cfg.LoginLockAfter, s.cfg.LoginLockFor)
				if ferr != nil {
					return ferr
				}
				return s.auditAuth(ctx, tx, user.TenantID, user.ID,
					"user.mfa_failed", "UserMfaFailed",
					map[string]any{"failure_count": count, "ip": req.IP})
			}); err != nil {
				s.logger.Error("record mfa failure", "error", err, "user_id", user.ID)
			}
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
				_ = s.users.SetPassword(bg, s.pool, uid, newHash)
			}
		}(user.ID, req.Password)
	}

	raw, hash, err := session.NewToken()
	if err != nil {
		return nil, err
	}
	expiresAt := s.now().Add(s.cfg.SessionTTL)

	// One transaction: clear failure counters, insert the durable session
	// row, and write the login audit + outbox event. Either all three
	// commit or none do — no half-issued logins, no lost audit.
	var sessID uuid.UUID
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		if err := s.users.MarkLoginSuccess(ctx, tx, user.ID); err != nil {
			return err
		}
		id, err := s.sessions.Insert(ctx, tx, hash, user.ID, user.TenantID, req.DeviceID, req.IP, req.UserAgent, expiresAt)
		if err != nil {
			return err
		}
		sessID = id
		return s.auditAuth(ctx, tx, user.TenantID, user.ID,
			"user.logged_in", "UserLoggedIn",
			map[string]any{"session_id": id, "mfa": user.MfaEnabled, "ip": req.IP})
	})
	if err != nil {
		return nil, err
	}
	for _, bucket := range rateBuckets {
		_ = s.limiter.Reset(ctx, bucket)
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
		// Stamp the user's current epoch so Resolve can authoritatively
		// detect a later global revoke (SEC-1 / AUTH-04).
		SessionEpoch: user.SessionEpoch,
		RawToken:     raw,
	}
	if err := s.store.Put(ctx, raw, sess); err != nil {
		// Roll back the durable row so we don't have a phantom session.
		_ = s.sessions.Revoke(ctx, s.pool, sessID, "redis put failed")
		return nil, err
	}

	return &LoginResult{Token: raw, Session: sess}, nil
}

// inTx runs fn inside a transaction, committing on success and rolling
// back on error. The single place the identity service opens
// transactions.
func (s *Service) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if err := s.sessions.Revoke(ctx, tx, sess.ID, "logout"); err != nil {
			return err
		}
		return s.auditAuth(ctx, tx, sess.TenantID, sess.UserID,
			"user.logged_out", "UserLoggedOut",
			map[string]any{"session_id": sess.ID})
	})
}

// RevokeSession revokes a single session the caller owns, by its UUID.
// It is the authoritative path the profile "revoke this device" button
// uses: it deletes the live Redis entry (so the session stops resolving
// immediately) AND marks the durable row revoked. Ownership is enforced
// against ownerUserID so one user can't kill another's session.
//
// Returns ErrSessionNotFound when the session doesn't exist, isn't
// active, or doesn't belong to ownerUserID.
func (s *Service) RevokeSession(ctx context.Context, ownerUserID, sessionID uuid.UUID) error {
	row, err := s.sessions.FindActiveOwnedBy(ctx, sessionID, ownerUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSessionNotFound
		}
		return err
	}
	// Kill the hot-path entry first so the session stops authenticating
	// even if the durable update lags or fails.
	if err := s.store.DeleteByID(ctx, sessionID); err != nil {
		return err
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if err := s.sessions.Revoke(ctx, tx, row.ID, "self-revoke"); err != nil {
			return err
		}
		return s.auditAuth(ctx, tx, row.TenantID, ownerUserID,
			"user.session_revoked", "UserSessionRevoked",
			map[string]any{"session_id": row.ID})
	})
}

// revokeAllUserSessions deletes every active session for a user from
// Redis (by id) and marks them revoked in Postgres. Used after a
// password reset so a leaked-credential attacker is logged out
// everywhere, not just from sessions whose TTL happens to lapse.
func (s *Service) revokeAllUserSessions(ctx context.Context, tx pgx.Tx, userID uuid.UUID, reason string) error {
	rows, err := s.sessions.ListActiveForUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := s.store.DeleteByID(ctx, row.ID); err != nil {
			// Best-effort per session; keep going so one Redis miss
			// doesn't strand the rest.
			s.logger.Warn("revoke session redis", "error", err, "session_id", row.ID)
		}
	}
	// Bump the user's session epoch inside the caller's tx. This is the
	// authoritative revocation (SEC-1 / AUTH-04): any session minted with the
	// old epoch — including ones whose best-effort Redis delete above missed —
	// fails the epoch check in Resolve. The durable RevokeAllForUser keeps the
	// sessions table's audit trail accurate.
	if err := s.users.BumpSessionEpoch(ctx, tx, userID); err != nil {
		return err
	}
	return s.sessions.RevokeAllForUser(ctx, tx, userID, reason)
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
// middleware on every protected request. After the hot-path Redis GET it
// makes one authoritative check in Postgres: the session's stamped
// session_epoch against the user's current epoch. A global revoke (password
// reset/change, "log out everywhere") bumps the user's epoch, so a stale
// session whose Redis entry was never cleaned up still fails this check and
// is treated as revoked (SEC-1 / AUTH-04).
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
	// Authoritative revocation check: the user's current epoch is the source
	// of truth. If it has moved past what the session was minted with, the
	// session has been globally revoked — drop the stale Redis entry and
	// reject, even though Redis still held it.
	epoch, err := s.users.CurrentSessionEpoch(ctx, s.pool, sess.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// User no longer exists: the session can't be valid.
			_ = s.store.Delete(ctx, rawToken)
			return nil, ErrSessionRevoked
		}
		return nil, err
	}
	if epoch != sess.SessionEpoch {
		_ = s.store.Delete(ctx, rawToken)
		return nil, ErrSessionRevoked
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
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if err := s.users.SetPassword(ctx, tx, userID, hash); err != nil {
			return err
		}
		// A password change invalidates every existing session: bump the
		// epoch (authoritative) and clear the live Redis entries + durable
		// rows, mirroring the reset flow (SEC-1 / AUTH-04).
		if err := s.revokeAllUserSessions(ctx, tx, userID, "password change"); err != nil {
			return err
		}
		return s.auditAuth(ctx, tx, tenantID, userID,
			"user.password_changed", "UserPasswordChanged", nil)
	})
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
	// The token lives in Redis; the audit + outbox row records that a
	// reset was requested (no Postgres state change beyond the audit).
	if err := s.inTx(ctx, func(tx pgx.Tx) error {
		return s.auditAuth(ctx, tx, user.TenantID, user.ID,
			"user.password_reset_requested", "UserPasswordResetRequested", nil)
	}); err != nil {
		s.logger.Error("audit password reset request", "error", err, "user_id", user.ID)
	}
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

	// We need the user's tenant for the audit row. Resolve it before the
	// tx; FindActiveOwnedBy isn't applicable here (no session yet).
	tenantID, err := s.users.TenantOf(ctx, userID)
	if err != nil {
		return err
	}

	// One transaction: set the new password, revoke all the user's
	// sessions, and write the audit + outbox row. The Redis side of
	// revocation (DeleteByID per session) runs inside revokeAllUserSessions
	// and is best-effort; the durable revoke + audit are atomic.
	if err := s.inTx(ctx, func(tx pgx.Tx) error {
		if err := s.users.SetPassword(ctx, tx, userID, pwHash); err != nil {
			return err
		}
		if err := s.revokeAllUserSessions(ctx, tx, userID, "password reset"); err != nil {
			return err
		}
		return s.auditAuth(ctx, tx, tenantID, userID,
			"user.password_reset", "UserPasswordReset", nil)
	}); err != nil {
		return err
	}
	_ = s.redis.Del(ctx, key).Err()
	return nil
}

// verifyTOTP decrypts the stored MFA secret (transparently passing through a
// legacy plaintext secret written before AUTH-13) and checks the code against
// it at the current time. A decryption failure is treated as a verification
// failure — a tampered or unreadable secret never authenticates.
//
// This is the pure cryptographic check with NO replay protection. Use
// checkTOTP on any authentication path that should reject a re-used code; this
// bare form is reserved for the one-shot enrollment/disable flows where the
// secret is being proven, not used to grant a session.
func (s *Service) verifyTOTP(stored, code string) bool {
	secret, err := s.mfaCipher.Decrypt(stored)
	if err != nil {
		return false
	}
	return totp.Verify(secret, code, s.now())
}

// checkTOTP is the replay-guarded TOTP check for the login hot path. It first
// runs the stateless cryptographic Verify; only on a cryptographic match does
// it atomically consume the code (set-if-not-exists in the session store) so a
// second presentation of the same code within its acceptance window is
// rejected. A shoulder-surfed or phished-then-relayed code is valid for the
// full window (period ± skew); single-use consumption closes that replay gap
// (W1-SEC-TOTP).
//
// Returns:
//   - nil               on a fresh, valid code (caller may authenticate);
//   - ErrMfaInvalid     when the code does not match the secret;
//   - ErrTotpReplay     when the code is valid but already consumed;
//   - the store error   when the consume cannot be confirmed (fail closed —
//     we never grant a session without a guarantee of single use).
func (s *Service) checkTOTP(ctx context.Context, userID uuid.UUID, stored, code string) error {
	secret, err := s.mfaCipher.Decrypt(stored)
	if err != nil {
		return ErrMfaInvalid
	}
	if !totp.Verify(secret, code, s.now()) {
		return ErrMfaInvalid
	}
	if err := s.totpGuard.Consume(ctx, userID, code); err != nil {
		if errors.Is(err, totp.ErrReplay) {
			return ErrTotpReplay
		}
		return err
	}
	return nil
}

// EnrollMfa generates a TOTP secret for the user, stores it (encrypted at rest)
// disabled, and returns the otpauth URL clients should render as a QR code.
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
	enc, err := s.mfaCipher.Encrypt(e.Secret)
	if err != nil {
		return nil, err
	}
	if err := s.inTx(ctx, func(tx pgx.Tx) error {
		if err := s.users.EnrollMfa(ctx, tx, userID, enc); err != nil {
			return err
		}
		return s.auditAuth(ctx, tx, tenantID, userID,
			"user.mfa_enrolled", "UserMfaEnrolled", nil)
	}); err != nil {
		return nil, err
	}
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
	if !s.verifyTOTP(*user.MfaSecret, code) {
		return ErrMfaInvalid
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if err := s.users.ActivateMfa(ctx, tx, userID); err != nil {
			return err
		}
		return s.auditAuth(ctx, tx, tenantID, userID,
			"user.mfa_activated", "UserMfaActivated", nil)
	})
}

// ConfirmEnroll verifies a freshly enrolled TOTP code, flips mfa_enabled to
// true, and issues the first set of one-time backup recovery codes. The
// plaintext codes are returned to the caller exactly once (only their hashes
// are stored) — the UI must surface them immediately and never again.
func (s *Service) ConfirmEnroll(ctx context.Context, userID, tenantID uuid.UUID, code string) ([]string, error) {
	user, err := s.users.FindByID(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	if user.MfaSecret == nil {
		return nil, errors.New("identity: no MFA secret enrolled")
	}
	if !s.verifyTOTP(*user.MfaSecret, code) {
		return nil, ErrMfaInvalid
	}
	plain, hashes, err := s.newBackupCodes()
	if err != nil {
		return nil, err
	}
	if err := s.inTx(ctx, func(tx pgx.Tx) error {
		if err := s.users.ActivateMfa(ctx, tx, userID); err != nil {
			return err
		}
		if err := s.mfa.SetBackupCodes(ctx, tx, tenantID, userID, hashes); err != nil {
			return err
		}
		return s.auditAuth(ctx, tx, tenantID, userID,
			"user.mfa_activated", "UserMfaActivated", nil)
	}); err != nil {
		return nil, err
	}
	return plain, nil
}

// DisableMfa turns MFA off: clears the stored secret, flips mfa_enabled to
// false, and removes the backup-code companion row. A current TOTP or backup
// code is required so a hijacked session can't silently strip the second
// factor.
func (s *Service) DisableMfa(ctx context.Context, userID, tenantID uuid.UUID, code string) error {
	user, err := s.users.FindByID(ctx, tenantID, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		return err
	}
	if !user.MfaEnabled {
		return ErrMfaNotEnabled
	}
	totpOK := user.MfaSecret != nil && s.verifyTOTP(*user.MfaSecret, code)
	if !totpOK {
		ok, cerr := s.consumeBackupCode(ctx, userID, code)
		if cerr != nil {
			return cerr
		}
		if !ok {
			return ErrMfaInvalid
		}
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if err := s.users.DisableMfa(ctx, tx, userID); err != nil {
			return err
		}
		if err := s.mfa.Clear(ctx, tx, userID); err != nil {
			return err
		}
		return s.auditAuth(ctx, tx, tenantID, userID,
			"user.mfa_disabled", "UserMfaDisabled", nil)
	})
}

// RegenerateBackupCodes issues a fresh set of one-time backup codes, replacing
// any existing set, and returns the plaintext codes once. MFA must be enabled.
func (s *Service) RegenerateBackupCodes(ctx context.Context, userID, tenantID uuid.UUID) ([]string, error) {
	user, err := s.users.FindByID(ctx, tenantID, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	if !user.MfaEnabled {
		return nil, ErrMfaNotEnabled
	}
	plain, hashes, err := s.newBackupCodes()
	if err != nil {
		return nil, err
	}
	if err := s.inTx(ctx, func(tx pgx.Tx) error {
		if err := s.mfa.SetBackupCodes(ctx, tx, tenantID, userID, hashes); err != nil {
			return err
		}
		return s.auditAuth(ctx, tx, tenantID, userID,
			"user.mfa_backup_codes_regenerated", "UserMfaBackupCodesRegenerated", nil)
	}); err != nil {
		return nil, err
	}
	return plain, nil
}

// RemainingBackupCodes returns how many unused backup codes the user has, for
// the profile UI to nudge a regenerate when the set runs low.
func (s *Service) RemainingBackupCodes(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.mfa.RemainingCount(ctx, s.pool, userID)
}

// MfaStatus is the per-user MFA snapshot the profile + /me surfaces consume.
type MfaStatus struct {
	Enabled              bool // a second factor is active on the account
	RequiredByRole       bool // the user's roles make MFA mandatory
	BackupCodesRemaining int  // unused recovery codes
}

// MfaState reports the actor's MFA status: whether it is enabled, whether their
// roles make it mandatory (admin/finance), and how many backup codes remain.
// The UI uses RequiredByRole && !Enabled to force enrollment.
func (s *Service) MfaState(ctx context.Context, tenantID, userID uuid.UUID) (MfaStatus, error) {
	user, err := s.users.FindByID(ctx, tenantID, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MfaStatus{}, ErrUserNotFound
		}
		return MfaStatus{}, err
	}
	roles, err := s.users.ListRoles(ctx, userID)
	if err != nil {
		return MfaStatus{}, err
	}
	remaining, err := s.mfa.RemainingCount(ctx, s.pool, userID)
	if err != nil {
		return MfaStatus{}, err
	}
	return MfaStatus{
		Enabled:              user.MfaEnabled,
		RequiredByRole:       RoleRequiresMfa(roles),
		BackupCodesRemaining: remaining,
	}, nil
}

// newBackupCodes generates a fresh set of human-typeable single-use codes,
// returning the plaintext set (shown once) alongside their Argon2id hashes (the
// only form persisted).
func (s *Service) newBackupCodes() (plain []string, hashes []string, err error) {
	const count = 10
	plain = make([]string, 0, count)
	hashes = make([]string, 0, count)
	for i := 0; i < count; i++ {
		code, err := backupCode()
		if err != nil {
			return nil, nil, err
		}
		h, err := s.hasher.Hash(normalizeBackupCode(code))
		if err != nil {
			return nil, nil, err
		}
		plain = append(plain, code)
		hashes = append(hashes, h)
	}
	return plain, hashes, nil
}

// consumeBackupCode checks code against the user's stored backup-code hashes
// and, on a match, removes that hash (single-use). Returns (true, nil) when a
// code was consumed. A user without a companion row simply yields (false, nil).
func (s *Service) consumeBackupCode(ctx context.Context, userID uuid.UUID, code string) (bool, error) {
	candidate := normalizeBackupCode(code)
	if candidate == "" {
		return false, nil
	}
	hashes, err := s.mfa.BackupCodes(ctx, s.pool, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	for _, h := range hashes {
		match, _, verr := s.hasher.Verify(candidate, h)
		if verr != nil {
			continue
		}
		if match {
			if err := s.inTx(ctx, func(tx pgx.Tx) error {
				return s.mfa.ConsumeBackupCode(ctx, tx, userID, h)
			}); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	return false, nil
}

// IssueResetToken mints a password-reset token for a known user id and
// stores it in Redis under the same key scheme as RequestPasswordReset.
// Used by tenant provisioning so a freshly created admin can set their
// initial password via the normal /auth/password-reset/confirm flow.
func (s *Service) IssueResetToken(ctx context.Context, userID uuid.UUID) (string, error) {
	raw, hash, err := session.NewToken()
	if err != nil {
		return "", err
	}
	key := s.cfg.PasswordResetPrefix + base64Hash(hash)
	if err := s.redis.Set(ctx, key, userID.String(), s.cfg.PasswordResetTTL).Err(); err != nil {
		return "", err
	}
	return raw, nil
}

// base64Hash returns the URL-safe base64 form of a token hash, suitable
// for use as a Redis key.
func base64Hash(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// backupCodeAlphabet excludes ambiguous glyphs (0/O, 1/I/L) so a code copied
// from the one-time display is unambiguous to re-type.
const backupCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// backupCode returns a fresh ~50-bit recovery code formatted as XXXX-XXXX for
// readability. The hyphen is cosmetic — normalizeBackupCode strips it before
// hash/verify so the user may type it with or without.
func backupCode() (string, error) {
	const n = 10
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, 0, n+1)
	for i, b := range buf {
		if i == n/2 {
			out = append(out, '-')
		}
		out = append(out, backupCodeAlphabet[int(b)%len(backupCodeAlphabet)])
	}
	return string(out), nil
}

// normalizeBackupCode canonicalizes a user-entered code for matching: strip
// whitespace and hyphens, upper-case. Keeps "abcd efgh", "ABCD-EFGH" and
// "abcdefgh" equivalent.
func normalizeBackupCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(code)) {
		if r == '-' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// rolesRequiringMfa is the set of role codes for which MFA is mandatory: any
// principal holding one of these must have MFA enabled (AUTH MFA policy). The
// privileged tenant roles — full admin and finance — gate the most sensitive
// surfaces (user management, the ledger, payouts), so a second factor is not
// optional for them. The HTTP requireMFASatisfied middleware enforces this on
// the admin-console routes (SR-M1).
//
// The canonical FuelGrid system role codes are seeded in migration 0004
// (system_admin, finance_officer, …). The remaining entries are forward-
// compatible aliases for tenant role taxonomies that map to the same
// admin/finance privilege tier, so renaming or adding such a role does not
// silently drop the MFA requirement.
var rolesRequiringMfa = map[string]bool{
	// Canonical seeded system roles.
	"system_admin":    true, // full tenant admin
	"finance_officer": true, // finance: cash reconciliation, invoices, supplier bills
	// Forward-compatible aliases for the admin/finance privilege tier.
	"tenant_admin": true,
	"admin":        true,
	"finance":      true,
	"finance_lead": true,
	"accountant":   true,
}

// RoleRequiresMfa reports whether any of the supplied role codes makes MFA
// mandatory. The HTTP layer enforces this via the requireMFASatisfied
// middleware (using MfaState's RequiredByRole, which calls this): a session
// whose role requires MFA but has MfaSatisfied=false is refused (403
// mfa_required) on the sensitive admin-console routes (SR-M1).
func RoleRequiresMfa(roleCodes []string) bool {
	for _, c := range roleCodes {
		if rolesRequiringMfa[strings.ToLower(c)] {
			return true
		}
	}
	return false
}
