package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/accounting"
	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/payables"
)

type payableDTO struct {
	ID                uuid.UUID  `json:"id"`
	SupplierID        uuid.UUID  `json:"supplier_id"`
	SourceInvoiceID   uuid.UUID  `json:"source_invoice_id"`
	InvoiceNumber     *string    `json:"invoice_number,omitempty"`
	InvoiceDate       *string    `json:"invoice_date,omitempty"`
	DueDate           *string    `json:"due_date,omitempty"`
	Amount            string     `json:"amount"`
	OutstandingAmount string     `json:"outstanding_amount"`
	StationID         *uuid.UUID `json:"station_id,omitempty"`
	Status            string     `json:"status"`
	JournalEntryID    *uuid.UUID `json:"journal_entry_id,omitempty"`
}

func toPayableDTO(p *payables.Payable) payableDTO {
	return payableDTO{
		ID: p.ID, SupplierID: p.SupplierID, SourceInvoiceID: p.SourceInvoiceID,
		InvoiceNumber: p.InvoiceNumber, InvoiceDate: fmtDate(p.InvoiceDate), DueDate: fmtDate(p.DueDate),
		Amount: p.Amount, OutstandingAmount: p.OutstandingAmount, StationID: p.StationID,
		Status: p.Status, JournalEntryID: p.JournalEntryID,
	}
}

// handleImportPayables creates payables for approved Phase-5 invoices and posts
// each to AP (debit inventory, credit accounts payable).
func (s *Server) handleImportPayables(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	created, err := s.payables.ImportApprovedInvoices(ctx, tx, actor.TenantID)
	if err != nil {
		s.logger.Error("import payables", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	for i := range created {
		p := created[i]
		date := time.Now()
		if p.InvoiceDate != nil {
			date = *p.InvoiceDate
		}
		entry, err := s.accounting.PostEntry(ctx, tx, actor.TenantID, accounting.PostEntryInput{
			EntryDate: date, SourceType: "payable", SourceID: &p.ID, StationID: p.StationID,
			PostedBy: actor.UserID, Lines: []accounting.PostLine{
				{SystemKey: "inventory", Debit: p.Amount, Credit: "0"},
				{SystemKey: "accounts_payable", Debit: "0", Credit: p.Amount},
			},
		})
		if code, msg := journalErrorResponse(err); code != 0 {
			writeError(w, code, msg)
			return
		}
		if err := s.payables.SetJournalEntry(ctx, tx, actor.TenantID, p.ID, entry.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "payable.imported", EventType: "PayablesImported", EntityType: "payables",
		EntityID: actor.TenantID.String(), NewValue: map[string]any{"imported": len(created)},
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": len(created)})
}

func (s *Server) handleListPayables(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.payables.List(r.Context(), actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]payableDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toPayableDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleAPaging(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.payables.Aging(r.Context(), actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, map[string]any{
			"supplier_id": rows[i].SupplierID, "outstanding": rows[i].Outstanding, "open_count": rows[i].OpenCount,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

type supplierPaymentRequest struct {
	SupplierID       uuid.UUID `json:"supplier_id"`
	PaymentDate      string    `json:"payment_date"`
	Method           string    `json:"method"`
	Reference        *string   `json:"reference,omitempty"`
	SourceAccountKey string    `json:"source_account_key"`
	Allocations      []struct {
		PayableID uuid.UUID `json:"payable_id"`
		Amount    string    `json:"amount"`
	} `json:"allocations"`
}

func (s *Server) handleRecordSupplierPayment(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req supplierPaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.SupplierID == uuid.Nil || req.Method == "" || len(req.Allocations) == 0 {
		writeError(w, http.StatusBadRequest, "supplier_id, method, and at least one allocation are required")
		return
	}
	date, derr := time.Parse(dateLayout, req.PaymentDate)
	if derr != nil {
		writeError(w, http.StatusBadRequest, "payment_date must be YYYY-MM-DD")
		return
	}
	srcKey := req.SourceAccountKey
	if srcKey == "" {
		srcKey = "bank"
	}
	// Payment amount = sum of allocations (computed in SQL via the lines).
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Create the header with amount 0; it's set to its allocated total below
	// (Postgres sums the allocation rows), so money stays exact in SQL.
	pmt, err := s.payables.CreatePayment(ctx, tx, actor.TenantID, payables.PaymentInput{
		SupplierID: req.SupplierID, PaymentDate: date, Method: req.Method, Reference: req.Reference,
		Amount: "0", SourceAccountKey: srcKey, CreatedBy: actor.UserID,
	})
	if err != nil {
		s.logger.Error("create supplier payment", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	lines := make([]accounting.PostLine, 0, len(req.Allocations)*2)
	for _, al := range req.Allocations {
		if v, ok := parseDecimal(al.Amount); !ok || v <= 0 {
			writeError(w, http.StatusBadRequest, "allocation amounts must be positive decimals")
			return
		}
		if _, err := s.payables.ApplyPayment(ctx, tx, actor.TenantID, al.PayableID, al.Amount); errors.Is(err, payables.ErrOverAllocated) {
			writeError(w, http.StatusUnprocessableEntity, "allocation exceeds the payable's outstanding balance")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if err := s.payables.AddAllocation(ctx, tx, actor.TenantID, pmt.ID, al.PayableID, al.Amount); err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown payable")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		// Each allocation: debit AP, credit the source account (balanced pair).
		lines = append(lines,
			accounting.PostLine{SystemKey: "accounts_payable", Debit: al.Amount, Credit: "0"},
			accounting.PostLine{SystemKey: srcKey, Debit: "0", Credit: al.Amount},
		)
	}

	// Set the payment header amount = its allocated total.
	if _, err := tx.Exec(ctx, `UPDATE supplier_payments SET amount = allocated_amount WHERE tenant_id = $1 AND id = $2`,
		actor.TenantID, pmt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	entry, err := s.accounting.PostEntry(ctx, tx, actor.TenantID, accounting.PostEntryInput{
		EntryDate: date, SourceType: "supplier_payment", SourceID: &pmt.ID, PostedBy: actor.UserID, Lines: lines,
	})
	if code, msg := journalErrorResponse(err); code != 0 {
		writeError(w, code, msg)
		return
	}
	if err := s.payables.SetPaymentJournalEntry(ctx, tx, actor.TenantID, pmt.ID, entry.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "supplier_payment.posted", EventType: "SupplierPaymentPosted", EntityType: "supplier_payment",
		EntityID: pmt.ID.String(), NewValue: map[string]any{"id": pmt.ID, "journal_entry_id": entry.ID},
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"payment_id": pmt.ID, "journal_entry_id": entry.ID})
}

func (s *Server) handleListSupplierPayments(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.payables.ListPayments(r.Context(), actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		p := rows[i]
		out = append(out, map[string]any{
			"id": p.ID, "supplier_id": p.SupplierID, "payment_date": p.PaymentDate.Format(dateLayout),
			"method": p.Method, "reference": p.Reference, "amount": p.Amount,
			"source_account_key": p.SourceAccountKey, "status": p.Status, "journal_entry_id": p.JournalEntryID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}
