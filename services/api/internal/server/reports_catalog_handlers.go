package server

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
	"github.com/japharyroman/fuelgrid-os/internal/reportcatalog"
)

// Reports & Intelligence Center catalog (REPORTS-CATALOG, Phase 1).
//
// GET /api/v1/reports/catalog returns the 16 blueprint categories (§4.4) as
// DATA — read from the report_categories / reports tables seeded in migration
// 0105 — each annotated with: its availability (live | partial | placeholder),
// a live KEY METRIC where one is genuinely computable (a decimal STRING for
// money/litres, a count otherwise), an ALERT COUNT where computable, and the
// reports under it the actor may see. Plus a hub-level data-quality warnings
// band aggregated across categories.
//
// Honesty rules enforced here:
//   - Permission filtering: a category is only returned if the actor holds its
//     required_permission (or is a system admin). The catalog never lists a
//     surface the actor cannot reach (blueprint §3.4 / §14).
//   - Sensitive-metric gating: margin / supplier-cost / credit-exposure figures
//     are only attached when the actor holds margin.view; otherwise the metric
//     is omitted (null), never zeroed or faked.
//   - No fabrication: a category with no genuine tenant-wide source returns a
//     NULL metric and an honest reason string; a placeholder category (Tank
//     live-sensor, Custom, Scheduled) is always null with its unavailable reason.

// catalogMetric is a category's live key figure. Value is null when no genuine
// figure is computable (a partial/placeholder category, or a sensitive figure
// gated away); Reason then explains why, honestly.
type catalogMetric struct {
	Label  string  `json:"label"`
	Value  *string `json:"value"`            // decimal string (money/litres) or count; null = unavailable
	Unit   string  `json:"unit,omitempty"`   // e.g. "TZS", "L", "count"
	Reason string  `json:"reason,omitempty"` // why Value is null (honest, never a fake number)
}

// catalogReport is one report under a category that the actor may see.
type catalogReport struct {
	Key                string `json:"key"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	Endpoint           string `json:"endpoint"`
	RequiredPermission string `json:"required_permission"`
	Availability       string `json:"availability"`
}

// catalogCategory is one Reports Home card.
type catalogCategory struct {
	Key                string          `json:"key"`
	Name               string          `json:"name"`
	Description        string          `json:"description"`
	Icon               string          `json:"icon"`
	SortOrder          int             `json:"sort_order"`
	RequiredPermission string          `json:"required_permission"`
	Availability       string          `json:"availability"`
	TargetRoute        string          `json:"target_route"`
	Metric             catalogMetric   `json:"metric"`
	AlertCount         int             `json:"alert_count"`
	Reports            []catalogReport `json:"reports"`
}

// catalogDataQuality is one hub-level data-quality warning.
type catalogDataQuality struct {
	CategoryKey string `json:"category_key"`
	Level       string `json:"level"` // info | warning
	Message     string `json:"message"`
}

// reportCatalogResponse is the GET /reports/catalog payload.
type reportCatalogResponse struct {
	GeneratedAt string               `json:"generated_at"`
	Categories  []catalogCategory    `json:"categories"`
	DataQuality []catalogDataQuality `json:"data_quality"`
}

// handleReportCatalog returns the permission-filtered report catalog with live
// metrics and a hub-level data-quality band. Gated by reports.read at the route;
// every category is additionally filtered by its own required_permission.
func (s *Server) handleReportCatalog(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ctx := r.Context()

	ps, err := s.policy.LoadFor(ctx, actor)
	if err != nil {
		s.logger.Error("report catalog: policy load", "error", err)
		writeError(w, http.StatusInternalServerError, "authorization error")
		return
	}

	cats, err := s.reportCatalog.ListCategories(ctx, actor.TenantID)
	if err != nil {
		s.logger.Error("report catalog: list categories", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	allReports, err := s.reportCatalog.ListReports(ctx, actor.TenantID)
	if err != nil {
		s.logger.Error("report catalog: list reports", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	reportsByCat := map[string][]catalogReport{}
	for i := range allReports {
		rep := allReports[i]
		if !canViewPermission(ps, rep.RequiredPermission) {
			continue
		}
		reportsByCat[rep.CategoryKey] = append(reportsByCat[rep.CategoryKey], catalogReport{
			Key: rep.Key, Name: rep.Name, Description: rep.Description,
			Endpoint: rep.Endpoint, RequiredPermission: rep.RequiredPermission,
			Availability: rep.Availability,
		})
	}

	// Shared figures, computed ONCE and reused across the cards that need them.
	figs := s.computeCatalogFigures(ctx, actor.TenantID, ps)

	out := reportCatalogResponse{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Categories:  []catalogCategory{},
		DataQuality: []catalogDataQuality{},
	}
	for i := range cats {
		c := cats[i]
		// Permission filter: only surface a category the actor can reach.
		if !canViewPermission(ps, c.RequiredPermission) {
			continue
		}
		reports := reportsByCat[c.Key]
		if reports == nil {
			reports = []catalogReport{}
		}
		metric, alerts := categoryMetric(c, figs)
		out.Categories = append(out.Categories, catalogCategory{
			Key: c.Key, Name: c.Name, Description: c.Description, Icon: c.Icon,
			SortOrder: c.SortOrder, RequiredPermission: c.RequiredPermission,
			Availability: c.Availability, TargetRoute: c.TargetRoute,
			Metric: metric, AlertCount: alerts, Reports: reports,
		})
		// Hub data-quality: surface an honest warning per non-live category the
		// actor can see, plus a real open-alerts warning where alerts exist.
		out.DataQuality = append(out.DataQuality, categoryDataQuality(c, alerts)...)
	}
	writeJSON(w, http.StatusOK, out)
}

// canViewPermission reports whether the actor may see a surface gated by perm.
// A system admin sees everything; otherwise the actor must hold the permission
// (scope is not demanded here — the catalog is a tenant-wide listing, and the
// per-report endpoints re-check station scope on access).
func canViewPermission(ps policy.PermissionSet, perm string) bool {
	if ps.IsSystemAdmin {
		return true
	}
	return ps.HasPermission(perm)
}

// catalogFigures holds the tenant-wide aggregates the hub cards draw on, fetched
// once per request. Sensitive figures are only populated when the actor is
// permitted to see them; marginAllowed records that decision so each card can
// decide whether to attach a margin/exposure figure.
type catalogFigures struct {
	openAlerts    int
	arExposure    string // receivables outstanding (decimal string) — sensitive
	arHasData     bool
	salesGross    string // revenue_days gross over window (decimal string)
	salesMargin   string // revenue_days margin (decimal string) — sensitive
	salesHasData  bool
	payablesTotal string // payables outstanding (decimal string) — sensitive
	payablesOpen  int
	deliveryCount int
	auditEvents   int
	exportCount   int
	marginAllowed bool
}

// computeCatalogFigures gathers every tenant-wide aggregate the cards may need,
// in one pass. A failed aggregate logs and leaves its field at the zero value
// (an honest "no data") rather than failing the whole catalog.
func (s *Server) computeCatalogFigures(ctx context.Context, tenantID uuid.UUID, ps policy.PermissionSet) catalogFigures {
	figs := catalogFigures{marginAllowed: canViewPermission(ps, "margin.view")}

	if alerts, err := s.risk.ListAlerts(ctx, tenantID, "open", ""); err == nil {
		figs.openAlerts = len(alerts)
	} else {
		s.logger.Error("report catalog: risk alerts", "error", err)
	}

	if rows, err := s.receivables.Aging(ctx, tenantID); err == nil {
		var sum float64
		for i := range rows {
			if v, ok := parseFloatSafe(rows[i].Balance); ok && v > 0 {
				sum += v
				figs.arHasData = true
			}
		}
		figs.arExposure = strconv.FormatFloat(sum, 'f', 2, 64)
	} else {
		s.logger.Error("report catalog: receivables aging", "error", err)
	}

	now := time.Now().UTC()
	from := now.AddDate(0, 0, -30)
	if roll, err := s.reportCatalog.SalesRollup(ctx, tenantID, from, now); err == nil {
		figs.salesGross = roll.GrossRevenue
		figs.salesMargin = roll.MarginTotal
		figs.salesHasData = roll.DayCount > 0
	} else {
		s.logger.Error("report catalog: sales rollup", "error", err)
	}

	if tot, err := s.payables.AgingTotals(ctx, tenantID); err == nil {
		figs.payablesTotal = tot.Outstanding
		figs.payablesOpen = tot.OpenCount
	} else {
		s.logger.Error("report catalog: payables aging", "error", err)
	}

	if n, err := s.reportCatalog.DeliveryCount(ctx, tenantID, from, now); err == nil {
		figs.deliveryCount = n
	} else {
		s.logger.Error("report catalog: delivery count", "error", err)
	}
	if n, err := s.reportCatalog.AuditEventCount(ctx, tenantID, from, now); err == nil {
		figs.auditEvents = n
	} else {
		s.logger.Error("report catalog: audit count", "error", err)
	}
	if n, err := s.reportCatalog.ExportCount(ctx, tenantID); err == nil {
		figs.exportCount = n
	} else {
		s.logger.Error("report catalog: export count", "error", err)
	}
	return figs
}

// categoryMetric resolves the live key metric + alert count for a category,
// honouring availability and sensitive-metric gating. A placeholder category is
// always (null, reason). A sensitive figure (margin / supplier cost / credit
// exposure) is omitted with an honest reason when margin.view is absent (carried
// on f.marginAllowed, decided once when the figures were computed).
func categoryMetric(c reportcatalog.Category, f catalogFigures) (catalogMetric, int) {
	if c.Availability == "placeholder" {
		return catalogMetric{Label: placeholderLabel(c.Key), Value: nil, Reason: placeholderReason(c.Key)}, 0
	}

	switch c.Key {
	case "executive":
		// Executive rollup: gross revenue (last 30d) with open-alert context.
		if !f.salesHasData {
			return catalogMetric{Label: "Gross revenue (30d)", Value: nil, Unit: "TZS",
				Reason: "No locked or draft revenue days in the last 30 days yet."}, f.openAlerts
		}
		v := f.salesGross
		return catalogMetric{Label: "Gross revenue (30d)", Value: &v, Unit: "TZS"}, f.openAlerts

	case "sales":
		if !f.salesHasData {
			return catalogMetric{Label: "Gross revenue (30d)", Value: nil, Unit: "TZS",
				Reason: "No revenue days recorded in the last 30 days yet."}, 0
		}
		v := f.salesGross
		return catalogMetric{Label: "Gross revenue (30d)", Value: &v, Unit: "TZS"}, 0

	case "finance":
		// Finance headline is supplier-cost-sensitive (payables outstanding).
		if !f.marginAllowed {
			return catalogMetric{Label: "Outstanding payables", Value: nil, Unit: "TZS",
				Reason: "Requires margin.view to see supplier cost / payables exposure."}, 0
		}
		v := f.payablesTotal
		return catalogMetric{Label: "Outstanding payables", Value: &v, Unit: "TZS"}, 0

	case "procurement":
		// Open payables count is non-sensitive; the value is sensitive.
		if !f.marginAllowed {
			return catalogMetric{Label: "Open payables", Value: nil, Unit: "count",
				Reason: "Requires margin.view to see supplier cost / payables exposure."}, 0
		}
		v := strconv.Itoa(f.payablesOpen)
		return catalogMetric{Label: "Open payables", Value: &v, Unit: "count"}, 0

	case "customer-credit":
		// Credit exposure is sensitive (margin.view-gated).
		if !f.marginAllowed {
			return catalogMetric{Label: "Credit exposure", Value: nil, Unit: "TZS",
				Reason: "Requires margin.view to see credit exposure."}, 0
		}
		if !f.arHasData {
			return catalogMetric{Label: "Credit exposure", Value: nil, Unit: "TZS",
				Reason: "No outstanding credit-customer balances."}, 0
		}
		v := f.arExposure
		return catalogMetric{Label: "Credit exposure", Value: &v, Unit: "TZS"}, 0

	case "risk-loss":
		v := strconv.Itoa(f.openAlerts)
		return catalogMetric{Label: "Open risk alerts", Value: &v, Unit: "count"}, f.openAlerts

	case "inventory":
		v := strconv.Itoa(f.openAlerts)
		return catalogMetric{Label: "Open alerts", Value: &v, Unit: "count"}, f.openAlerts

	case "delivery":
		v := strconv.Itoa(f.deliveryCount)
		return catalogMetric{Label: "Deliveries (30d)", Value: &v, Unit: "count"}, 0

	case "audit":
		v := strconv.Itoa(f.auditEvents)
		return catalogMetric{Label: "Audit events (30d)", Value: &v, Unit: "count"}, 0

	case "export-history":
		v := strconv.Itoa(f.exportCount)
		return catalogMetric{Label: "Exports recorded", Value: &v, Unit: "count"}, 0

	case "shift":
		// Shift close detail is station-scoped; no honest tenant rollup exists.
		return catalogMetric{Label: "Shift close", Value: nil,
			Reason: "Per-station report — open a station to see shift close figures."}, 0

	case "pump":
		return catalogMetric{Label: "Pump throughput", Value: nil,
			Reason: "Per-station report — pump throughput is computed per station."}, 0

	case "fleet":
		return catalogMetric{Label: "Fleet consumption", Value: nil,
			Reason: "Per-customer report — fleet consumption is computed per credit customer."}, 0
	}

	return catalogMetric{Label: "Metric", Value: nil, Reason: "No tenant-wide metric for this category yet."}, 0
}

// categoryDataQuality emits the hub-level data-quality warnings for a category:
// an honest note for any non-live category, and an open-alerts warning where the
// category carries open risk alerts.
func categoryDataQuality(c reportcatalog.Category, alerts int) []catalogDataQuality {
	var out []catalogDataQuality
	switch c.Availability {
	case "placeholder":
		out = append(out, catalogDataQuality{
			CategoryKey: c.Key, Level: "info", Message: c.Name + ": " + placeholderReason(c.Key),
		})
	case "partial":
		out = append(out, catalogDataQuality{
			CategoryKey: c.Key, Level: "info",
			Message: c.Name + ": tenant-wide metric not yet wired — open the report for full figures.",
		})
	}
	if alerts > 0 && (c.Key == "risk-loss" || c.Key == "inventory") {
		out = append(out, catalogDataQuality{
			CategoryKey: c.Key, Level: "warning",
			Message: c.Name + ": " + strconv.Itoa(alerts) + " open risk alert(s) need review.",
		})
	}
	return out
}

// placeholderLabel / placeholderReason give the honest unavailable copy for the
// categories with no backing data yet.
func placeholderLabel(key string) string {
	switch key {
	case "tank":
		return "Live tank telemetry"
	case "custom":
		return "Custom report builder"
	case "scheduled":
		return "Scheduled reports"
	}
	return "Unavailable"
}

func placeholderReason(key string) string {
	switch key {
	case "tank":
		return "No ATG / sensor feed connected — live tank telemetry is not available."
	case "custom":
		return "The custom report builder is not built yet."
	case "scheduled":
		return "Per-tenant scheduled reports are not built yet."
	}
	return "Not available yet."
}
