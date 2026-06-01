package repo

import (
	"context"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// MfaRepo is the Postgres-backed store for the user_mfa companion table
// (migration 0080): per-user hashed backup recovery codes plus the MFA
// activation timestamp. The TOTP seed itself stays on users.mfa_secret; this
// table holds only what the secret-on-users design lacked.
type MfaRepo struct {
	pool *database.Pool
}

// NewMfaRepo wires an MfaRepo against the supplied pool.
func NewMfaRepo(pool *database.Pool) *MfaRepo {
	return &MfaRepo{pool: pool}
}

// SetBackupCodes replaces the user's backup-code set with the supplied hashes
// and stamps generated_at + enabled_at. Upserts the row so the first call after
// enrollment creates it. Rides the caller's transaction.
func (r *MfaRepo) SetBackupCodes(ctx context.Context, q database.Querier, tenantID, userID uuid.UUID, hashes []string) error {
	_, err := q.Exec(ctx, `
		INSERT INTO user_mfa (user_id, tenant_id, backup_codes, generated_at, enabled_at, updated_at)
		VALUES ($1, $2, $3, now(), now(), now())
		ON CONFLICT (user_id) DO UPDATE
		SET backup_codes = EXCLUDED.backup_codes,
		    generated_at = now(),
		    enabled_at   = COALESCE(user_mfa.enabled_at, now()),
		    updated_at   = now()
	`, userID, tenantID, hashes)
	return err
}

// BackupCodes returns the user's current backup-code hashes (empty slice when
// the row is absent or holds no unused codes).
func (r *MfaRepo) BackupCodes(ctx context.Context, q database.Querier, userID uuid.UUID) ([]string, error) {
	var codes []string
	err := q.QueryRow(ctx,
		`SELECT backup_codes FROM user_mfa WHERE user_id = $1`, userID,
	).Scan(&codes)
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// RemainingCount returns how many unused backup codes the user has. Zero when
// the row is absent.
func (r *MfaRepo) RemainingCount(ctx context.Context, q database.Querier, userID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx,
		`SELECT COALESCE(array_length(backup_codes, 1), 0) FROM user_mfa WHERE user_id = $1`, userID,
	).Scan(&n)
	if err != nil {
		// Absent row: treat as zero rather than an error so callers don't have
		// to special-case "never enrolled backup codes".
		return 0, nil
	}
	return n, nil
}

// ConsumeBackupCode removes a single matched hash from the user's set,
// single-use. Rides the caller's transaction.
func (r *MfaRepo) ConsumeBackupCode(ctx context.Context, q database.Querier, userID uuid.UUID, hash string) error {
	_, err := q.Exec(ctx, `
		UPDATE user_mfa
		SET backup_codes = array_remove(backup_codes, $2),
		    updated_at = now()
		WHERE user_id = $1
	`, userID, hash)
	return err
}

// Clear removes the user's MFA companion row entirely. Called when MFA is
// disabled so no stale backup codes survive. Rides the caller's transaction.
func (r *MfaRepo) Clear(ctx context.Context, q database.Querier, userID uuid.UUID) error {
	_, err := q.Exec(ctx, `DELETE FROM user_mfa WHERE user_id = $1`, userID)
	return err
}
