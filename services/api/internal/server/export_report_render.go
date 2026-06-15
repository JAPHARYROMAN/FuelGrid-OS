package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
	"github.com/japharyroman/fuelgrid-os/internal/reporting"
)

// Async export rendering (Reports Center Phase 13 — Export Center).
//
// This file is the worker's report registry: given a queued export job (a
// report_key + filters + the requesting actor), it (1) RE-CHECKS the actor's
// permission AT GENERATION TIME — a user who lost access between enqueue and run
// must not receive the data — then (2) builds the report's ReportEnvelope from
// the SAME repos the live structured endpoints use (every money/litre figure is
// the exact decimal string the repos return; nothing is recomputed in float),
// and (3) renders the envelope into the requested CSV/PDF/XLSX bytes by reusing
// the merged renderers (encoding/csv, the premium fpdf layout, buildWorkbook).
//
// The registry intentionally covers the report keys the export surface already
// supports (the same set buildExportURL maps); an unknown key fails the job with
// a clear reason rather than producing an empty file.

// errExportForbidden marks a job whose actor no longer holds the required
// permission at generation time. The worker turns it into a 'failed' job with a
// forbidden reason — never a file — so revoked access can never leak data.
var errExportForbidden = errors.New("export: actor not permitted to view this report")

// errExportUnsupported marks a report_key/format combination the worker cannot
// render. Mirrors buildExportURL's "unsupported" rejection but at render time.
var errExportUnsupported = errors.New("export: unsupported report_key/format combination")

// reportSpec describes how to authorize and build one report's envelope. perm is
// the permission the actor must hold; when stationScoped is true the check is run
// against the filters' station_id (and that station must be present).
type reportSpec struct {
	perm          string
	stationScoped bool
	build         func(ctx context.Context, s *Server, actor identity.Actor, filters map[string]string) (ReportEnvelope, error)
}

// reportSpecFor resolves the spec for a report key, normalising the aliases the
// export surface accepts onto a single builder.
func reportSpecFor(reportKey string) (reportSpec, bool) {
	switch reportKey {
	case "revenue", "station-close", "sales":
		return reportSpec{perm: "revenue.read", stationScoped: true, build: buildStationCloseEnvelope}, true
	case "reconciliation", "inventory-reconciliation":
		// NOTE: the "inventory" and "delivery" aliases are deliberately NOT routed
		// here. On the legacy export surface those keys map (via buildExportURL) to
		// the station INVENTORY snapshot (inventory.csv), gated by inventory.read —
		// a different report AND a different permission than reconciliation. Routing
		// them to the reconciliation builder would silently change both the data and
		// the authorization for back-compat callers, so they fall through to the
		// legacy receipt path unchanged.
		return reportSpec{perm: "reconciliation.read", stationScoped: true, build: buildReconciliationEnvelope}, true
	case "financials":
		return reportSpec{perm: "finance.read", stationScoped: false, build: buildFinancialsEnvelope}, true
	case "ar-aging", "customer-aging", "receivables":
		return reportSpec{perm: "customer.read", stationScoped: false, build: buildReceivablesEnvelope}, true
	}
	return reportSpec{}, false
}

// renderExportJob is the worker's per-job pipeline: authorize, build, render.
// It returns the rendered bytes, the content type, the download filename and a
// sha256 hex checksum, or an error. errExportForbidden / errExportUnsupported
// are surfaced verbatim so the worker can record a precise failure reason.
func (s *Server) renderExportJob(
	ctx context.Context, actor identity.Actor, reportKey, format string, filters map[string]string,
) (data []byte, contentType, filename, checksum string, err error) {
	spec, ok := reportSpecFor(reportKey)
	if !ok {
		return nil, "", "", "", errExportUnsupported
	}

	// (1) Permission re-check AT GENERATION. The authoritative policy evaluator
	// runs with just the actor + context (no HTTP request), exactly like the
	// request middleware, so a revoked grant fails here.
	resource := policy.Resource{}
	if spec.stationScoped {
		sid, perr := uuid.Parse(strings.TrimSpace(filters["station_id"]))
		if perr != nil {
			return nil, "", "", "", fmt.Errorf("export: station_id required for %s", reportKey)
		}
		resource = policy.AtStation(sid)
	}
	if cerr := s.policy.Can(ctx, actor, spec.perm, resource); cerr != nil {
		if errors.Is(cerr, policy.ErrForbidden) {
			return nil, "", "", "", errExportForbidden
		}
		return nil, "", "", "", fmt.Errorf("export: permission check: %w", cerr)
	}

	// (2) Build the envelope from the live repos.
	env, berr := spec.build(ctx, s, actor, filters)
	if berr != nil {
		return nil, "", "", "", fmt.Errorf("export: build %s: %w", reportKey, berr)
	}

	// (3) Render the requested format.
	switch format {
	case "csv":
		data = renderEnvelopeCSV(env)
		contentType = "text/csv; charset=utf-8"
	case "xlsx":
		data, err = renderEnvelopeXLSX(env)
		contentType = xlsxContentType
	case "pdf":
		preparedBy := s.exportPreparedBy(ctx, actor)
		branding := s.loadLetterheadFor(ctx, actor.TenantID)
		data, err = renderEnvelopePDF(env, branding, preparedBy, time.Now().UTC())
		contentType = "application/pdf"
	default:
		return nil, "", "", "", errExportUnsupported
	}
	if err != nil {
		return nil, "", "", "", fmt.Errorf("export: render %s.%s: %w", reportKey, format, err)
	}

	sum := sha256.Sum256(data)
	checksum = hex.EncodeToString(sum[:])
	filename = exportJobFileName(reportKey, format)
	return data, contentType, filename, checksum, nil
}

// exportPreparedBy resolves a human "prepared by" label for the actor (full name,
// else email, else the user id) for the PDF signature line. Never fatal — a
// lookup failure degrades to the user id.
func (s *Server) exportPreparedBy(ctx context.Context, actor identity.Actor) string {
	if s.userRepo == nil {
		return actor.UserID.String()
	}
	u, err := s.userRepo.FindByID(ctx, actor.TenantID, actor.UserID)
	if err != nil || u == nil {
		return actor.UserID.String()
	}
	if strings.TrimSpace(u.FullName) != "" {
		return u.FullName
	}
	if strings.TrimSpace(u.Email) != "" {
		return u.Email
	}
	return actor.UserID.String()
}

// loadLetterheadFor is the request-free counterpart of loadLetterhead: it loads
// the tenant branding (text + logo bytes) for the worker, which has no
// *http.Request. A nil branding repo or a load error degrades to the empty
// (FuelGrid-only) letterhead, so a missing tenant_branding row renders fine.
func (s *Server) loadLetterheadFor(ctx context.Context, tenantID uuid.UUID) LetterheadBranding {
	if s.branding == nil {
		return LetterheadBranding{}
	}
	b, err := s.branding.Get(ctx, tenantID)
	if err != nil {
		s.logger.Error("export worker: load branding", "error", err, "tenant_id", tenantID)
		return LetterheadBranding{}
	}
	lb := LetterheadBranding{
		DisplayName:    b.DisplayName,
		LegalName:      b.LegalName,
		TaxID:          b.TaxID,
		RegistrationNo: b.RegistrationNo,
		AddressLine1:   b.AddressLine1,
		AddressLine2:   b.AddressLine2,
		City:           b.City,
		Country:        b.Country,
		Phone:          b.Phone,
		Email:          b.Email,
		Website:        b.Website,
		FooterNote:     b.FooterNote,
	}
	if b.HasLogo {
		data, ct, found, lerr := s.branding.GetLogo(ctx, tenantID)
		if lerr != nil {
			s.logger.Error("export worker: load logo", "error", lerr, "tenant_id", tenantID)
		} else if found {
			lb.Logo = data
			lb.LogoContentType = ct
		}
	}
	return lb
}

// ---- Envelope builders (worker-side, request-free) ----
//
// Each builder mirrors the live structured handler's figures for the report,
// reading from the same repos and carrying exact decimal strings. They are
// deliberately compact (KPI + table) — the file is a faithful snapshot, not the
// full interactive page.

// buildStationCloseEnvelope builds the Daily Station Close snapshot for the
// filters' station: the latest revenue day's headline figures + a recent-days
// trend table. Same data as handleStationCloseReport / the daily-close PDF.
func buildStationCloseEnvelope(ctx context.Context, s *Server, actor identity.Actor, filters map[string]string) (ReportEnvelope, error) {
	stationID, err := uuid.Parse(strings.TrimSpace(filters["station_id"]))
	if err != nil {
		return ReportEnvelope{}, fmt.Errorf("invalid station_id")
	}
	station, serr := s.stations.Get(ctx, actor.TenantID, stationID)
	if errors.Is(serr, pgx.ErrNoRows) {
		return ReportEnvelope{}, fmt.Errorf("station not found")
	}
	if serr != nil {
		return ReportEnvelope{}, serr
	}
	sid := stationID.String()
	period := filters["period"]
	if period == "" {
		period = "current"
	}
	env := newEnvelope("station-close", "Daily Station Close — "+station.Name, period, &sid)
	env.FiltersUsed["station_id"] = sid
	env.FiltersUsed["station"] = station.Code

	days, derr := s.revenue.RecentDays(ctx, actor.TenantID, stationID, 30)
	if derr != nil {
		return ReportEnvelope{}, derr
	}
	if len(days) > 0 {
		d := days[0]
		env.FiltersUsed["business_date"] = d.BusinessDate.Format(dateLayout)
		env.Summary = []summaryMetric{
			{Label: "Sales value", Value: d.GrossRevenue, Unit: "TZS"},
			{Label: "Net revenue", Value: d.NetRevenue, Unit: "TZS"},
			{Label: "Margin", Value: d.MarginTotal, Unit: "TZS"},
			{Label: "Total tendered", Value: d.TenderTotal, Unit: "TZS"},
			{Label: "Cash variance", Value: d.CashVariance, Unit: "TZS"},
			{Label: "Status", Value: d.Status},
		}
	} else {
		env.Summary = []summaryMetric{{Label: "Status", Value: "no_data"}}
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level: "warning", Message: "No revenue day has been computed for this station yet — close figures are unavailable.",
		})
	}

	env.Table.Columns = []string{"business_date", "status", "gross", "net", "margin", "tendered", "cash_variance"}
	for i := range days {
		d := days[i]
		env.Table.Rows = append(env.Table.Rows, []string{
			d.BusinessDate.Format(dateLayout), d.Status, d.GrossRevenue, d.NetRevenue,
			d.MarginTotal, d.TenderTotal, d.CashVariance,
		})
	}
	if len(days) > 0 {
		env.Insights = append(env.Insights, reporting.Insight{
			Severity: reporting.SeverityInfo,
			Message:  fmt.Sprintf("Snapshot covers %d recent revenue day(s); latest business date %s.", len(days), days[0].BusinessDate.Format(dateLayout)),
		})
	}
	return env, nil
}

// buildReconciliationEnvelope builds the inventory reconciliation snapshot for
// the filters' station's resolved day: the per-tank book-vs-physical lines. Same
// data as handleReconciliationReport.
func buildReconciliationEnvelope(ctx context.Context, s *Server, actor identity.Actor, filters map[string]string) (ReportEnvelope, error) {
	stationID, err := uuid.Parse(strings.TrimSpace(filters["station_id"]))
	if err != nil {
		return ReportEnvelope{}, fmt.Errorf("invalid station_id")
	}
	station, serr := s.stations.Get(ctx, actor.TenantID, stationID)
	if errors.Is(serr, pgx.ErrNoRows) {
		return ReportEnvelope{}, fmt.Errorf("station not found")
	}
	if serr != nil {
		return ReportEnvelope{}, serr
	}
	sid := stationID.String()
	period := filters["period"]
	if period == "" {
		period = "current"
	}
	env := newEnvelope("inventory-reconciliation", "Inventory Reconciliation — "+station.Name, period, &sid)
	env.FiltersUsed["station_id"] = sid
	env.FiltersUsed["station"] = station.Code

	// Resolve the day: explicit operating_day_id, else the latest active day.
	var dayID uuid.UUID
	if raw := strings.TrimSpace(filters["operating_day_id"]); raw != "" {
		if id, perr := uuid.Parse(raw); perr == nil {
			if day, gerr := s.operations.GetDay(ctx, actor.TenantID, id); gerr == nil {
				dayID = day.ID
				env.FiltersUsed["business_date"] = day.BusinessDate.Format(dateLayout)
			}
		}
	}
	if dayID == uuid.Nil {
		if day, gerr := s.operations.LatestActiveDayForStation(ctx, actor.TenantID, stationID); gerr == nil {
			dayID = day.ID
			env.FiltersUsed["business_date"] = day.BusinessDate.Format(dateLayout)
		}
	}

	env.Table.Columns = []string{
		"tank", "product", "opening", "deliveries", "sales", "adjustments",
		"expected_closing", "actual_closing", "variance", "variance_pct", "tolerance", "status",
	}
	var reconciled int
	if dayID != uuid.Nil {
		raw, lerr := s.reconciliation.ListForStationDayWithProduct(ctx, actor.TenantID, stationID, dayID)
		if lerr != nil {
			return ReportEnvelope{}, lerr
		}
		for i := range raw {
			rc := reconLineFromStationDay(raw[i])
			env.Table.Rows = append(env.Table.Rows, []string{
				rc.TankLabel, rc.ProductName, rc.OpeningBook, rc.DeliveriesTotal, rc.SalesTotal,
				rc.AdjustmentsTotal, rc.ExpectedClosing, rc.ClosingPhysical, rc.VarianceLitres,
				rc.VariancePercent, rc.TolerancePercent, rc.Status,
			})
			reconciled++
		}
	}
	env.Summary = []summaryMetric{{Label: "Tanks reconciled", Value: itoa(reconciled), Unit: "count"}}
	if reconciled == 0 {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level: "warning", Message: "No tanks have been reconciled for this station's active day yet — reconciliation figures are unavailable.",
		})
	}
	return env, nil
}

// buildFinancialsEnvelope builds the tenant-wide financial statement snapshot for
// the filters' ?period window: the P&L and balance-sheet headline figures. Same
// data as handleExportFinancialsPDF.
func buildFinancialsEnvelope(ctx context.Context, s *Server, actor identity.Actor, filters map[string]string) (ReportEnvelope, error) {
	from, to, label := resolveReportPeriod(filters["period"], time.Now())
	env := newEnvelope("financials", "Financial Statement", label, nil)
	env.FiltersUsed["period"] = label
	env.FiltersUsed["from"] = from.Format(dateLayout)
	env.FiltersUsed["to"] = to.Format(dateLayout)

	is, err := s.accounting.IncomeStatement(ctx, actor.TenantID, from, to)
	if err != nil {
		return ReportEnvelope{}, err
	}
	bs, err := s.accounting.BalanceSheet(ctx, actor.TenantID, to)
	if err != nil {
		return ReportEnvelope{}, err
	}
	env.Summary = []summaryMetric{
		{Label: "Revenue", Value: is.Revenue, Unit: "TZS"},
		{Label: "Expenses", Value: is.Expenses, Unit: "TZS"},
		{Label: "Net profit", Value: is.NetProfit, Unit: "TZS"},
		{Label: "Assets", Value: bs.Assets, Unit: "TZS"},
		{Label: "Liabilities", Value: bs.Liabilities, Unit: "TZS"},
		{Label: "Equity", Value: bs.Equity, Unit: "TZS"},
	}
	env.Table.Columns = []string{"statement", "line", "amount"}
	env.Table.Rows = [][]string{
		{"Profit & Loss", "Revenue", is.Revenue},
		{"Profit & Loss", "Expenses", is.Expenses},
		{"Profit & Loss", "Net profit", is.NetProfit},
		{"Balance Sheet", "Assets", bs.Assets},
		{"Balance Sheet", "Liabilities", bs.Liabilities},
		{"Balance Sheet", "Retained earnings", bs.RetainedEarnings},
		{"Balance Sheet", "Net income (period to date)", bs.NetIncome},
		{"Balance Sheet", "Equity", bs.Equity},
	}
	sev := reporting.SeverityInfo
	msg := "Balance check: Assets = Liabilities + Equity holds to the cent."
	if !bs.Balanced {
		sev = reporting.SeverityWarning
		msg = "Balance check: the books do NOT balance for this date — review posted journals."
	}
	env.Insights = append(env.Insights, reporting.Insight{Severity: sev, Message: msg})
	return env, nil
}

// buildReceivablesEnvelope builds the tenant-wide receivables aging snapshot: the
// per-customer outstanding balances, largest first. Same data as the AR-aging CSV.
func buildReceivablesEnvelope(ctx context.Context, s *Server, actor identity.Actor, _ map[string]string) (ReportEnvelope, error) {
	env := newEnvelope("ar-aging", "Receivables Aging", "current", nil)
	rows, err := s.receivables.InvoiceAging(ctx, actor.TenantID)
	if err != nil {
		return ReportEnvelope{}, err
	}
	env.Table.Columns = []string{"customer_id", "code", "name", "balance"}
	for i := range rows {
		env.Table.Rows = append(env.Table.Rows, []string{
			rows[i].CustomerID.String(), rows[i].Code, rows[i].Name, rows[i].Balance,
		})
	}
	env.Summary = []summaryMetric{{Label: "Customers with balance", Value: itoa(len(rows)), Unit: "count"}}
	return env, nil
}

// ---- Envelope -> file renderers ----

// renderEnvelopeCSV serialises the envelope's summary and table as a single CSV:
// a KPI block (label,value,unit) then a blank line, then the table headers + rows.
// Every figure is the exact decimal string the envelope carries.
func renderEnvelopeCSV(env ReportEnvelope) []byte {
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)

	_ = cw.Write([]string{"report", env.Metadata.Title})
	_ = cw.Write([]string{"period", env.Metadata.Period})
	_ = cw.Write([]string{"generated_at", env.Metadata.GeneratedAt})
	if line := envelopeFiltersLine(env); line != "" {
		_ = cw.Write([]string{"filters", line})
	}
	_ = cw.Write(nil)

	if len(env.Summary) > 0 {
		_ = cw.Write([]string{"metric", "value", "unit"})
		for i := range env.Summary {
			m := env.Summary[i]
			_ = cw.Write([]string{m.Label, m.Value, m.Unit})
		}
		_ = cw.Write(nil)
	}

	if len(env.Table.Columns) > 0 {
		_ = cw.Write(env.Table.Columns)
		for i := range env.Table.Rows {
			_ = cw.Write(env.Table.Rows[i])
		}
	}
	cw.Flush()
	return buf.Bytes()
}

// renderEnvelopeXLSX serialises the envelope's table (with the KPIs prepended as a
// short sheet preamble would complicate the freeze-pane, so the KPIs ride a
// dedicated header band) using the shared buildWorkbook. Numeric-looking columns
// are formatted as numbers; the rest stay text. Returns the .xlsx bytes.
func renderEnvelopeXLSX(env ReportEnvelope) ([]byte, error) {
	// Sheet 1: the detail table.
	cols := make([]xlsxColumn, 0, len(env.Table.Columns))
	for c, h := range env.Table.Columns {
		numFmt := ""
		if len(env.Table.Rows) > 0 && c < len(env.Table.Rows[0]) && looksNumeric(env.Table.Rows[0][c]) {
			numFmt = "number"
		}
		cols = append(cols, xlsxColumn{header: h, numFmt: numFmt})
	}
	// When the report has no table (e.g. an empty receivables list) fall back to a
	// KPI sheet so the workbook is never empty.
	if len(cols) == 0 {
		cols = []xlsxColumn{{header: "metric"}, {header: "value"}, {header: "unit"}}
		rows := make([][]string, 0, len(env.Summary))
		for i := range env.Summary {
			m := env.Summary[i]
			rows = append(rows, []string{m.Label, m.Value, m.Unit})
		}
		return buildWorkbook("Summary", cols, rows)
	}
	sheet := "Report"
	if env.Metadata.ReportKey != "" {
		sheet = excelSheetName(env.Metadata.ReportKey)
	}
	return buildWorkbook(sheet, cols, env.Table.Rows)
}

// excelSheetName trims a report key into a valid (<=31 char, no special chars)
// Excel sheet name.
func excelSheetName(key string) string {
	name := strings.NewReplacer("[", " ", "]", " ", ":", " ", "*", " ", "?", " ", "/", " ", "\\", " ").Replace(key)
	if len(name) > 31 {
		name = name[:31]
	}
	return name
}

// itoa is a tiny local int->string helper used by the builders.
func itoa(n int) string { return stringsItoa(n) }
