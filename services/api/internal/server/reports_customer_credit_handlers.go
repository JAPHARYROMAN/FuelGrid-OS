package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/receivables"
	"github.com/japharyroman/fuelgrid-os/internal/reporting"
)

// Customer Credit report (Reports Center §5.9) — the signature receivables-aging
// + credit-exposure suite as a structured ReportEnvelope (report_envelope.go).
//
// TENANT-WIDE (not station-scoped): receivables and credit limits are a
// tenant-level concern, so the report is gated by customer.read at the route
// (the same permission that already governs the AR-aging view and the customer
// master). ?period selects the as-of report date for the aging buckets
// (this-month default → today's date as the as-of cut).
//
// CORE NET-NEW AGGREGATION: the legacy Aging()/InvoiceAging() returned FLAT
// outstanding balances; here the issued-invoice ledger is aged server-side into
// Current / 1-30 / 31-60 / 61-90 / 90+ buckets by due_date vs the report date
// (receivables.AgingBuckets / AgingSummary, all SQL date math, decimal strings).
//
// SENSITIVE-METRIC GATING (blueprint §14): CREDIT EXPOSURE — the credit limit,
// live exposure, available headroom and utilization — is sensitive. It is only
// surfaced to an actor holding customer_credit.read (the "view customer credit
// position" permission, migration 0051). A non-holder sees the aging buckets,
// overdue and risk badges but NOT exposure/limit/utilization: those fields are
// OMITTED entirely (not zeroed), with a data-quality note, mirroring the Sales /
// Delivery margin-gating pattern.

// creditCustomerRow is one credit customer's aging + standing for the report's
// chart_data + table. Every money field is a decimal STRING. The exposure block
// (limit / exposure / available / utilization / over_limit / warning_pct) is a
// set of *string / *bool pointers so it is OMITTED entirely for an actor without
// customer_credit.read — never zeroed.
type creditCustomerRow struct {
	CustomerID string `json:"customer_id"`
	Code       string `json:"code"`
	Name       string `json:"name"`

	Current     string `json:"current"`
	Days1To30   string `json:"days_1_30"`
	Days31To60  string `json:"days_31_60"`
	Days61To90  string `json:"days_61_90"`
	Days90Plus  string `json:"days_90_plus"`
	Outstanding string `json:"outstanding"`
	Overdue     string `json:"overdue"`

	RiskCategory string `json:"risk_category"`
	OnHold       bool   `json:"on_hold"`
	Status       string `json:"status"`

	// CREDIT EXPOSURE — gated. Omitted (nil) for non-holders.
	CreditLimit *string `json:"credit_limit,omitempty"`
	Exposure    *string `json:"exposure,omitempty"`
	Available   *string `json:"available,omitempty"`
	Utilization *string `json:"utilization,omitempty"`
	WarningPct  *string `json:"warning_pct,omitempty"`
	OverLimit   *bool   `json:"over_limit,omitempty"`
}

// agingBucketSlice is one bucket of the tenant-wide aging chart (a stacked /
// categorical bar by bucket). Amount is a decimal string.
type agingBucketSlice struct {
	Bucket string `json:"bucket"`
	Amount string `json:"amount"`
}

// customerCreditChartData is the report's report-specific chart payload: the
// tenant-wide aging buckets (the centerpiece bar), the per-customer rows (which
// power the credit-limit utilization meters, the top-overdue ranking and the
// balance-by-customer trend), and the exposure_shown gate flag.
type customerCreditChartData struct {
	Buckets       []agingBucketSlice  `json:"buckets"`
	Customers     []creditCustomerRow `json:"customers"`
	ExposureShown bool                `json:"exposure_shown"`
}

// handleCustomerCreditReport returns the §5.9 Customer Credit report as a
// ReportEnvelope: a receivable / overdue / %-overdue / over-limit / on-hold KPI
// hero, the aging-bucket chart, per-customer aging rows (with gated credit-limit
// utilization), the deterministic credit insights and honest data-quality.
// Tenant-wide, gated by customer.read; exposure figures gated by
// customer_credit.read.
func (s *Server) handleCustomerCreditReport(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ctx := r.Context()
	_, _, period := resolveReportPeriod(r.URL.Query().Get("period"), time.Now())
	// The aging buckets are aged as-of TODAY (the moment the report is read), not
	// the period's start, so "overdue" reflects the live, current standing.
	asOf := time.Now()

	env := newEnvelope("customer-credit", "Customer Credit", period, nil)
	env.FiltersUsed["period"] = period
	env.FiltersUsed["as_of"] = asOf.Format(dateLayout)

	// CREDIT EXPOSURE is sensitive: only attach limit/exposure/utilization when
	// the actor may read the customer credit position. Decided once.
	exposureShown := s.canViewCreditExposure(ctx, actor)

	rows, rerr := s.receivables.AgingBuckets(ctx, actor.TenantID, asOf)
	if rerr != nil {
		s.logger.Error("customer-credit report: aging buckets", "error", rerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	totals, terr := s.receivables.AgingSummary(ctx, actor.TenantID, asOf)
	if terr != nil {
		s.logger.Error("customer-credit report: aging summary", "error", terr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// ---- KPI hero (§5.9): total receivable, overdue, % overdue, over-limit,
	// on-hold. % overdue parses to float for the DISPLAY ratio only. ----
	env.Summary = []summaryMetric{
		{Label: "Total receivable", Value: totals.Outstanding, Unit: "TZS"},
		{Label: "Total overdue", Value: totals.Overdue, Unit: "TZS"},
		{Label: "% overdue", Value: pctOfTotal(totals.Overdue, totals.Outstanding)},
		{Label: "Customers with balance", Value: strconv.Itoa(totals.CustomersWithBalance), Unit: "count"},
		{Label: "Customers over limit", Value: strconv.Itoa(totals.CustomersOverLimit), Unit: "count"},
		{Label: "Customers on hold", Value: strconv.Itoa(totals.CustomersOnHold), Unit: "count"},
	}

	// ---- chart_data: aging buckets + per-customer rows (exposure gated) ----
	buckets := []agingBucketSlice{
		{Bucket: "Current", Amount: totals.Current},
		{Bucket: "1-30", Amount: totals.Days1To30},
		{Bucket: "31-60", Amount: totals.Days31To60},
		{Bucket: "61-90", Amount: totals.Days61To90},
		{Bucket: "90+", Amount: totals.Days90Plus},
	}
	custRows := make([]creditCustomerRow, 0, len(rows))
	for i := range rows {
		custRows = append(custRows, creditCustomerRowFrom(rows[i], exposureShown))
	}
	env.ChartData = customerCreditChartData{
		Buckets:       buckets,
		Customers:     custRows,
		ExposureShown: exposureShown,
	}

	// ---- drillable table: per-customer aging (exposure columns gated) ----
	if exposureShown {
		env.Table.Columns = []string{
			"customer", "current", "1_30", "31_60", "61_90", "90_plus",
			"outstanding", "overdue", "credit_limit", "exposure", "utilization", "risk",
		}
	} else {
		env.Table.Columns = []string{
			"customer", "current", "1_30", "31_60", "61_90", "90_plus",
			"outstanding", "overdue", "risk",
		}
	}
	for i := range rows {
		c := rows[i]
		risk := c.RiskCategory
		if c.OnHold {
			risk = "on_hold"
		} else if c.OverLimit {
			risk = "over_limit"
		}
		row := []string{
			c.Name, c.Current, c.Days1To30, c.Days31To60, c.Days61To90, c.Days90Plus,
			c.Outstanding, c.Overdue,
		}
		if exposureShown {
			row = append(row, c.CreditLimit, c.Exposure, c.Utilization+"%")
		}
		row = append(row, risk)
		env.Table.Rows = append(env.Table.Rows, row)
	}

	// ---- deterministic insights (reuse the §5.9 CustomerCredit composer) ----
	topName, topBalance := "", "0"
	if len(rows) > 0 {
		topName, topBalance = rows[0].Name, rows[0].Outstanding
	}
	env.applyReport(reporting.CustomerCredit(reporting.CustomerCreditInput{
		Outstanding:            totals.Outstanding,
		Overdue:                totals.Overdue,
		Days61To90:             totals.Days61To90,
		Days90Plus:             totals.Days90Plus,
		TopName:                topName,
		TopBalance:             topBalance,
		CustomersWithBalance:   totals.CustomersWithBalance,
		CustomersOverLimit:     totals.CustomersOverLimit,
		CustomersOnHold:        totals.CustomersOnHold,
		InvoicesWithoutDueDate: totals.InvoicesWithoutDueDate,
		UnallocatedPayments:    totals.UnallocatedPayments,
		ExposureShown:          exposureShown,
	}))

	// ---- drilldown + export (the AR-aging CSV remains the export of record) ----
	env.Drilldown = []drilldownLink{
		{Label: "Credit & cashflow report", Href: "/api/v1/reports/credit-cashflow"},
		{Label: "Customer aging (AR ledger)", Href: "/api/v1/reports/customer-aging/insights"},
	}
	env.ExportOptions = []exportOption{
		{Format: "csv", URL: "/api/v1/reports/ar-aging.csv"},
	}
	writeJSON(w, http.StatusOK, env)
}

// handleCustomerCreditDrilldown returns one customer's open invoices (aged into
// buckets) and recent payments for the report's balance -> invoices -> payments
// drilldown, as a small JSON payload. Tenant-wide, gated by customer.read at the
// route. Money figures are exact decimal strings.
func (s *Server) handleCustomerCreditDrilldown(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	customerID, perr := uuid.Parse(r.URL.Query().Get("customer_id"))
	if perr != nil {
		writeError(w, http.StatusBadRequest, "valid customer_id is required")
		return
	}
	ctx := r.Context()
	asOf := time.Now()

	if _, gerr := s.receivables.GetCustomer(ctx, actor.TenantID, customerID); errors.Is(gerr, receivables.ErrNotFound) {
		writeError(w, http.StatusNotFound, "customer not found")
		return
	} else if gerr != nil {
		s.logger.Error("customer-credit drilldown: get customer", "error", gerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	invoices, payments, derr := s.receivables.CustomerCreditDrilldown(ctx, actor.TenantID, customerID, asOf)
	if derr != nil {
		s.logger.Error("customer-credit drilldown", "error", derr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	type invoiceLine struct {
		InvoiceID     string  `json:"invoice_id"`
		InvoiceNumber *string `json:"invoice_number"`
		InvoiceDate   string  `json:"invoice_date"`
		DueDate       *string `json:"due_date"`
		Amount        string  `json:"amount"`
		Outstanding   string  `json:"outstanding"`
		DaysOverdue   int     `json:"days_overdue"`
		Bucket        string  `json:"bucket"`
		Status        string  `json:"status"`
	}
	type paymentLine struct {
		PaymentID   string  `json:"payment_id"`
		PaymentDate string  `json:"payment_date"`
		Method      string  `json:"method"`
		Reference   *string `json:"reference"`
		Amount      string  `json:"amount"`
		Allocated   string  `json:"allocated"`
		Status      string  `json:"status"`
	}

	invOut := make([]invoiceLine, 0, len(invoices))
	for i := range invoices {
		l := invoices[i]
		var due *string
		if l.DueDate != nil {
			d := l.DueDate.Format(dateLayout)
			due = &d
		}
		invOut = append(invOut, invoiceLine{
			InvoiceID: l.InvoiceID.String(), InvoiceNumber: l.InvoiceNumber,
			InvoiceDate: l.InvoiceDate.Format(dateLayout), DueDate: due,
			Amount: l.Amount, Outstanding: l.Outstanding, DaysOverdue: l.DaysOverdue,
			Bucket: l.Bucket, Status: l.Status,
		})
	}
	payOut := make([]paymentLine, 0, len(payments))
	for i := range payments {
		p := payments[i]
		payOut = append(payOut, paymentLine{
			PaymentID: p.PaymentID.String(), PaymentDate: p.PaymentDate.Format(dateLayout),
			Method: p.Method, Reference: p.Reference, Amount: p.Amount,
			Allocated: p.Allocated, Status: p.Status,
		})
	}

	writeJSON(w, http.StatusOK, struct {
		CustomerID string        `json:"customer_id"`
		Invoices   []invoiceLine `json:"invoices"`
		Payments   []paymentLine `json:"payments"`
	}{CustomerID: customerID.String(), Invoices: invOut, Payments: payOut})
}

// creditCustomerRowFrom maps an aging-bucket repo row onto the wire row,
// attaching the gated CREDIT EXPOSURE block only when exposureShown is true (the
// fields stay nil / omitted otherwise — never zeroed).
func creditCustomerRowFrom(c receivables.AgingBucketRow, exposureShown bool) creditCustomerRow {
	row := creditCustomerRow{
		CustomerID: c.CustomerID.String(), Code: c.Code, Name: c.Name,
		Current: c.Current, Days1To30: c.Days1To30, Days31To60: c.Days31To60,
		Days61To90: c.Days61To90, Days90Plus: c.Days90Plus,
		Outstanding: c.Outstanding, Overdue: c.Overdue,
		RiskCategory: c.RiskCategory, OnHold: c.OnHold, Status: c.Status,
	}
	if exposureShown {
		limit, exposure, available := c.CreditLimit, c.Exposure, c.Available
		util, warn, over := c.Utilization, c.WarningPct, c.OverLimit
		row.CreditLimit = &limit
		row.Exposure = &exposure
		row.Available = &available
		row.Utilization = &util
		row.WarningPct = &warn
		row.OverLimit = &over
	}
	return row
}

// pctOfTotal returns part/total*100 as a 1-dp percent STRING for the headline
// "% overdue" KPI. Parses to float for the DISPLAY ratio only; the underlying
// money figures stay decimal strings. Returns "0" when total is zero/unparsable.
func pctOfTotal(part, total string) string {
	p, okP := parseFloatSafe(part)
	t, okT := parseFloatSafe(total)
	if !okP || !okT || t == 0 {
		return "0"
	}
	return strconv.FormatFloat(p/t*100, 'f', 1, 64)
}

// canViewCreditExposure reports whether the actor may read the sensitive credit
// position (limit / exposure / utilization) — the §5.9 sensitive-metric gate.
// customer_credit.read ("view customer credit position", migration 0051) is a
// tenant-wide permission, so a holder (or a system admin) sees exposure; everyone
// else sees the aging buckets without it. A failed policy load fails CLOSED so
// exposure never leaks on an error.
func (s *Server) canViewCreditExposure(ctx context.Context, actor identity.Actor) bool {
	ps, err := s.policy.LoadFor(ctx, actor)
	if err != nil {
		return false
	}
	return ps.IsSystemAdmin || ps.HasPermission("customer_credit.read")
}
