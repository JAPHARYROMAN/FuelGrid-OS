package server

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/receivables"
)

// Record-document PDF handlers (DOC-PDF, Phase 2b).
//
// The two formal single-record documents you'd send a counterparty: a purchase
// order (to a supplier) and a customer invoice (to a customer). Both are built
// on renderRecordDocument — letterhead + party block + key/value header +
// line-items table + closing totals — and stream a branded PDF inline with a
// dated filename, audited as document.exported. Money / litre figures are exact
// decimal strings; derived line totals use big.Rat (decMul/decAccumulator),
// never float64.

// productLabel renders a product as "CODE — Name", or the short id when unknown.
func productLabel(m map[uuid.UUID]string, id uuid.UUID) string {
	if label, ok := m[id]; ok {
		return label
	}
	return shortID(id)
}

// productLabelMap builds an id -> "CODE — Name" map from the tenant catalogue so
// PO/invoice line items show a readable product instead of a UUID. One query.
func (s *Server) productLabelMap(r *http.Request, tenantID uuid.UUID) (map[uuid.UUID]string, error) {
	rows, err := s.products.List(r.Context(), tenantID)
	if err != nil {
		return nil, err
	}
	m := make(map[uuid.UUID]string, len(rows))
	for i := range rows {
		m[rows[i].ID] = rows[i].Code + " — " + rows[i].Name
	}
	return m, nil
}

// handleExportPurchaseOrderPDF renders a single purchase order as the formal
// document you'd email a supplier: supplier party block, PO number/date/status,
// line items (ordered & received litres, unit cost, line total), and a grand
// total. Reuses purchaseOrderForStationPermission so it mirrors the JSON
// detail's purchase_order.read permission and station authorization.
func (s *Server) handleExportPurchaseOrderPDF(w http.ResponseWriter, r *http.Request) {
	actor, po, ok := s.purchaseOrderForStationPermission(w, r, "purchase_order.read")
	if !ok {
		return
	}

	suppliers, err := s.supplierLookup(r, actor.TenantID)
	if err != nil {
		s.logger.Error("export purchase order pdf: suppliers", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	products, err := s.productLabelMap(r, actor.TenantID)
	if err != nil {
		s.logger.Error("export purchase order pdf: products", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	grand := newDecAccumulator()
	lines := make([][]string, 0, len(po.Lines))
	for i := range po.Lines {
		ln := po.Lines[i]
		lineTotal := ""
		if lt, lok := decMul(ln.OrderedLitres, ln.UnitPrice, 2); lok {
			lineTotal = lt
			grand.add(lt)
		}
		lines = append(lines, []string{
			productLabel(products, ln.ProductID),
			ln.OrderedLitres,
			ln.ReceivedLitres,
			ln.UnitPrice,
			lineTotal,
		})
	}

	expected := "—"
	if po.ExpectedDeliveryDate != nil {
		expected = po.ExpectedDeliveryDate.Format(dateLayout)
	}
	meta := []DocumentMeta{
		{Label: "PO number", Value: shortID(po.ID)},
		{Label: "Status", Value: po.Status},
		{Label: "Raised", Value: po.CreatedAt.Format(dateLayout)},
		{Label: "Expected delivery", Value: expected},
	}

	sup := suppliers[po.SupplierID]
	partyLines := []string{sup.Name}
	if sup.ContactName != nil && *sup.ContactName != "" {
		partyLines = append(partyLines, *sup.ContactName)
	}
	if sup.ContactEmail != nil && *sup.ContactEmail != "" {
		partyLines = append(partyLines, *sup.ContactEmail)
	}
	if len(partyLines) == 1 && partyLines[0] == "" {
		partyLines = []string{supplierLabel(suppliers, po.SupplierID)}
	}

	spec := RecordDocumentSpec{
		Title:        "Purchase Order",
		SubLines:     []string{"PO " + shortID(po.ID)},
		PartyHeading: "Supplier",
		PartyLines:   partyLines,
		MetaPairs:    meta,
		LineColumns: []DocumentColumn{
			{Header: "Product", Width: 70},
			{Header: "Ordered (L)", Width: 26, Align: "R", Numeric: true},
			{Header: "Received (L)", Width: 26, Align: "R", Numeric: true},
			{Header: "Unit cost", Width: 28, Align: "R", Numeric: true},
			{Header: "Line total", Width: 30, Align: "R", Numeric: true},
		},
		Lines:  lines,
		Totals: []DocumentMeta{{Label: "Grand total", Value: grand.string(2)}},
		Note:   "This purchase order is system-generated and valid without signature.",
	}
	body, err := s.renderRecordDocument(r, actor.TenantID, spec)
	if err != nil {
		s.logger.Error("export purchase order pdf: render", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeDocumentPDF(w, r, actor, "purchase_order_pdf",
		fmt.Sprintf("purchase-order-%s.pdf", shortID(po.ID)), body, map[string]any{
			"purchase_order_id": po.ID.String(),
		})
}

// handleExportCustomerInvoicePDF renders a single customer invoice as the
// formal document you'd send a customer: bill-to party block, invoice
// number/dates/status, billed line items, and subtotal / total. Gated by
// finance.read (via the route), tenant-scoped via GetInvoice.
func (s *Server) handleExportCustomerInvoicePDF(w http.ResponseWriter, r *http.Request) {
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

	inv, err := s.receivables.GetInvoice(r.Context(), actor.TenantID, id)
	if errors.Is(err, receivables.ErrNotFound) {
		writeError(w, http.StatusNotFound, "customer invoice not found")
		return
	}
	if err != nil {
		s.logger.Error("export customer invoice pdf: get", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	invLines, err := s.receivables.ListInvoiceLines(r.Context(), actor.TenantID, inv.ID)
	if err != nil {
		s.logger.Error("export customer invoice pdf: lines", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	cust, err := s.receivables.GetCustomer(r.Context(), actor.TenantID, inv.CustomerID)
	if err != nil && !errors.Is(err, receivables.ErrNotFound) {
		s.logger.Error("export customer invoice pdf: customer", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	subtotal := newDecAccumulator()
	lines := make([][]string, 0, len(invLines))
	for i := range invLines {
		ln := invLines[i]
		subtotal.add(ln.Amount)
		desc := derefOr(ln.Description)
		if desc == "" {
			desc = ln.RevenueAccountKey
		}
		lines = append(lines, []string{desc, ln.Amount})
	}

	number := shortID(inv.ID)
	if inv.InvoiceNumber != nil && *inv.InvoiceNumber != "" {
		number = *inv.InvoiceNumber
	}
	due := "—"
	if inv.DueDate != nil {
		due = inv.DueDate.Format(dateLayout)
	}
	meta := []DocumentMeta{
		{Label: "Invoice number", Value: number},
		{Label: "Status", Value: inv.Status},
		{Label: "Invoice date", Value: inv.InvoiceDate.Format(dateLayout)},
		{Label: "Due date", Value: due},
	}

	partyLines := []string{}
	if cust != nil {
		partyLines = append(partyLines, cust.Name)
		if cust.BillingAddress != nil && *cust.BillingAddress != "" {
			partyLines = append(partyLines, *cust.BillingAddress)
		}
		if cust.TaxID != nil && *cust.TaxID != "" {
			partyLines = append(partyLines, "Tax ID: "+*cust.TaxID)
		}
	} else {
		partyLines = append(partyLines, shortID(inv.CustomerID))
	}

	// The invoice header amount is the authoritative total; the line subtotal
	// equals it (no separate tax line is modelled yet, so subtotal == total).
	totals := []DocumentMeta{
		{Label: "Subtotal", Value: subtotal.string(2)},
		{Label: "Total", Value: inv.Amount},
	}

	spec := RecordDocumentSpec{
		Title:        "Tax Invoice",
		SubLines:     []string{"Invoice " + number},
		PartyHeading: "Bill to",
		PartyLines:   partyLines,
		MetaPairs:    meta,
		LineColumns: []DocumentColumn{
			{Header: "Description", Width: 140},
			{Header: "Amount", Width: 40, Align: "R", Numeric: true},
		},
		Lines:  lines,
		Totals: totals,
		Note:   "Please remit payment by the due date, quoting the invoice number.",
	}
	body, err := s.renderRecordDocument(r, actor.TenantID, spec)
	if err != nil {
		s.logger.Error("export customer invoice pdf: render", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeDocumentPDF(w, r, actor, "customer_invoice_pdf",
		fmt.Sprintf("invoice-%s.pdf", number), body, map[string]any{
			"customer_invoice_id": inv.ID.String(),
		})
}
