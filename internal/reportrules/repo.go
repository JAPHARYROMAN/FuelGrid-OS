package reportrules

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// ErrNotFound is returned when a report rule does not exist for the tenant.
var ErrNotFound = errors.New("reportrules: not found")

// ErrSystemImmutable is returned when an API caller tries to DELETE a seeded
// system rule. System rules can be disabled / re-moded / tuned, never deleted, so
// the seed set is always restorable.
var ErrSystemImmutable = errors.New("reportrules: system rule cannot be deleted")

// Repo is the data layer for report_rules (Reports Center Phase 15). It runs on
// the owner pool and scopes every query explicitly by tenant_id (mirrors the
// risk/scheduledReports repos); the table's RLS policy is defense-in-depth.
type Repo struct{ pool *database.Pool }

// New constructs a Repo over the given pool.
func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

// RuleInput is the configurable surface for creating or updating a report rule.
// Threshold is a decimal STRING ("" => NULL) so it stays off the float path;
// ComparisonPeriodDays <= 0 leaves the column NULL; ThresholdConfigJSON is the
// raw jsonb body ("" => '{}').
type RuleInput struct {
	Code                 string
	Name                 string
	Description          string
	ReportKey            string
	Category             string
	Condition            string
	Threshold            string
	ThresholdConfigJSON  string
	ComparisonPeriodDays int
	Severity             string
	MessageTemplate      string
	RecommendedAction    string
	Placement            string
	Mode                 string
	NotifyOnFire         *bool
	Enabled              *bool
	Status               string
}

func nullableStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullableDays(d int) *int {
	if d <= 0 {
		return nil
	}
	return &d
}

func boolOr(p *bool, def bool) bool {
	if p != nil {
		return *p
	}
	return def
}

// CreateRule inserts a tenant-owned (non-system) report rule and returns its id.
func (r *Repo) CreateRule(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in RuleInput) (uuid.UUID, error) {
	cfg := in.ThresholdConfigJSON
	if cfg == "" {
		cfg = "{}"
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO report_rules
		    (tenant_id, code, name, description, report_key, category, condition,
		     threshold, threshold_config, comparison_period_days, severity,
		     message_template, recommended_action, report_placement, mode,
		     notify_on_fire, is_system, enabled, status)
		VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), COALESCE(NULLIF($6,''),'general'),
		        $7, $8::numeric, $9::jsonb, $10, COALESCE(NULLIF($11,''),'info'),
		        $12, NULLIF($13,''), COALESCE(NULLIF($14,''),'insight'),
		        COALESCE(NULLIF($15,''),'augment'), $16, false, $17,
		        COALESCE(NULLIF($18,''),'active'))
		RETURNING id
	`, tenantID, in.Code, in.Name, in.Description, in.ReportKey, in.Category, in.Condition,
		nullableStr(in.Threshold), cfg, nullableDays(in.ComparisonPeriodDays), in.Severity,
		in.MessageTemplate, in.RecommendedAction, in.Placement, in.Mode,
		boolOr(in.NotifyOnFire, false), boolOr(in.Enabled, true), in.Status).Scan(&id)
	return id, err
}

// UpdateRule fully updates a rule's configurable fields. is_system / code are
// immutable. Threshold and threshold_config are set unconditionally (a caller may
// clear them); the COALESCE(NULLIF...) fields keep a value when the caller sends
// an empty string. enabled / notify_on_fire are only changed when supplied.
func (r *Repo) UpdateRule(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, in RuleInput) error {
	cfg := in.ThresholdConfigJSON
	if cfg == "" {
		cfg = "{}"
	}
	tag, err := tx.Exec(ctx, `
		UPDATE report_rules SET
		    name = COALESCE(NULLIF($3,''), name),
		    description = $4,
		    report_key = COALESCE(NULLIF($5,''), report_key),
		    category = COALESCE(NULLIF($6,''), category),
		    condition = COALESCE(NULLIF($7,''), condition),
		    threshold = $8::numeric,
		    threshold_config = $9::jsonb,
		    comparison_period_days = COALESCE($10, comparison_period_days),
		    severity = COALESCE(NULLIF($11,''), severity),
		    message_template = COALESCE(NULLIF($12,''), message_template),
		    recommended_action = COALESCE(NULLIF($13,''), recommended_action),
		    report_placement = COALESCE(NULLIF($14,''), report_placement),
		    mode = COALESCE(NULLIF($15,''), mode),
		    notify_on_fire = COALESCE($16, notify_on_fire),
		    enabled = COALESCE($17, enabled),
		    status = COALESCE(NULLIF($18,''), status)
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id, in.Name, in.Description, in.ReportKey, in.Category, in.Condition,
		nullableStr(in.Threshold), cfg, nullableDays(in.ComparisonPeriodDays), in.Severity,
		in.MessageTemplate, in.RecommendedAction, in.Placement, in.Mode,
		in.NotifyOnFire, in.Enabled, in.Status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetRuleEnabled flips a rule's enabled flag — the per-rule kill switch that makes
// disabling a rule actually remove its insight from the report.
func (r *Repo) SetRuleEnabled(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, enabled bool) error {
	tag, err := tx.Exec(ctx, `UPDATE report_rules SET enabled = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, id, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteRule removes a tenant-owned rule. A seeded system rule is immutable
// (ErrSystemImmutable) so the default set is always restorable; disable it
// instead.
func (r *Repo) DeleteRule(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) error {
	var isSystem bool
	err := tx.QueryRow(ctx, `SELECT is_system FROM report_rules WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan(&isSystem)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if isSystem {
		return ErrSystemImmutable
	}
	tag, err := tx.Exec(ctx, `DELETE FROM report_rules WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const ruleColumns = `
	id, code, name, description, report_key, category, condition,
	threshold::text, threshold_config, comparison_period_days, severity,
	message_template, recommended_action, report_placement, mode,
	notify_on_fire, is_system, enabled, status
`

// scanRuleMap scans a row into the JSON-facing map the CRUD API returns.
func scanRuleMap(rows pgx.Rows) (map[string]any, error) {
	var (
		id                                        uuid.UUID
		code, name, category, condition, severity string
		placement, mode, status                   string
		description, reportKey, threshold         *string
		msgTmpl, recAction                        *string
		compPeriod                                *int
		cfgRaw                                    []byte
		notify, isSystem, enabled                 bool
	)
	if err := rows.Scan(&id, &code, &name, &description, &reportKey, &category, &condition,
		&threshold, &cfgRaw, &compPeriod, &severity, &msgTmpl, &recAction, &placement, &mode,
		&notify, &isSystem, &enabled, &status); err != nil {
		return nil, err
	}
	var cfg any
	if len(cfgRaw) > 0 {
		_ = json.Unmarshal(cfgRaw, &cfg)
	}
	return map[string]any{
		"id": id, "code": code, "name": name, "description": description,
		"report_key": reportKey, "category": category, "condition": condition,
		"threshold": threshold, "threshold_config": cfg,
		"comparison_period_days": compPeriod, "severity": severity,
		"message_template": msgTmpl, "recommended_action": recAction,
		"report_placement": placement, "mode": mode, "notify_on_fire": notify,
		"is_system": isSystem, "enabled": enabled, "status": status,
	}, nil
}

// ListRulesPage lists a tenant's report rules (optionally filtered by report_key)
// ordered by code. code is unique per tenant so it is a stable ordering key. An
// empty reportKey lists ALL rules (the management surface); a non-empty one lists
// rules for that report PLUS the broad (NULL report_key) rules.
func (r *Repo) ListRulesPage(ctx context.Context, tenantID uuid.UUID, reportKey string, limit, offset int) ([]map[string]any, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+ruleColumns+`
		FROM report_rules
		WHERE tenant_id = $1
		  AND ($2 = '' OR report_key = $2 OR report_key IS NULL)
		ORDER BY code
		LIMIT $3 OFFSET $4
	`, tenantID, reportKey, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		m, err := scanRuleMap(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetRule reads a single rule by id (tenant-scoped). Returns ErrNotFound when
// absent or cross-tenant.
func (r *Repo) GetRule(ctx context.Context, tenantID, id uuid.UUID) (map[string]any, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+ruleColumns+` FROM report_rules WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if rows.Err() != nil {
			return nil, rows.Err()
		}
		return nil, ErrNotFound
	}
	return scanRuleMap(rows)
}

// LoadActiveRules loads the enabled, active rules applicable to a report key (the
// report's own rules PLUS the broad NULL-report_key rules) as evaluable Rule
// values. This is what the report handler passes to Evaluate. It runs on the
// owner pool and is tenant-scoped; an error returns no rules (the report still
// renders with just the composer output — fail open, never block the report).
func (r *Repo) LoadActiveRules(ctx context.Context, tenantID uuid.UUID, reportKey string) ([]Rule, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, code, name, COALESCE(report_key,''), category, condition,
		       COALESCE(threshold::text,''), threshold_config,
		       COALESCE(comparison_period_days,0), severity,
		       message_template, COALESCE(recommended_action,''),
		       report_placement, mode, notify_on_fire, is_system, enabled, status
		FROM report_rules
		WHERE tenant_id = $1
		  AND enabled = true AND status = 'active'
		  AND (report_key = $2 OR report_key IS NULL)
		ORDER BY code
	`, tenantID, reportKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var (
			id                                             uuid.UUID
			code, name, rk, category, condition, threshold string
			severity, msgTmpl, recAction, placement, mode  string
			status                                         string
			compPeriod                                     int
			cfgRaw                                         []byte
			notify, isSystem, enabled                      bool
		)
		if err := rows.Scan(&id, &code, &name, &rk, &category, &condition, &threshold,
			&cfgRaw, &compPeriod, &severity, &msgTmpl, &recAction, &placement, &mode,
			&notify, &isSystem, &enabled, &status); err != nil {
			return nil, err
		}
		cfg := map[string]any{}
		if len(cfgRaw) > 0 {
			_ = json.Unmarshal(cfgRaw, &cfg)
		}
		out = append(out, Rule{
			ID: id.String(), Code: code, Name: name, ReportKey: rk, Category: category,
			Condition: condition, Threshold: threshold, ThresholdConfig: cfg,
			ComparisonPeriodDays: compPeriod, Severity: Severity(severity),
			MessageTemplate: msgTmpl, RecommendedAction: recAction,
			Placement: Placement(placement), Mode: Mode(mode), NotifyOnFire: notify,
			IsSystem: isSystem, Enabled: enabled, Status: status,
		})
	}
	return out, rows.Err()
}
