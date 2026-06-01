// Package notifications is the in-app notification feed: a per-user (or
// tenant-wide) message store written by the event subscriber when a domain
// event of interest fires, and read back by the authenticated owner via the
// /notifications API and the topbar bell.
package notifications

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// ErrNotFound is returned when a notification does not exist in the caller's
// tenant (or is not visible to the caller).
var ErrNotFound = errors.New("notifications: not found")

// Severity levels mirror the DB CHECK constraint and the UI Badge variants.
const (
	SeverityInfo     = "info"
	SeveritySuccess  = "success"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// Notification is one feed entry. UserID is nil for a tenant-wide notification
// (every user in the tenant sees it); set for a private one. ReadAt is nil
// until the owner marks it read.
type Notification struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	UserID            *uuid.UUID
	Type              string
	Title             string
	Body              string
	Severity          string
	RelatedEntityType *string
	RelatedEntityID   *string
	ReadAt            *time.Time
	CreatedAt         time.Time
}

// CreateInput holds the fields needed to raise a notification.
type CreateInput struct {
	UserID            *uuid.UUID
	Type              string
	Title             string
	Body              string
	Severity          string
	RelatedEntityType *string
	RelatedEntityID   *string
}

// Repo is the notifications data layer.
type Repo struct{ pool *database.Pool }

// New constructs the notifications repository.
func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const notificationColumns = `id, tenant_id, user_id, type, title, body, severity,
	related_entity_type, related_entity_id, read_at, created_at`

func scanNotification(row pgx.Row) (Notification, error) {
	var n Notification
	err := row.Scan(&n.ID, &n.TenantID, &n.UserID, &n.Type, &n.Title, &n.Body,
		&n.Severity, &n.RelatedEntityType, &n.RelatedEntityID, &n.ReadAt, &n.CreatedAt)
	return n, err
}

// Create inserts a notification and returns it. Severity defaults to "info"
// when blank. A nil UserID makes the notification tenant-wide.
func (r *Repo) Create(ctx context.Context, tenantID uuid.UUID, in CreateInput) (Notification, error) {
	severity := in.Severity
	if severity == "" {
		severity = SeverityInfo
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO notifications
			(tenant_id, user_id, type, title, body, severity, related_entity_type, related_entity_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+notificationColumns,
		tenantID, in.UserID, in.Type, in.Title, in.Body, severity,
		in.RelatedEntityType, in.RelatedEntityID)
	return scanNotification(row)
}

// ListForUser returns the caller's feed: their own notifications plus the
// tenant-wide ones (user_id IS NULL), newest first. When unreadOnly is true
// only unread rows are returned. The window is the [offset, offset+limit) page;
// callers pass limit+1 to detect a further page (the server trims and sets
// has_more).
func (r *Repo) ListForUser(ctx context.Context, tenantID, userID uuid.UUID, unreadOnly bool, limit, offset int) ([]Notification, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+notificationColumns+`
		FROM notifications
		WHERE tenant_id = $1
		  AND (user_id = $2 OR user_id IS NULL)
		  AND ($3 = false OR read_at IS NULL)
		ORDER BY created_at DESC, id DESC
		LIMIT $4 OFFSET $5`,
		tenantID, userID, unreadOnly, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// UnreadCount returns how many unread notifications are visible to the user
// (their own + tenant-wide).
func (r *Repo) UnreadCount(ctx context.Context, tenantID, userID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM notifications
		WHERE tenant_id = $1
		  AND (user_id = $2 OR user_id IS NULL)
		  AND read_at IS NULL`,
		tenantID, userID).Scan(&n)
	return n, err
}

// MarkRead marks a single notification read for the user. It is idempotent
// (already-read rows are left untouched) and scoped so a user can only mark
// rows they can see — their own or tenant-wide. Returns ErrNotFound when no
// matching row exists.
func (r *Repo) MarkRead(ctx context.Context, tenantID, userID, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE notifications
		SET read_at = now()
		WHERE tenant_id = $1 AND id = $2
		  AND (user_id = $3 OR user_id IS NULL)
		  AND read_at IS NULL`,
		tenantID, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Either the row doesn't exist / isn't visible, or it was already read.
		// Distinguish so an already-read mark is a no-op success, not a 404.
		var exists bool
		if err := r.pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM notifications
				WHERE tenant_id = $1 AND id = $2 AND (user_id = $3 OR user_id IS NULL)
			)`, tenantID, id, userID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
	}
	return nil
}

// MarkAllRead marks every unread notification visible to the user as read and
// returns how many rows were updated.
func (r *Repo) MarkAllRead(ctx context.Context, tenantID, userID uuid.UUID) (int, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE notifications
		SET read_at = now()
		WHERE tenant_id = $1
		  AND (user_id = $2 OR user_id IS NULL)
		  AND read_at IS NULL`,
		tenantID, userID)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
