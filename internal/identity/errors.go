package identity

import "errors"

// Sentinel errors. Handlers map these to HTTP status codes; tests rely on
// errors.Is. New error types should be added here, not invented in each
// service or handler.
var (
	ErrInvalidCredentials = errors.New("identity: invalid email or password")
	ErrUserSuspended      = errors.New("identity: user account is suspended")
	ErrUserLocked         = errors.New("identity: user account is locked")
	ErrTenantNotFound     = errors.New("identity: tenant not found")
	ErrUserNotFound       = errors.New("identity: user not found")
	ErrSessionNotFound    = errors.New("identity: session not found")
	ErrSessionExpired     = errors.New("identity: session expired")
	ErrSessionRevoked     = errors.New("identity: session revoked")
	ErrMfaRequired        = errors.New("identity: MFA code required")
	ErrMfaInvalid         = errors.New("identity: MFA code invalid")
	ErrTotpReplay         = errors.New("identity: TOTP code already used")
	ErrMfaAlreadyEnabled  = errors.New("identity: MFA is already enabled")
	ErrMfaNotEnabled      = errors.New("identity: MFA is not enabled")
	ErrPasswordWeak       = errors.New("identity: password does not meet requirements")
	ErrResetTokenInvalid  = errors.New("identity: password reset token invalid or expired")
	ErrRateLimited        = errors.New("identity: rate limit exceeded")
)
