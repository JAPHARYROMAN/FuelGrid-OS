package fleet

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Contact struct {
	ID                     uuid.UUID
	TenantID               uuid.UUID
	CustomerID             uuid.UUID
	Name                   string
	Role                   *string
	Email                  *string
	Phone                  *string
	StatementPreference    string
	NotificationPreference string
	CreatedAt              time.Time
}

type ContactInput struct {
	Name                   string
	Role                   *string
	Email                  *string
	Phone                  *string
	StatementPreference    string
	NotificationPreference string
}

const contactColumns = `
    id, tenant_id, customer_id, name, role, email, phone,
    statement_preference, notification_preference, created_at
`

func scanContact(row pgx.Row, c *Contact) error {
	return row.Scan(
		&c.ID, &c.TenantID, &c.CustomerID, &c.Name, &c.Role, &c.Email, &c.Phone,
		&c.StatementPreference, &c.NotificationPreference, &c.CreatedAt,
	)
}

func (r *Repo) ListContacts(ctx context.Context, tenantID, customerID uuid.UUID) ([]Contact, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+contactColumns+` FROM customer_contacts WHERE tenant_id = $1 AND customer_id = $2 ORDER BY name`, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Contact{}
	for rows.Next() {
		var c Contact
		if err := scanContact(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListContactsPage is the paginated variant of ListContacts (REL-REPO).
func (r *Repo) ListContactsPage(ctx context.Context, tenantID, customerID uuid.UUID, limit, offset int) ([]Contact, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+contactColumns+` FROM customer_contacts
		WHERE tenant_id = $1 AND customer_id = $2
		ORDER BY name, id
		LIMIT $3 OFFSET $4
	`, tenantID, customerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Contact{}
	for rows.Next() {
		var c Contact
		if err := scanContact(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repo) CreateContact(ctx context.Context, tx pgx.Tx, tenantID, customerID uuid.UUID, in ContactInput) (*Contact, error) {
	var c Contact
	if err := scanContact(tx.QueryRow(ctx, `
		INSERT INTO customer_contacts
		    (tenant_id, customer_id, name, role, email, phone, statement_preference, notification_preference)
		VALUES ($1, $2, $3, $4, $5, $6, COALESCE(NULLIF($7, ''), 'email'), COALESCE(NULLIF($8, ''), 'email'))
		RETURNING `+contactColumns,
		tenantID, customerID, in.Name, in.Role, in.Email, in.Phone, in.StatementPreference, in.NotificationPreference,
	), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) DeleteContact(ctx context.Context, tx pgx.Tx, tenantID, customerID, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `DELETE FROM customer_contacts WHERE tenant_id = $1 AND customer_id = $2 AND id = $3`, tenantID, customerID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
