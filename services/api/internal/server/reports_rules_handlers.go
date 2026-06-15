package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/notifications"
	"github.com/japharyroman/fuelgrid-os/internal/reporting"
	"github.com/japharyroman/fuelgrid-os/internal/reportrules"
)

// pointValues extracts the decimal-string Values from a chronological series of
// reporting.PeriodPoint (oldest->newest), for feeding seriesFacts.
func pointValues(pts []reporting.PeriodPoint) []string {
	out := make([]string, len(pts))
	for i := range pts {
		out[i] = pts[i].Value
	}
	return out
}

// Report insight rules CRUD + the engine glue (Reports Center Phase 15 — blueprint
// §9 / §9.3 / §21.3 / §23). This is the report-insight sibling of risk_handlers.go's
// rule CRUD: GET/POST/PUT/DELETE /reports/rules (+ enable/disable), gated by
// reports.rules.manage, tenant-isolated, and audited. The engine itself
// (internal/reportrules) is wired into the report envelopes via runReportRules.

// runReportRules is the single glue the structured report handlers call after
// applyReport to fold the config-driven engine's AUGMENT-mode insights into the
// envelope (no-regression: the composer stays authoritative; only tuned/custom
// rules add lines). It loads the tenant's active rules for reportKey, evaluates
// them against the already-computed facts, records every hit in env.InsightRules,
// and dispatches an in-app notification for any fired rule that opted in
// (notify_on_fire) at warning/critical severity. It NEVER fails the report: a
// rules load error is logged and the report renders with just the composer output.
func (s *Server) runReportRules(ctx context.Context, tenantID uuid.UUID, env *ReportEnvelope, reportKey string, facts reportrules.Facts) {
	if s.reportRules == nil {
		return
	}
	rules, err := s.reportRules.LoadActiveRules(ctx, tenantID, reportKey)
	if err != nil {
		s.logger.WarnContext(ctx, "report rules load failed; rendering with composer output only",
			"report_key", reportKey, "error", err)
		return
	}
	fired := env.applyReportRules(reportKey, rules, facts)
	if s.notifications == nil {
		return
	}
	for i := range fired {
		f := fired[i]
		// Opt-in only, and only when the rule's insight actually surfaces (augment)
		// at a severity that warrants a push. The engine never spams: a silent or
		// shadow rule, or one that did not set notify_on_fire, produces no
		// notification.
		if !f.NotifyOnFire || f.Mode != reportrules.ModeAugment {
			continue
		}
		if f.Severity != reportrules.SeverityWarning && f.Severity != reportrules.SeverityCritical {
			continue
		}
		entType := "report_rule"
		entID := f.RuleID
		if _, nerr := s.notifications.Create(ctx, tenantID, notifications.CreateInput{
			Type:              "report_insight",
			Title:             "Report insight: " + f.RuleName,
			Body:              f.Message,
			Severity:          string(f.Severity),
			RelatedEntityType: &entType,
			RelatedEntityID:   &entID,
		}); nerr != nil {
			s.logger.WarnContext(ctx, "report rule notification failed",
				"rule_code", f.RuleCode, "error", nerr)
		}
	}
}

// seriesFacts fills the gross/margin period-over-period + variance-vs-average
// facts from a chronological (oldest->newest) series of decimal-string values,
// under the given fact-key prefix ("gross" or "margin"). It mirrors the composer
// inputs exactly: <prefix>_current = the last point, <prefix>_prior = the prior
// point, and <prefix>_avg = the mean of all EARLIER points (the same window the
// VarianceVs30dAverage composer averages). Blank/short series simply leave keys
// unset (an honest no-op), so the corresponding rule cannot fire.
func seriesFacts(facts reportrules.Facts, prefix string, values []string) {
	n := len(values)
	if n >= 1 {
		facts.Nums[prefix+"_current"] = values[n-1]
	}
	if n >= 2 {
		facts.Nums[prefix+"_prior"] = values[n-2]
	}
	if n >= 3 {
		var sum float64
		var count int
		for i := 0; i < n-1; i++ {
			if v, ok := parseFloatSafe(values[i]); ok {
				sum += v
				count++
			}
		}
		if count > 0 {
			facts.Nums[prefix+"_avg"] = strconvFormatFloat(sum / float64(count))
		}
	}
}

// strconvFormatFloat renders a float average as a decimal string for a fact value.
func strconvFormatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// ---- CRUD ----

type reportRuleRequest struct {
	Code                 string          `json:"code"`
	Name                 string          `json:"name"`
	Description          string          `json:"description,omitempty"`
	ReportKey            string          `json:"report_key,omitempty"`
	Category             string          `json:"category,omitempty"`
	Condition            string          `json:"condition,omitempty"`
	Threshold            string          `json:"threshold,omitempty"`
	ThresholdConfig      json.RawMessage `json:"threshold_config,omitempty"`
	ComparisonPeriodDays int             `json:"comparison_period_days,omitempty"`
	Severity             string          `json:"severity,omitempty"`
	MessageTemplate      string          `json:"message_template,omitempty"`
	RecommendedAction    string          `json:"recommended_action,omitempty"`
	ReportPlacement      string          `json:"report_placement,omitempty"`
	Mode                 string          `json:"mode,omitempty"`
	NotifyOnFire         *bool           `json:"notify_on_fire,omitempty"`
	Status               string          `json:"status,omitempty"`
	Enabled              *bool           `json:"enabled,omitempty"`
}

// toInput maps the request to the repo input, validating the condition against the
// registered evaluator set and the enum fields. Returns ("", true) on success or
// (message, false) on a validation failure.
func (req reportRuleRequest) toInput() (reportrules.RuleInput, string, bool) {
	if req.Condition != "" {
		if _, ok := reportrules.EvaluatorFor(req.Condition); !ok {
			return reportrules.RuleInput{}, "unknown condition (no registered evaluator)", false
		}
	}
	if req.Severity != "" && req.Severity != "info" && req.Severity != "warning" && req.Severity != "critical" {
		return reportrules.RuleInput{}, "severity must be info|warning|critical", false
	}
	if req.ReportPlacement != "" && req.ReportPlacement != "insight" && req.ReportPlacement != "data_quality" && req.ReportPlacement != "summary" {
		return reportrules.RuleInput{}, "report_placement must be insight|data_quality|summary", false
	}
	if req.Mode != "" && req.Mode != "shadow" && req.Mode != "augment" {
		return reportrules.RuleInput{}, "mode must be shadow|augment", false
	}
	if req.Status != "" && req.Status != "draft" && req.Status != "active" && req.Status != "paused" && req.Status != "retired" {
		return reportrules.RuleInput{}, "status must be draft|active|paused|retired", false
	}
	cfg := ""
	if len(req.ThresholdConfig) > 0 {
		// Validate it parses as a JSON object so a malformed body fails fast rather
		// than at the DB cast.
		var probe map[string]any
		if err := json.Unmarshal(req.ThresholdConfig, &probe); err != nil {
			return reportrules.RuleInput{}, "threshold_config must be a JSON object", false
		}
		cfg = string(req.ThresholdConfig)
	}
	return reportrules.RuleInput{
		Code: req.Code, Name: req.Name, Description: req.Description,
		ReportKey: req.ReportKey, Category: req.Category, Condition: req.Condition,
		Threshold: req.Threshold, ThresholdConfigJSON: cfg,
		ComparisonPeriodDays: req.ComparisonPeriodDays, Severity: req.Severity,
		MessageTemplate: req.MessageTemplate, RecommendedAction: req.RecommendedAction,
		Placement: req.ReportPlacement, Mode: req.Mode, NotifyOnFire: req.NotifyOnFire,
		Enabled: req.Enabled, Status: req.Status,
	}, "", true
}

func (s *Server) handleListReportRules(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.reportRules.ListRulesPage(r.Context(), actor.TenantID, r.URL.Query().Get("report_key"), limit+1, offset)
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

func (s *Server) handleGetReportRule(w http.ResponseWriter, r *http.Request) {
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
	rule, err := s.reportRules.GetRule(r.Context(), actor.TenantID, id)
	if errors.Is(err, reportrules.ErrNotFound) {
		writeError(w, http.StatusNotFound, "rule not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) handleCreateReportRule(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req reportRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" || req.Name == "" || req.Condition == "" || req.MessageTemplate == "" {
		writeError(w, http.StatusBadRequest, "code, name, condition and message_template are required")
		return
	}
	in, msg, ok := req.toInput()
	if !ok {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	var ruleID uuid.UUID
	committed := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "report_rule.created", EventType: "ReportRuleCreated", EntityType: "report_rule",
	}, func(tx pgx.Tx) (string, error) {
		id, err := s.reportRules.CreateRule(r.Context(), tx, actor.TenantID, in)
		if err != nil {
			if isUniqueViolation(err) {
				writeError(w, http.StatusConflict, "a rule with this code already exists")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		ruleID = id
		return id.String(), nil
	})
	if !committed {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": ruleID})
}

func (s *Server) handleUpdateReportRule(w http.ResponseWriter, r *http.Request) {
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
	var req reportRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	in, msg, ok := req.toInput()
	if !ok {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	committed := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "report_rule.updated", EventType: "ReportRuleUpdated", EntityType: "report_rule", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		if err := s.reportRules.UpdateRule(r.Context(), tx, actor.TenantID, id, in); errors.Is(err, reportrules.ErrNotFound) {
			writeError(w, http.StatusNotFound, "rule not found")
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
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) handleSetReportRuleEnabled(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	committed := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "report_rule.enabled", EventType: "ReportRuleEnabled", EntityType: "report_rule", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		if err := s.reportRules.SetRuleEnabled(r.Context(), tx, actor.TenantID, id, *req.Enabled); errors.Is(err, reportrules.ErrNotFound) {
			writeError(w, http.StatusNotFound, "rule not found")
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
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "enabled": *req.Enabled})
}

func (s *Server) handleDeleteReportRule(w http.ResponseWriter, r *http.Request) {
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
	committed := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "report_rule.deleted", EventType: "ReportRuleDeleted", EntityType: "report_rule", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		err := s.reportRules.DeleteRule(r.Context(), tx, actor.TenantID, id)
		if errors.Is(err, reportrules.ErrNotFound) {
			writeError(w, http.StatusNotFound, "rule not found")
			return "", err
		}
		if errors.Is(err, reportrules.ErrSystemImmutable) {
			writeError(w, http.StatusConflict, "a system rule cannot be deleted; disable it instead")
			return "", err
		}
		if err != nil {
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
