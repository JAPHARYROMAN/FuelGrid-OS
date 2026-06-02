package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// List-document PDF handlers (DOC-PDF).
//
// The first three letterheaded list documents built on the reusable
// renderListDocument framework: a tenant's customers, suppliers, and products.
// Each mirrors the scope/filters and read permission of its JSON list handler
// (handleListCustomers / handleListSuppliers / handleListProducts), streams a
// branded PDF inline (so the browser can preview it in a tab) with a dated
// filename, and records the export in the audit log — the same provable-export
// pattern the report PDFs use. Money/litre/rate figures are the exact decimal
// strings the repos return; this layer never touches float64.

// writeDocumentPDF records the export in the audit log (action
// 'document.exported') with a content checksum and — on success — streams the
// PDF inline (Content-Disposition inline) so the BFF can either preview it in a
// browser tab or save it. documentType is the stable slug stored on the audit
// entry; filename is the suggested download name; meta is merged into the
// audited NewValue alongside the byte count and checksum. Any failure writes a
// JSON error and returns early. This is always the handler's final step.
func (s *Server) writeDocumentPDF(
	w http.ResponseWriter, r *http.Request, actor identity.Actor,
	documentType, filename string, body []byte, meta map[string]any,
) {
	sum := sha256.Sum256(body)
	checksum := hex.EncodeToString(sum[:])
	exportID := uuid.New()

	newValue := map[string]any{
		"document_type": documentType, "format": "pdf",
		"byte_count": len(body), "checksum": checksum,
	}
	for k, v := range meta {
		newValue[k] = v
	}

	ctx := r.Context()
	tx, terr := s.deps.DB.Begin(ctx)
	if terr != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "document.exported", EventType: "DocumentExported",
		EntityType: "document_export", EntityID: exportID.String(),
		NewValue:  newValue,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("document pdf export audit", "error", err, "document_type", documentType)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, filename))
	w.Header().Set("X-Export-Id", exportID.String())
	w.Header().Set("X-Export-Checksum", checksum)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body) //nolint:gosec // G705: body is a server-rendered PDF (fpdf bytes), not user-controlled input
}

// docDateStamp is the YYYY-MM-DD stamp used in list-document filenames.
func docDateStamp() string { return time.Now().UTC().Format(dateLayout) }

// derefOr returns the pointed-to string, or "" for a nil pointer. Used to flatten
// the optional contact fields the list repos return into table cells.
func derefOr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// handleExportCustomersPDF renders the tenant's credit customers as a branded
// list document (code, name, contact, credit limit, status). Mirrors
// handleListCustomers' tenant scope and customer.read permission (via the
// route). The full customer set is rendered (one document, paginated), not a
// single API page.
func (s *Server) handleExportCustomersPDF(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	rows, err := s.receivables.ListCustomers(r.Context(), actor.TenantID)
	if err != nil {
		s.logger.Error("export customers pdf: list", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tableRows := make([][]string, 0, len(rows))
	for i := range rows {
		c := rows[i]
		contact := derefOr(c.ContactName)
		if phone := derefOr(c.ContactPhone); phone != "" {
			if contact != "" {
				contact += " • "
			}
			contact += phone
		}
		tableRows = append(tableRows, []string{
			c.Code, c.Name, contact, c.CreditLimit, c.Status,
		})
	}

	spec := ListDocumentSpec{
		Title:    "Customers",
		SubLines: []string{fmt.Sprintf("%d records", len(rows))},
		Columns: []DocumentColumn{
			{Header: "Code", Width: 24},
			{Header: "Name", Width: 56},
			{Header: "Contact", Width: 50},
			{Header: "Credit limit", Width: 30, Align: "R", Numeric: true},
			{Header: "Status", Width: 20, Align: "C"},
		},
		Rows: tableRows,
	}
	body, err := s.renderListDocument(r, actor.TenantID, spec)
	if err != nil {
		s.logger.Error("export customers pdf: render", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeDocumentPDF(w, r, actor, "customers_pdf", "customers-"+docDateStamp()+".pdf", body, map[string]any{
		"record_count": len(rows),
	})
}

// handleExportSuppliersPDF renders the tenant's suppliers as a branded list
// document (code, name, contact, payment terms, status). Mirrors
// handleListSuppliers' tenant scope and purchase_order.read permission (via the
// route).
func (s *Server) handleExportSuppliersPDF(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	rows, err := s.procurement.ListSuppliers(r.Context(), actor.TenantID)
	if err != nil {
		s.logger.Error("export suppliers pdf: list", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tableRows := make([][]string, 0, len(rows))
	for i := range rows {
		sup := rows[i]
		contact := derefOr(sup.ContactName)
		if email := derefOr(sup.ContactEmail); email != "" {
			if contact != "" {
				contact += " • "
			}
			contact += email
		}
		tableRows = append(tableRows, []string{
			sup.Code, sup.Name, contact,
			fmt.Sprintf("%d", sup.PaymentTermsDays), sup.Status,
		})
	}

	spec := ListDocumentSpec{
		Title:    "Suppliers",
		SubLines: []string{fmt.Sprintf("%d records", len(rows))},
		Columns: []DocumentColumn{
			{Header: "Code", Width: 24},
			{Header: "Name", Width: 56},
			{Header: "Contact", Width: 56},
			{Header: "Terms (days)", Width: 24, Align: "R", Numeric: true},
			{Header: "Status", Width: 20, Align: "C"},
		},
		Rows: tableRows,
	}
	body, err := s.renderListDocument(r, actor.TenantID, spec)
	if err != nil {
		s.logger.Error("export suppliers pdf: render", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeDocumentPDF(w, r, actor, "suppliers_pdf", "suppliers-"+docDateStamp()+".pdf", body, map[string]any{
		"record_count": len(rows),
	})
}

// handleExportProductsPDF renders the tenant's product catalogue as a branded
// list document (code, name, default price, tax rate, status). Mirrors
// handleListProducts' tenant scope and station.read permission (via the route).
func (s *Server) handleExportProductsPDF(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	rows, err := s.products.List(r.Context(), actor.TenantID)
	if err != nil {
		s.logger.Error("export products pdf: list", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tableRows := make([][]string, 0, len(rows))
	for i := range rows {
		p := rows[i]
		tableRows = append(tableRows, []string{
			p.Code, p.Name, p.DefaultPrice, p.TaxRate, p.Status,
		})
	}

	spec := ListDocumentSpec{
		Title:    "Products",
		SubLines: []string{fmt.Sprintf("%d records", len(rows))},
		Columns: []DocumentColumn{
			{Header: "Code", Width: 26},
			{Header: "Name", Width: 64},
			{Header: "Default price", Width: 32, Align: "R", Numeric: true},
			{Header: "Tax rate", Width: 26, Align: "R", Numeric: true},
			{Header: "Status", Width: 22, Align: "C"},
		},
		Rows: tableRows,
	}
	body, err := s.renderListDocument(r, actor.TenantID, spec)
	if err != nil {
		s.logger.Error("export products pdf: render", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeDocumentPDF(w, r, actor, "products_pdf", "products-"+docDateStamp()+".pdf", body, map[string]any{
		"record_count": len(rows),
	})
}
