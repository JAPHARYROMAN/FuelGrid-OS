package reportbuilder

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// ErrTemplateNotFound is returned when a template does not exist (or is invisible)
// for the tenant/actor.
var ErrTemplateNotFound = errors.New("reportbuilder: template not found")

// SharedScope is how a saved template is shared.
type SharedScope string

const (
	ScopePrivate SharedScope = "private" // visible only to the creator
	ScopeTenant  SharedScope = "tenant"  // visible to every reports user in the tenant
	ScopeRole    SharedScope = "role"    // visible to the listed role codes
)

// Template is a saved builder spec row.
type Template struct {
	ID                  uuid.UUID   `json:"id"`
	Name                string      `json:"name"`
	Description         string      `json:"description,omitempty"`
	DatasetKey          string      `json:"dataset_key"`
	Spec                Spec        `json:"spec"`
	RequiredPermission  string      `json:"required_permission"`
	SensitivePermission string      `json:"sensitive_permission,omitempty"`
	SharedScope         SharedScope `json:"shared_scope"`
	SharedRoles         []string    `json:"shared_roles"`
	CreatedBy           *uuid.UUID  `json:"created_by,omitempty"`
	IsSystem            bool        `json:"is_system"`
	CreatedAt           time.Time   `json:"created_at"`
	UpdatedAt           time.Time   `json:"updated_at"`
}

// TemplateInput is the create/update surface. Spec is the VALIDATED builder spec;
// the handler validates it (and derives the permissions) before calling the repo.
type TemplateInput struct {
	Name                string
	Description         string
	DatasetKey          string
	Spec                Spec
	RequiredPermission  string
	SensitivePermission string
	SharedScope         SharedScope
	SharedRoles         []string
}

// TemplateRepo is the data layer for report_templates. It runs on the owner pool
// and scopes every query explicitly by tenant_id (mirrors the reportrules repo);
// the table's RLS policy is defense-in-depth.
type TemplateRepo struct{ pool *database.Pool }

// NewTemplateRepo constructs a TemplateRepo over the given pool.
func NewTemplateRepo(pool *database.Pool) *TemplateRepo { return &TemplateRepo{pool: pool} }

const templateColumns = `
	id, name, COALESCE(description,''), dataset_key, spec,
	required_permission, COALESCE(sensitive_permission,''),
	shared_scope, shared_roles, created_by, is_system, created_at, updated_at
`

func scanTemplate(row pgx.Row) (Template, error) {
	var (
		t        Template
		desc     string
		sensPerm string
		specRaw  []byte
		scope    string
	)
	if err := row.Scan(&t.ID, &t.Name, &desc, &t.DatasetKey, &specRaw,
		&t.RequiredPermission, &sensPerm, &scope, &t.SharedRoles, &t.CreatedBy,
		&t.IsSystem, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return Template{}, err
	}
	t.Description = desc
	t.SensitivePermission = sensPerm
	t.SharedScope = SharedScope(scope)
	if t.SharedRoles == nil {
		t.SharedRoles = []string{}
	}
	if len(specRaw) > 0 {
		_ = json.Unmarshal(specRaw, &t.Spec)
	}
	return t, nil
}

// Create inserts a new (non-system) template and returns its id. The spec is
// stored as jsonb. Runs inside the caller's audited tx.
func (r *TemplateRepo) Create(ctx context.Context, tx pgx.Tx, tenantID, createdBy uuid.UUID, in TemplateInput) (uuid.UUID, error) {
	specJSON, err := json.Marshal(in.Spec)
	if err != nil {
		return uuid.Nil, err
	}
	roles := in.SharedRoles
	if roles == nil {
		roles = []string{}
	}
	scope := in.SharedScope
	if scope == "" {
		scope = ScopePrivate
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO report_templates
		    (tenant_id, name, description, dataset_key, spec, required_permission,
		     sensitive_permission, shared_scope, shared_roles, created_by, is_system)
		VALUES ($1, $2, NULLIF($3,''), $4, $5::jsonb, $6, NULLIF($7,''), $8, $9, $10, false)
		RETURNING id
	`, tenantID, in.Name, in.Description, in.DatasetKey, string(specJSON),
		in.RequiredPermission, in.SensitivePermission, string(scope), roles, createdBy).Scan(&id)
	return id, err
}

// Update fully replaces a template's editable fields. is_system templates are
// immutable via the API (the handler refuses them); the repo still scopes by
// tenant. ErrTemplateNotFound when absent/cross-tenant.
func (r *TemplateRepo) Update(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, in TemplateInput) error {
	specJSON, err := json.Marshal(in.Spec)
	if err != nil {
		return err
	}
	roles := in.SharedRoles
	if roles == nil {
		roles = []string{}
	}
	scope := in.SharedScope
	if scope == "" {
		scope = ScopePrivate
	}
	tag, err := tx.Exec(ctx, `
		UPDATE report_templates SET
		    name = $3,
		    description = NULLIF($4,''),
		    dataset_key = $5,
		    spec = $6::jsonb,
		    required_permission = $7,
		    sensitive_permission = NULLIF($8,''),
		    shared_scope = $9,
		    shared_roles = $10
		WHERE tenant_id = $1 AND id = $2 AND is_system = false
	`, tenantID, id, in.Name, in.Description, in.DatasetKey, string(specJSON),
		in.RequiredPermission, in.SensitivePermission, string(scope), roles)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTemplateNotFound
	}
	return nil
}

// Delete removes a tenant-owned template. ErrTemplateNotFound when absent,
// cross-tenant, or a system template.
func (r *TemplateRepo) Delete(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `DELETE FROM report_templates WHERE tenant_id = $1 AND id = $2 AND is_system = false`, tenantID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTemplateNotFound
	}
	return nil
}

// Get reads a single template by id (tenant-scoped only — the SHARE visibility
// check is the caller's job via Visible). ErrTemplateNotFound when absent or
// cross-tenant.
func (r *TemplateRepo) Get(ctx context.Context, tenantID, id uuid.UUID) (Template, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+templateColumns+` FROM report_templates WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	t, err := scanTemplate(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Template{}, ErrTemplateNotFound
	}
	return t, err
}

// List returns the tenant's templates ordered newest-first. Share-scope filtering
// is applied by the CALLER (Visible) per actor, since visibility depends on the
// actor's id + roles which RLS/the repo cannot express.
func (r *TemplateRepo) List(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]Template, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+templateColumns+`
		FROM report_templates
		WHERE tenant_id = $1
		ORDER BY created_at DESC, id
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Template{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Visible reports whether the template should be visible to the actor given the
// share scope. A private template is visible only to its creator; a tenant share
// to everyone in the tenant (the repo already tenant-scoped it); a role share to
// an actor holding any of the listed role codes. A system admin (or the creator)
// always sees it. This is the SHARE-SCOPE enforcement applied on read.
func (t Template) Visible(actorID uuid.UUID, actorRoles map[string]bool, isAdmin bool) bool {
	if isAdmin {
		return true
	}
	if t.CreatedBy != nil && *t.CreatedBy == actorID {
		return true
	}
	switch t.SharedScope {
	case ScopeTenant:
		return true
	case ScopeRole:
		for _, rc := range t.SharedRoles {
			if actorRoles[rc] {
				return true
			}
		}
		return false
	default: // ScopePrivate
		return false
	}
}
