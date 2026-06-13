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
		// Hub data-quality: surface an honest note per non-live category the actor
		// can see, plus a real open-alerts warning where alerts exist.
		out.DataQuality = append(out.DataQuality, categoryDataQuality(c, metric, figs)...)
	}
	writeJSON(w, http.StatusOK, out)
}

// canViewPermission reports whether the actor may see a CARD/REPORT gated by
// perm. A system admin sees everything; otherwise the actor must hold the
// permission. This is the coarse listing gate: scope is not demanded here, since
// the per-report endpoints re-check station scope on access (an out-of-scope
// station 403s in-handler). It governs only *whether a card is listed*, never
// whether a tenant-wide metric value is attached to it — that is the stricter
// canSeeTenantWideMetric gate below.
func canViewPermission(ps policy.PermissionSet, perm string) bool {
	if ps.IsSystemAdmin {
		return true
	}
	return ps.HasPermission(perm)
}

// canSeeTenantWideMetric reports whether the actor may see a TENANT-WIDE
// aggregate figure gated by perm — a strictly stronger test than
// canViewPermission. A metric on a hub card sums data across every station; a
// station-scoped actor who only holds perm for their own stations must not see
// the tenant-wide total (that would leak other stations' revenue / deliveries /
// alerts they cannot otherwise read). So a tenant-wide value is shown only when:
//
//   - the actor is a system admin; or
//   - perm is a tenant-wide permission (scope is irrelevant — every holder sees
//     the whole tenant by definition); or
//   - perm is station-scoped but the actor has tenant-wide reach (TenantWide).
//
// A station-scoped actor holding a station-scoped perm gets a null metric with
// an honest reason instead of a cross-station total.
func canSeeTenantWideMetric(ps policy.PermissionSet, perm string) bool {
	if ps.IsSystemAdmin {
		return true
	}
	if !ps.HasPermission(perm) {
		return false
	}
	if !ps.StationScoped[perm] {
		// Tenant-wide permission: every holder has tenant-wide reach.
		return true
	}
	// Station-scoped permission: only a tenant-wide actor sees the whole tenant.
	return ps.TenantWide
}

// catalogFigures holds the tenant-wide aggregates the hub cards draw on, fetched
// once per request, plus the per-metric scope decisions made once when the
// figures were computed. A tenant-wide aggregate is only ATTACHED to a card when
// the actor has tenant-wide authority over the gating permission (see
// canSeeTenantWideMetric); the *Visible flags record that decision so each card
// can suppress a cross-station total for a station-scoped actor with a null +
// honest reason rather than leak other stations' figures.
type catalogFigures struct {
	openAlerts     int
	inventoryAlert int    // open fuel-variance (inventory) alerts only
	arExposure     string // receivables outstanding (decimal string) — sensitive
	arHasData      bool
	salesGross     string // revenue_days gross over window (decimal string)
	salesMargin    string // revenue_days margin (decimal string) — sensitive
	salesHasData   bool
	payablesTotal  string // payables outstanding (decimal string) — sensitive
	payablesOpen   int
	deliveryCount  int
	auditEvents    int
	exportCount    int

	marginAllowed bool // holds margin.view with tenant-wide reach over it

	// Tenant-wide metric visibility per station-scoped gating permission. False
	// for a station-scoped actor → the card shows a null metric + honest reason
	// instead of a cross-station total.
	salesVisible     bool // revenue.read (sales / executive gross)
	deliveryVisible  bool // station.read (delivery count)
	inventoryVisible bool // reconciliation.read (inventory alert count)
}

// computeCatalogFigures gathers every tenant-wide aggregate the cards may need,
// in one pass. A failed aggregate logs and leaves its field at the zero value
// (an honest "no data") rather than failing the whole catalog.
func (s *Server) computeCatalogFigures(ctx context.Context, tenantID uuid.UUID, ps policy.PermissionSet) catalogFigures {
	figs := catalogFigures{
		// margin.view is itself station-scoped; a sensitive tenant-wide money
		// figure is only honest for an actor with tenant-wide reach over it.
		marginAllowed:    canSeeTenantWideMetric(ps, "margin.view"),
		salesVisible:     canSeeTenantWideMetric(ps, "revenue.read"),
		deliveryVisible:  canSeeTenantWideMetric(ps, "station.read"),
		inventoryVisible: canSeeTenantWideMetric(ps, "reconciliation.read"),
	}

	if alerts, err := s.risk.ListAlerts(ctx, tenantID, "open", ""); err == nil {
		figs.openAlerts = len(alerts)
	} else {
		s.logger.Error("report catalog: risk alerts", "error", err)
	}

	// Inventory alerts are ONLY the fuel-variance (tank reconciliation) alert
	// type — not attendant / supplier / stockout alerts that ListAlerts("") also
	// returns. The inventory card and its DQ warning must reflect inventory only.
	if alerts, err := s.risk.ListAlerts(ctx, tenantID, "open", "fuel_variance_over_tolerance"); err == nil {
		figs.inventoryAlert = len(alerts)
	} else {
		s.logger.Error("report catalog: inventory alerts", "error", err)
	}

	if v, hasData, err := s.reportCatalog.ReceivablesExposure(ctx, tenantID); err == nil {
		figs.arExposure = v
		figs.arHasData = hasData
	} else {
		s.logger.Error("report catalog: receivables exposure", "error", err)
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

// stationScopedReason explains, honestly, why a tenant-wide figure is withheld
// from a station-scoped actor. The figure exists but summing it across the whole
// tenant would disclose stations the actor cannot read; the per-station report is
// the right place for them to see their own station's number.
const stationScopedReason = "Tenant-wide figure is hidden for a station-scoped role — open a station report for your own stations."

// categoryMetric resolves the live key metric + the alert count a category
// CONTRIBUTES TO THE HERO for a category, honouring availability, sensitive-metric
// gating, and station scope.
//
//   - A placeholder category is always (null, reason).
//   - A sensitive figure (margin / supplier cost / credit exposure) is omitted
//     with an honest reason when the actor lacks tenant-wide margin.view
//     (f.marginAllowed, decided once when the figures were computed).
//   - A tenant-wide aggregate gated by a STATION-SCOPED permission (sales /
//     executive gross via revenue.read; delivery count via station.read;
//     inventory alerts via reconciliation.read) is withheld from a station-scoped
//     actor (the *Visible flags) — the tenant-wide total would leak other
//     stations' figures. Such an actor gets a null metric + honest reason.
//   - The returned int is what the category contributes to the hero "open report
//     alerts" total. Only risk-loss OWNS that total (it is the tenant-wide open
//     risk-alert count); every other category returns 0 so the hero is not
//     double/triple-counted. The inventory card still surfaces its own
//     (inventory-only) alert count as its METRIC VALUE, just not in the hero sum.
func categoryMetric(c reportcatalog.Category, f catalogFigures) (catalogMetric, int) {
	if c.Availability == "placeholder" {
		return catalogMetric{Label: placeholderLabel(c.Key), Value: nil, Reason: placeholderReason(c.Key)}, 0
	}

	switch c.Key {
	case "executive":
		// Executive rollup: gross revenue (last 30d). Gated by revenue.read,
		// which is station-scoped — withhold the tenant-wide total from a
		// station-scoped actor.
		if !f.salesVisible {
			return catalogMetric{Label: "Gross revenue (30d)", Value: nil, Unit: "TZS",
				Reason: stationScopedReason}, 0
		}
		if !f.salesHasData {
			return catalogMetric{Label: "Gross revenue (30d)", Value: nil, Unit: "TZS",
				Reason: "No locked or draft revenue days in the last 30 days yet."}, 0
		}
		v := f.salesGross
		return catalogMetric{Label: "Gross revenue (30d)", Value: &v, Unit: "TZS"}, 0

	case "sales":
		if !f.salesVisible {
			return catalogMetric{Label: "Gross revenue (30d)", Value: nil, Unit: "TZS",
				Reason: stationScopedReason}, 0
		}
		if !f.salesHasData {
			return catalogMetric{Label: "Gross revenue (30d)", Value: nil, Unit: "TZS",
				Reason: "No revenue days recorded in the last 30 days yet."}, 0
		}
		v := f.salesGross
		return catalogMetric{Label: "Gross revenue (30d)", Value: &v, Unit: "TZS"}, 0

	case "finance":
		// Finance headline is supplier-cost-sensitive (payables outstanding).
		// finance.read is tenant-wide, so the only gate here is margin.view.
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
		// risk.read is tenant-wide, so the tenant-wide open-alert count is honest
		// for every holder. This is the ONE category that owns the hero alert
		// total.
		v := strconv.Itoa(f.openAlerts)
		return catalogMetric{Label: "Open risk alerts", Value: &v, Unit: "count"}, f.openAlerts

	case "inventory":
		// Inventory's metric is the INVENTORY-ONLY open-alert count (fuel
		// variance), gated by reconciliation.read which is station-scoped — a
		// station-scoped actor gets a null metric. The count is NOT added to the
		// hero total (risk-loss owns that) to avoid double counting.
		if !f.inventoryVisible {
			return catalogMetric{Label: "Open inventory alerts", Value: nil, Unit: "count",
				Reason: stationScopedReason}, 0
		}
		v := strconv.Itoa(f.inventoryAlert)
		return catalogMetric{Label: "Open inventory alerts", Value: &v, Unit: "count"}, 0

	case "delivery":
		// Delivery count is gated by station.read (station-scoped); withhold the
		// tenant-wide total from a station-scoped actor.
		if !f.deliveryVisible {
			return catalogMetric{Label: "Deliveries (30d)", Value: nil, Unit: "count",
				Reason: stationScopedReason}, 0
		}
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

// categoryDataQuality emits the hub-level data-quality notes for a category:
//   - an honest "coming soon" note for any placeholder category;
//   - a "not yet wired" note for a partial category ONLY when no metric value is
//     actually shown (a partial category that returns a live value — executive /
//     sales — must not contradict itself with a "not wired" note);
//   - an inventory-alert warning when the category carries open inventory alerts
//     (scoped to the inventory-only count, only when the actor may see it).
func categoryDataQuality(c reportcatalog.Category, m catalogMetric, f catalogFigures) []catalogDataQuality {
	var out []catalogDataQuality
	switch c.Availability {
	case "placeholder":
		out = append(out, catalogDataQuality{
			CategoryKey: c.Key, Level: "info", Message: c.Name + ": " + placeholderReason(c.Key),
		})
	case "partial":
		if m.Value == nil {
			out = append(out, catalogDataQuality{
				CategoryKey: c.Key, Level: "info",
				Message: c.Name + ": tenant-wide metric not yet wired — open the report for full figures.",
			})
		}
	}
	// Open-alert warnings: risk-loss owns the tenant-wide open-alert count;
	// inventory warns only on its own (inventory) alerts and only when the actor
	// may see that count (otherwise the warning would itself leak a cross-station
	// total to a station-scoped role).
	if c.Key == "risk-loss" && f.openAlerts > 0 {
		out = append(out, catalogDataQuality{
			CategoryKey: c.Key, Level: "warning",
			Message: c.Name + ": " + strconv.Itoa(f.openAlerts) + " open risk alert(s) need review.",
		})
	}
	if c.Key == "inventory" && f.inventoryVisible && f.inventoryAlert > 0 {
		out = append(out, catalogDataQuality{
			CategoryKey: c.Key, Level: "warning",
			Message: c.Name + ": " + strconv.Itoa(f.inventoryAlert) + " open inventory alert(s) need review.",
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
