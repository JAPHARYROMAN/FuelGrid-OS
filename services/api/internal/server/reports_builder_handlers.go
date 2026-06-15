package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
	"github.com/japharyroman/fuelgrid-os/internal/reportbuilder"
)

// Custom Report Builder (Reports Center Phase 11 — blueprint §6, §22).
//
// A WHITELISTED dataset registry (internal/reportbuilder) + a SAFE query composer:
// the user picks a registered dataset + a subset of its allowlisted dimensions,
// measures (with an agg), filters, sort and a visualization, and the composer
// builds a parameterized, tenant- + station-scoped query from ONLY the registry's
// identifiers (NO free SQL, ever). Sensitive columns (margin / cost / exposure)
// are gated by margin.view and OMITTED for non-holders. Results render through the
// shared ReportEnvelope, so the builder reuses the report shell, export, snapshots,
// rules and scheduling.
//
// Endpoints (all under the admin-console group; coarse-gated by reports.builder):
//   GET    /reports/builder/datasets             — the registry the actor may use
//   POST   /reports/builder/preview              — validate a spec + run (no save)
//   POST   /reports/builder/templates            — save a template
//   GET    /reports/builder/templates            — list visible templates
//   GET    /reports/builder/templates/{id}       — read one (share-scope enforced)
//   PUT    /reports/builder/templates/{id}       — update (owner / admin)
//   DELETE /reports/builder/templates/{id}       — delete (owner / admin)
//   POST   /reports/builder/templates/{id}/run   — run a saved template

// ---- dataset registry listing ----

// handleBuilderDatasets returns the curated datasets the actor MAY use, each with
// its dimensions / measures / filters, permission-filtered:
//   - a dataset is listed only when the actor holds its required_permission;
//   - a dataset's SENSITIVE measures are stripped from the response when the actor
//     lacks the dataset's sensitive permission (margin.view) — the builder never
//     offers a column the actor cannot read.
func (s *Server) handleBuilderDatasets(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ps, err := s.policy.LoadFor(r.Context(), actor)
	if err != nil {
		s.logger.Error("builder datasets: policy load", "error", err)
		writeError(w, http.StatusInternalServerError, "authorization error")
		return
	}

	out := struct {
		GeneratedAt string                  `json:"generated_at"`
		Datasets    []reportbuilder.Dataset `json:"datasets"`
		Aggregates  []reportbuilder.AggFunc `json:"aggregates"`
	}{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Datasets:    []reportbuilder.Dataset{},
		Aggregates:  []reportbuilder.AggFunc{reportbuilder.AggSum, reportbuilder.AggAvg, reportbuilder.AggCount, reportbuilder.AggMin, reportbuilder.AggMax},
	}

	for _, ds := range reportbuilder.Registry() {
		if !canViewPermission(ps, ds.RequiredPermission) {
			continue
		}
		// Strip sensitive measures the actor cannot read so the builder never lists
		// a column that would be omitted at run time.
		allowSensitive := ds.SensitivePermission == "" || canViewPermission(ps, ds.SensitivePermission)
		if !allowSensitive {
			ds = stripSensitiveMeasures(ds)
		}
		out.Datasets = append(out.Datasets, ds)
	}
	writeJSON(w, http.StatusOK, out)
}

// stripSensitiveMeasures returns a copy of ds with its sensitive measures removed,
// for an actor who cannot read them. The dataset's other fields are unchanged.
func stripSensitiveMeasures(ds reportbuilder.Dataset) reportbuilder.Dataset {
	kept := make([]reportbuilder.Measure, 0, len(ds.Measures))
	for i := range ds.Measures {
		if ds.Measures[i].Sensitive {
			continue
		}
		kept = append(kept, ds.Measures[i])
	}
	ds.Measures = kept
	return ds
}

// ---- preview (validate + run, no save) ----

type builderPreviewRequest struct {
	Spec reportbuilder.Spec `json:"spec"`
}

// handleBuilderPreview validates the spec against the registry allowlist and, if
// valid, composes + runs the query and returns a ReportEnvelope. It does NOT save.
// The dataset's own permission is the authoritative data gate (re-checked here),
// on top of the route's reports.builder gate.
func (s *Server) handleBuilderPreview(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req builderPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	s.runBuilderSpec(w, r, actor, req.Spec, "Custom report preview")
}

// runBuilderSpec is the shared validate→authorize→compose→envelope path used by
// preview and template run. It:
//  1. validates the spec against the registry (400 with a precise code on any
//     non-allowlisted identifier / agg / operator — NO query runs),
//  2. re-checks the dataset's permission against the actor's policy (403 if not
//     held) — the authoritative data gate at preview AND run time,
//  3. resolves the actor's tenant + station scope and the sensitive-permission
//     flag, then composes + runs the parameterized query,
//  4. maps the result into a ReportEnvelope (table + summary + chart) and folds in
//     an honest data-quality note when sensitive columns were omitted.
func (s *Server) runBuilderSpec(w http.ResponseWriter, r *http.Request, actor identity.Actor, spec reportbuilder.Spec, title string) {
	ctx := r.Context()

	// (1) Allowlist validation — the only path from a spec to SQL. A precise,
	// machine-readable code (e.g. "unknown_dimension") rides the 400 so the client
	// can pinpoint the offending field; the generic "invalid_spec" is the fallback.
	ds, verr := spec.Validate()
	if verr != nil {
		code := reportbuilder.SpecErrorCode(verr)
		if code == "" {
			code = "invalid_spec"
		}
		writeErrorCode(w, http.StatusBadRequest, code, verr.Error())
		return
	}

	// (2) Dataset permission re-check (preview + run both gate here).
	ps, perr := s.policy.LoadFor(ctx, actor)
	if perr != nil {
		s.logger.Error("builder: policy load", "error", perr)
		writeError(w, http.StatusInternalServerError, "authorization error")
		return
	}
	if !canViewPermission(ps, ds.RequiredPermission) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	// (3) Scope: tenant always; station scope for a station-axis dataset. Sensitive
	// columns require the dataset's sensitive permission.
	scope := reportbuilder.ScopeContext{
		TenantID:       actor.TenantID,
		TenantWide:     ps.TenantWide || ps.IsSystemAdmin,
		AllowSensitive: ds.SensitivePermission == "" || canViewPermission(ps, ds.SensitivePermission),
	}
	if !scope.TenantWide {
		for id := range ps.StationIDs {
			scope.StationIDs = append(scope.StationIDs, id)
		}
	}

	// Compose + run inside a tenant-scoped, read-only transaction so RLS (when the
	// app pool is the non-owner role) is active end-to-end. The composer never
	// writes; the tx is read-only and rolled back.
	var res reportbuilder.Result
	runErr := s.withReadTx(ctx, actor.TenantID, func(tx pgx.Tx) error {
		var cerr error
		res, cerr = reportbuilder.Compose(ctx, tx, ds, spec, scope)
		return cerr
	})
	if runErr != nil {
		if code := reportbuilder.SpecErrorCode(runErr); code != "" {
			// A scope-level rejection (no station access / tenant-wide-only dataset).
			writeErrorCode(w, http.StatusForbidden, code, runErr.Error())
			return
		}
		s.logger.Error("builder: compose/run", "error", runErr, "dataset", ds.Key)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	env := builderEnvelope(ds, spec, res, title)
	writeJSON(w, http.StatusOK, env)
}

// withReadTx runs fn inside a transaction with app.current_tenant set to tenantID
// (so RLS is active) and ALWAYS rolls back — the builder is strictly read-only.
func (s *Server) withReadTx(ctx context.Context, tenantID uuid.UUID, fn func(pgx.Tx) error) error {
	tx, err := s.deps.DB.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL app.current_tenant = '"+tenantID.String()+"'"); err != nil {
		return err
	}
	return fn(tx)
}

// builderEnvelope maps a composed Result onto the shared ReportEnvelope: the
// columns/rows become the drillable table, a per-decimal-measure total becomes the
// summary, and a chart payload carries the columns + rows + the chosen viz. An
// omitted-sensitive note is added to data-quality, honestly.
func builderEnvelope(ds reportbuilder.Dataset, spec reportbuilder.Spec, res reportbuilder.Result, title string) ReportEnvelope {
	if title == "" {
		title = ds.Name
	}
	env := newEnvelope("custom:"+ds.Key, title, "custom", nil)
	env.FiltersUsed["dataset"] = ds.Key
	viz := spec.Viz
	if viz == "" {
		viz = "table"
	}
	env.FiltersUsed["viz"] = viz

	// Table: the columns + rows verbatim (decimal strings pass through).
	cols := make([]string, len(res.Columns))
	for i := range res.Columns {
		cols[i] = res.Columns[i].Label
	}
	env.Table.Columns = cols
	env.Table.Rows = res.Rows

	// Summary: the SUM of each decimal measure column across the returned rows is
	// not recomputed here (no float money) — instead we surface row count + the
	// distinct dimension count as honest, non-money headline figures, plus the
	// dataset name. Per-row decimal values stay exact in the table.
	env.Summary = []summaryMetric{
		{Label: "Rows", Value: strconv.Itoa(len(res.Rows)), Unit: "count"},
		{Label: "Columns", Value: strconv.Itoa(len(res.Columns)), Unit: "count"},
	}

	// Chart payload: the structured columns + rows + viz so the frontend can render
	// the chosen visualization without re-deriving the shape.
	env.ChartData = builderChart{
		Viz:     viz,
		Columns: res.Columns,
		Rows:    res.Rows,
	}

	if len(res.OmittedSensitive) > 0 {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "info",
			Message: "Some sensitive columns (margin / cost / exposure) are hidden — they require the " + ds.SensitivePermission + " permission.",
		})
	}
	return env
}

// builderChart is the builder's chart_data payload.
type builderChart struct {
	Viz     string                 `json:"viz"`
	Columns []reportbuilder.Column `json:"columns"`
	Rows    [][]string             `json:"rows"`
}

// ---- templates CRUD ----

type templateRequest struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description,omitempty"`
	Spec        reportbuilder.Spec        `json:"spec"`
	SharedScope reportbuilder.SharedScope `json:"shared_scope,omitempty"`
	SharedRoles []string                  `json:"shared_roles,omitempty"`
}

// deriveTemplatePermissions resolves the (validated) spec's dataset into the
// required_permission stored on the template and the sensitive_permission when the
// spec selects any sensitive measure. These are the run-time gates.
func deriveTemplatePermissions(ds reportbuilder.Dataset, spec reportbuilder.Spec) (required, sensitive string) {
	required = ds.RequiredPermission
	for _, sm := range spec.Measures {
		for i := range ds.Measures {
			if ds.Measures[i].ID == sm.Measure && ds.Measures[i].Sensitive {
				return required, ds.SensitivePermission
			}
		}
	}
	return required, ""
}

func (req templateRequest) validate() (reportbuilder.Dataset, string, bool) {
	if req.Name == "" {
		return reportbuilder.Dataset{}, "name is required", false
	}
	ds, err := req.Spec.Validate()
	if err != nil {
		return reportbuilder.Dataset{}, err.Error(), false
	}
	scope := req.SharedScope
	if scope == "" {
		scope = reportbuilder.ScopePrivate
	}
	switch scope {
	case reportbuilder.ScopePrivate, reportbuilder.ScopeTenant:
	case reportbuilder.ScopeRole:
		if len(req.SharedRoles) == 0 {
			return reportbuilder.Dataset{}, "shared_roles is required for a role share", false
		}
	default:
		return reportbuilder.Dataset{}, "shared_scope must be private|tenant|role", false
	}
	return ds, "", true
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req templateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	ds, msg, ok := req.validate()
	if !ok {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	required, sensitive := deriveTemplatePermissions(ds, req.Spec)
	scope := req.SharedScope
	if scope == "" {
		scope = reportbuilder.ScopePrivate
	}
	in := reportbuilder.TemplateInput{
		Name: req.Name, Description: req.Description, DatasetKey: ds.Key, Spec: req.Spec,
		RequiredPermission: required, SensitivePermission: sensitive,
		SharedScope: scope, SharedRoles: req.SharedRoles,
	}

	var id uuid.UUID
	committed := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "report_template.created", EventType: "ReportTemplateCreated", EntityType: "report_template",
	}, func(tx pgx.Tx) (string, error) {
		newID, err := s.reportTemplate.Create(r.Context(), tx, actor.TenantID, actor.UserID, in)
		if err != nil {
			if isUniqueViolation(err) {
				writeError(w, http.StatusConflict, "a template with this name already exists")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		id = newID
		return newID.String(), nil
	})
	if !committed {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	ps, roleCodes, ok := s.actorRoleCodesAndPolicy(w, r, actor)
	if !ok {
		return
	}
	// Share-scope is filtered IN SQL (ListVisible), so pagination is correct no
	// matter how many invisible templates other actors created — a permitted
	// actor's templates can never be pushed past the fetched window. Over-fetch one
	// row to compute has_more honestly.
	rows, err := s.reportTemplate.ListVisible(r.Context(), actor.TenantID, actor.UserID, roleCodes, ps.IsSystemAdmin, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	writePagedMore(w, http.StatusOK, rows, len(rows), limit, offset, hasMore)
}

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	tpl, err := s.reportTemplate.Get(r.Context(), actor.TenantID, id)
	if errors.Is(err, reportbuilder.ErrTemplateNotFound) {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	ps, roles, ok := s.actorRolesAndPolicy(w, r, actor)
	if !ok {
		return
	}
	// Share-scope enforced on read: a private template the actor cannot see 404s
	// (indistinguishable from "does not exist" — no existence leak).
	if !tpl.Visible(actor.UserID, roles, ps.IsSystemAdmin) {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	writeJSON(w, http.StatusOK, tpl)
}

func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req templateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	ds, msg, ok := req.validate()
	if !ok {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	// Ownership: only the creator or a system admin may edit a template.
	existing, gerr := s.reportTemplate.Get(r.Context(), actor.TenantID, id)
	if errors.Is(gerr, reportbuilder.ErrTemplateNotFound) {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	if gerr != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.canManageTemplate(w, r, actor, existing) {
		return
	}

	required, sensitive := deriveTemplatePermissions(ds, req.Spec)
	scope := req.SharedScope
	if scope == "" {
		scope = reportbuilder.ScopePrivate
	}
	in := reportbuilder.TemplateInput{
		Name: req.Name, Description: req.Description, DatasetKey: ds.Key, Spec: req.Spec,
		RequiredPermission: required, SensitivePermission: sensitive,
		SharedScope: scope, SharedRoles: req.SharedRoles,
	}
	committed := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "report_template.updated", EventType: "ReportTemplateUpdated", EntityType: "report_template", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		if err := s.reportTemplate.Update(r.Context(), tx, actor.TenantID, id, in); errors.Is(err, reportbuilder.ErrTemplateNotFound) {
			writeError(w, http.StatusNotFound, "template not found")
			return "", err
		} else if err != nil {
			if isUniqueViolation(err) {
				writeError(w, http.StatusConflict, "a template with this name already exists")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		return id.String(), nil
	})
	if !committed {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	existing, gerr := s.reportTemplate.Get(r.Context(), actor.TenantID, id)
	if errors.Is(gerr, reportbuilder.ErrTemplateNotFound) {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	if gerr != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.canManageTemplate(w, r, actor, existing) {
		return
	}
	committed := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "report_template.deleted", EventType: "ReportTemplateDeleted", EntityType: "report_template", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		if err := s.reportTemplate.Delete(r.Context(), tx, actor.TenantID, id); errors.Is(err, reportbuilder.ErrTemplateNotFound) {
			writeError(w, http.StatusNotFound, "template not found")
			return "", err
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		return id.String(), nil
	})
	if !committed {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}

// handleRunTemplate runs a SAVED template and returns a ReportEnvelope, RE-CHECKING
// the permission at run time (a viewer of a shared template still cannot read data
// they could not run live). Share-scope is enforced on the read; the spec is the
// stored (already-validated) spec, re-validated as defense-in-depth.
func (s *Server) handleRunTemplate(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	tpl, err := s.reportTemplate.Get(r.Context(), actor.TenantID, id)
	if errors.Is(err, reportbuilder.ErrTemplateNotFound) {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	ps, roles, ok := s.actorRolesAndPolicy(w, r, actor)
	if !ok {
		return
	}
	if !tpl.Visible(actor.UserID, roles, ps.IsSystemAdmin) {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	// runBuilderSpec re-validates the spec AND re-checks the dataset permission, so
	// a shared template never exposes data the runner cannot read live.
	s.runBuilderSpec(w, r, actor, tpl.Spec, tpl.Name)
}

// ---- helpers ----

// canManageTemplate enforces template OWNERSHIP for edit/delete: only the creator
// or a system admin may manage a template. Writes the 403 and returns false when
// not permitted.
func (s *Server) canManageTemplate(w http.ResponseWriter, r *http.Request, actor identity.Actor, tpl reportbuilder.Template) bool {
	ps, err := s.policy.LoadFor(r.Context(), actor)
	if err != nil {
		s.logger.Error("template manage: policy load", "error", err)
		writeError(w, http.StatusInternalServerError, "authorization error")
		return false
	}
	if ps.IsSystemAdmin {
		return true
	}
	if tpl.CreatedBy != nil && *tpl.CreatedBy == actor.UserID {
		return true
	}
	writeError(w, http.StatusForbidden, "only the template owner can manage it")
	return false
}

// actorRoleCodesAndPolicy loads the actor's policy + role codes (as a slice) for
// the share-scope visibility check. On error it writes the response and returns
// ok=false.
func (s *Server) actorRoleCodesAndPolicy(w http.ResponseWriter, r *http.Request, actor identity.Actor) (policy.PermissionSet, []string, bool) {
	ps, err := s.policy.LoadFor(r.Context(), actor)
	if err != nil {
		s.logger.Error("builder: policy load", "error", err)
		writeError(w, http.StatusInternalServerError, "authorization error")
		return policy.PermissionSet{}, nil, false
	}
	codes, err := s.userRepo.ListRoles(r.Context(), actor.UserID)
	if err != nil {
		s.logger.Error("builder: list roles", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return policy.PermissionSet{}, nil, false
	}
	return ps, codes, true
}

// actorRolesAndPolicy loads the actor's policy + role codes (as a set) for the
// per-row share-scope visibility check (handleGetTemplate / handleRunTemplate).
// On error it writes the response and returns ok=false.
func (s *Server) actorRolesAndPolicy(w http.ResponseWriter, r *http.Request, actor identity.Actor) (policy.PermissionSet, map[string]bool, bool) {
	ps, codes, ok := s.actorRoleCodesAndPolicy(w, r, actor)
	if !ok {
		return policy.PermissionSet{}, nil, false
	}
	roles := make(map[string]bool, len(codes))
	for _, c := range codes {
		roles[c] = true
	}
	return ps, roles, true
}
