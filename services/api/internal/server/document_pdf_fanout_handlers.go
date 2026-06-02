package server

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/procurement"
)

// allRowsLimit caps a document's row count well above any realistic tenant
// catalogue, so a list document renders the full filtered set in one paginated
// PDF rather than a single API page. The repos' *Page methods take a limit; a
// document is not a paged API response, so it asks for "everything".
const allRowsLimit = 100000

// Fanned-out list-document PDF handlers (DOC-PDF, Phase 2b).
//
// The second wave of letterheaded list documents built on renderListDocument:
// purchase orders, station deliveries/GRNs, expenses, AR customer aging,
// supplier balances (AP aging), and the GL journal. Each mirrors the scope,
// filters, and read permission of its JSON list handler (via the route),
// streams a branded PDF inline with a dated filename, and records the export in
// the audit log — the same provable-export pattern the first wave uses. Money /
// litre / rate figures are the exact decimal strings the repos return; this
// layer never touches float64.

// shortID renders the first segment of a UUID (8 hex chars) as a human-facing
// reference for entities that have no business "number" of their own (purchase
// orders, journal-less records). The full UUID stays available via the API.
func shortID(id uuid.UUID) string {
	s := id.String()
	if len(s) >= 8 {
		return s[:8]
	}
	return s
}

// supplierLookup builds an id -> "CODE — Name" map (plus a code-only map) from
// the tenant's supplier master, so documents can show a readable supplier name
// instead of a raw UUID. One query, reused across rows.
func (s *Server) supplierLookup(r *http.Request, tenantID uuid.UUID) (map[uuid.UUID]procurement.Supplier, error) {
	rows, err := s.procurement.ListSuppliers(r.Context(), tenantID)
	if err != nil {
		return nil, err
	}
	m := make(map[uuid.UUID]procurement.Supplier, len(rows))
	for i := range rows {
		m[rows[i].ID] = rows[i]
	}
	return m, nil
}

// supplierLabel renders a supplier as "CODE — Name", or the short id when the
// supplier is unknown (deactivated/removed).
func supplierLabel(m map[uuid.UUID]procurement.Supplier, id uuid.UUID) string {
	if sup, ok := m[id]; ok {
		return sup.Code + " — " + sup.Name
	}
	return shortID(id)
}

// handleExportPurchaseOrdersPDF renders the tenant's purchase orders as a
// branded list document. Mirrors handleListPurchaseOrders' station-read scope,
// optional supplier/status filters, and purchase_order.read permission (via the
// route). The full filtered set is rendered (one paginated document).
func (s *Server) handleExportPurchaseOrdersPDF(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	filter, ok := s.stationReadFilter(w, r, actor)
	if !ok {
		return
	}
	var supplierID *uuid.UUID
	if raw := r.URL.Query().Get("supplier_id"); raw != "" {
		id, perr := uuid.Parse(raw)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid supplier_id")
			return
		}
		supplierID = &id
	}
	var status *string
	if raw := r.URL.Query().Get("status"); raw != "" {
		status = &raw
	}

	// Render the full filtered set, not a single API page.
	rows, err := s.procurement.ListPurchaseOrdersPage(r.Context(), actor.TenantID, procurement.PurchaseOrderFilter{
		StationIDs: filter, SupplierID: supplierID, Status: status,
	}, allRowsLimit, 0)
	if err != nil {
		s.logger.Error("export purchase orders pdf: list", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	suppliers, err := s.supplierLookup(r, actor.TenantID)
	if err != nil {
		s.logger.Error("export purchase orders pdf: suppliers", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tableRows := make([][]string, 0, len(rows))
	for i := range rows {
		po := &rows[i]
		// Per-PO ordered litres + line value totals (decimal-exact).
		orderedAcc := newDecAccumulator()
		valueAcc := newDecAccumulator()
		for j := range po.Lines {
			ln := po.Lines[j]
			orderedAcc.add(ln.OrderedLitres)
			if lt, lok := decMul(ln.OrderedLitres, ln.UnitPrice, 2); lok {
				valueAcc.add(lt)
			}
		}
		expected := ""
		if po.ExpectedDeliveryDate != nil {
			expected = po.ExpectedDeliveryDate.Format(dateLayout)
		}
		tableRows = append(tableRows, []string{
			shortID(po.ID),
			supplierLabel(suppliers, po.SupplierID),
			expected,
			po.Status,
			orderedAcc.string(3),
			valueAcc.string(2),
		})
	}

	metaPairs := []DocumentMeta{}
	if status != nil {
		metaPairs = append(metaPairs, DocumentMeta{Label: "Status", Value: *status})
	}
	if supplierID != nil {
		metaPairs = append(metaPairs, DocumentMeta{Label: "Supplier", Value: supplierLabel(suppliers, *supplierID)})
	}

	spec := ListDocumentSpec{
		Title:     "Purchase Orders",
		SubLines:  []string{fmt.Sprintf("%d records", len(rows))},
		MetaPairs: metaPairs,
		Columns: []DocumentColumn{
			{Header: "PO", Width: 22},
			{Header: "Supplier", Width: 66},
			{Header: "Expected", Width: 26, Align: "C"},
			{Header: "Status", Width: 24, Align: "C"},
			{Header: "Ordered (L)", Width: 22, Align: "R", Numeric: true},
			{Header: "Value", Width: 20, Align: "R", Numeric: true},
		},
		Rows: tableRows,
	}
	body, err := s.renderListDocument(r, actor.TenantID, spec)
	if err != nil {
		s.logger.Error("export purchase orders pdf: render", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeDocumentPDF(w, r, actor, "purchase_orders_pdf", "purchase-orders-"+docDateStamp()+".pdf", body, map[string]any{
		"record_count": len(rows),
	})
}

// handleExportStationDeliveriesPDF renders a station's deliveries/GRNs as a
// branded list document. Mirrors handleListStationDeliveries' station-scoped
// inventory.read permission (authorized in-handler against the path station)
// and tenant scope.
func (s *Server) handleExportStationDeliveriesPDF(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}

	rows, err := s.inventory.ListDeliveriesForStationPage(r.Context(), actor.TenantID, stationID, allRowsLimit, 0)
	if err != nil {
		s.logger.Error("export station deliveries pdf: list", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tableRows := make([][]string, 0, len(rows))
	for i := range rows {
		d := &rows[i]
		ref := derefOr(d.SupplierRef)
		landed := ""
		if d.LandedCostTotal != nil {
			landed = *d.LandedCostTotal
		}
		tableRows = append(tableRows, []string{
			d.ReceivedAt.Format(dateLayout),
			ref,
			d.VolumeLitres,
			derefOr(d.LineUnitPrice),
			landed,
			d.MatchStatus,
		})
	}

	spec := ListDocumentSpec{
		Title:    "Deliveries / GRNs",
		SubLines: []string{fmt.Sprintf("%d records", len(rows))},
		MetaPairs: []DocumentMeta{
			{Label: "Station", Value: stationID.String()},
		},
		Columns: []DocumentColumn{
			{Header: "Received", Width: 26, Align: "C"},
			{Header: "Supplier ref", Width: 50},
			{Header: "Volume (L)", Width: 26, Align: "R", Numeric: true},
			{Header: "Unit price", Width: 26, Align: "R", Numeric: true},
			{Header: "Landed cost", Width: 28, Align: "R", Numeric: true},
			{Header: "Match", Width: 24, Align: "C"},
		},
		Rows: tableRows,
	}
	body, err := s.renderListDocument(r, actor.TenantID, spec)
	if err != nil {
		s.logger.Error("export station deliveries pdf: render", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeDocumentPDF(w, r, actor, "deliveries_pdf",
		fmt.Sprintf("deliveries-%s-%s.pdf", shortID(stationID), docDateStamp()), body, map[string]any{
			"record_count": len(rows), "station_id": stationID.String(),
		})
}

// handleExportExpensesPDF renders the tenant's expenses as a branded list
// document. Mirrors handleListExpenses' tenant scope, optional status filter,
// and finance.read permission (via the route).
func (s *Server) handleExportExpensesPDF(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	status := r.URL.Query().Get("status")

	rows, err := s.expenses.ListExpensesPage(r.Context(), actor.TenantID, status, allRowsLimit, 0)
	if err != nil {
		s.logger.Error("export expenses pdf: list", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	total := newDecAccumulator()
	tableRows := make([][]string, 0, len(rows))
	for i := range rows {
		e := &rows[i]
		total.add(e.Amount)
		tableRows = append(tableRows, []string{
			e.ExpenseDate.Format(dateLayout),
			derefOr(e.Payee),
			e.AccountKey,
			e.PaymentMode,
			e.Status,
			e.Amount,
		})
	}

	metaPairs := []DocumentMeta{}
	if status != "" {
		metaPairs = append(metaPairs, DocumentMeta{Label: "Status", Value: status})
	}

	spec := ListDocumentSpec{
		Title:     "Expenses",
		SubLines:  []string{fmt.Sprintf("%d records", len(rows))},
		MetaPairs: metaPairs,
		Columns: []DocumentColumn{
			{Header: "Date", Width: 26, Align: "C"},
			{Header: "Payee", Width: 52},
			{Header: "Account", Width: 36},
			{Header: "Mode", Width: 24, Align: "C"},
			{Header: "Status", Width: 22, Align: "C"},
			{Header: "Amount", Width: 20, Align: "R", Numeric: true},
		},
		Rows:      tableRows,
		TotalsRow: []string{"Total", "", "", "", "", total.string(2)},
	}
	body, err := s.renderListDocument(r, actor.TenantID, spec)
	if err != nil {
		s.logger.Error("export expenses pdf: render", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeDocumentPDF(w, r, actor, "expenses_pdf", "expenses-"+docDateStamp()+".pdf", body, map[string]any{
		"record_count": len(rows),
	})
}

// handleExportCustomerAgingPDF renders AR aging by customer as a branded list
// document (outstanding balance per customer). Mirrors handleCustomerInvoiceAging's
// tenant scope and finance.read permission (via the route).
func (s *Server) handleExportCustomerAgingPDF(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	rows, err := s.receivables.InvoiceAging(r.Context(), actor.TenantID)
	if err != nil {
		s.logger.Error("export customer aging pdf: list", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	total := newDecAccumulator()
	tableRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		total.add(row.Balance)
		tableRows = append(tableRows, []string{row.Code, row.Name, row.Balance})
	}

	spec := ListDocumentSpec{
		Title:    "Customer Aging",
		SubLines: []string{fmt.Sprintf("%d customers with a balance", len(rows))},
		Columns: []DocumentColumn{
			{Header: "Code", Width: 30},
			{Header: "Customer", Width: 110},
			{Header: "Outstanding", Width: 40, Align: "R", Numeric: true},
		},
		Rows:      tableRows,
		TotalsRow: []string{"Total", "", total.string(2)},
	}
	body, err := s.renderListDocument(r, actor.TenantID, spec)
	if err != nil {
		s.logger.Error("export customer aging pdf: render", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeDocumentPDF(w, r, actor, "customer_aging_pdf", "customer-aging-"+docDateStamp()+".pdf", body, map[string]any{
		"record_count": len(rows),
	})
}

// handleExportSupplierBalancesPDF renders AP balances by supplier (outstanding
// payable + open count) as a branded list document. Mirrors handleAPaging's
// tenant scope and payable.read permission (via the route).
func (s *Server) handleExportSupplierBalancesPDF(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	rows, err := s.payables.Aging(r.Context(), actor.TenantID)
	if err != nil {
		s.logger.Error("export supplier balances pdf: list", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	suppliers, err := s.supplierLookup(r, actor.TenantID)
	if err != nil {
		s.logger.Error("export supplier balances pdf: suppliers", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	total := newDecAccumulator()
	tableRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		total.add(row.Outstanding)
		tableRows = append(tableRows, []string{
			supplierLabel(suppliers, row.SupplierID),
			fmt.Sprintf("%d", row.OpenCount),
			row.Outstanding,
		})
	}

	spec := ListDocumentSpec{
		Title:    "Supplier Balances",
		SubLines: []string{fmt.Sprintf("%d suppliers with a balance", len(rows))},
		Columns: []DocumentColumn{
			{Header: "Supplier", Width: 110},
			{Header: "Open invoices", Width: 30, Align: "R", Numeric: true},
			{Header: "Outstanding", Width: 40, Align: "R", Numeric: true},
		},
		Rows:      tableRows,
		TotalsRow: []string{"Total", "", total.string(2)},
	}
	body, err := s.renderListDocument(r, actor.TenantID, spec)
	if err != nil {
		s.logger.Error("export supplier balances pdf: render", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeDocumentPDF(w, r, actor, "supplier_balances_pdf", "supplier-balances-"+docDateStamp()+".pdf", body, map[string]any{
		"record_count": len(rows),
	})
}

// handleExportJournalEntriesPDF renders the GL journal (entries) as a branded
// list document. Mirrors handleListJournalEntries' tenant scope and journal.read
// permission (via the route). One paginated document over the full journal.
func (s *Server) handleExportJournalEntriesPDF(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	rows, err := s.accounting.ListEntriesPage(r.Context(), actor.TenantID, allRowsLimit, 0)
	if err != nil {
		s.logger.Error("export journal entries pdf: list", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	total := newDecAccumulator()
	tableRows := make([][]string, 0, len(rows))
	for i := range rows {
		e := &rows[i]
		total.add(e.Total)
		memo := ""
		if e.Memo != nil {
			memo = *e.Memo
		}
		tableRows = append(tableRows, []string{
			fmt.Sprintf("%d", e.EntryNumber),
			e.EntryDate.Format(dateLayout),
			e.SourceType,
			e.Status,
			memo,
			e.Total,
		})
	}

	spec := ListDocumentSpec{
		Title:    "Journal Entries",
		SubLines: []string{fmt.Sprintf("%d entries", len(rows))},
		Columns: []DocumentColumn{
			{Header: "No.", Width: 16, Align: "R", Numeric: true},
			{Header: "Date", Width: 24, Align: "C"},
			{Header: "Source", Width: 36},
			{Header: "Status", Width: 22, Align: "C"},
			{Header: "Memo", Width: 56},
			{Header: "Total", Width: 26, Align: "R", Numeric: true},
		},
		Rows:      tableRows,
		TotalsRow: []string{"Total", "", "", "", "", total.string(2)},
	}
	body, err := s.renderListDocument(r, actor.TenantID, spec)
	if err != nil {
		s.logger.Error("export journal entries pdf: render", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeDocumentPDF(w, r, actor, "journal_entries_pdf", "journal-entries-"+docDateStamp()+".pdf", body, map[string]any{
		"record_count": len(rows),
	})
}
