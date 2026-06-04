package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/accounting"
	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/receivables"
)

type customerInvoiceDTO struct {
	ID                uuid.UUID  `json:"id"`
	CustomerID        uuid.UUID  `json:"customer_id"`
	InvoiceNumber     *string    `json:"invoice_number,omitempty"`
	InvoiceDate       string     `json:"invoice_date"`
	DueDate           *string    `json:"due_date,omitempty"`
	Amount            string     `json:"amount"`
	OutstandingAmount string     `json:"outstanding_amount"`
	SourceType        string     `json:"source_type"`
	StationID         *uuid.UUID `json:"station_id,omitempty"`
	Status            string     `json:"status"`
	JournalEntryID    *uuid.UUID `json:"journal_entry_id,omitempty"`
}

func toCustomerInvoiceDTO(i *receivables.CustomerInvoice) customerInvoiceDTO {
	return customerInvoiceDTO{
		ID: i.ID, CustomerID: i.CustomerID, InvoiceNumber: i.InvoiceNumber,
		InvoiceDate: i.InvoiceDate.Format(dateLayout), DueDate: fmtDate(i.DueDate),
		Amount: i.Amount, OutstandingAmount: i.OutstandingAmount, SourceType: i.SourceType,
		StationID: i.StationID, Status: i.Status, JournalEntryID: i.JournalEntryID,
	}
}

// handleCreateCustomerInvoice creates a draft invoice with its billed lines.
func (s *Server) handleCreateCustomerInvoice(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		CustomerID    uuid.UUID  `json:"customer_id"`
		InvoiceNumber *string    `json:"invoice_number,omitempty"`
		InvoiceDate   string     `json:"invoice_date,omitempty"`
		DueDate       string     `json:"due_date,omitempty"`
		SourceType    string     `json:"source_type,omitempty"`
		SourceID      *uuid.UUID `json:"source_id,omitempty"`
		StationID     *uuid.UUID `json:"station_id,omitempty"`
		Lines         []struct {
			Description       *string `json:"description,omitempty"`
			Amount            string  `json:"amount"`
			RevenueAccountKey string  `json:"revenue_account_key,omitempty"`
		} `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.CustomerID == uuid.Nil || len(req.Lines) == 0 {
		writeError(w, http.StatusBadRequest, "customer_id and at least one line are required")
		return
	}
	invoiceDate := time.Now()
	if req.InvoiceDate != "" {
		t, derr := time.Parse(dateLayout, req.InvoiceDate)
		if derr != nil {
			writeError(w, http.StatusBadRequest, "invoice_date must be YYYY-MM-DD")
			return
		}
		invoiceDate = t
	}
	due := parseOptDate(req.DueDate)

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	inv, err := s.receivables.CreateInvoice(ctx, tx, actor.TenantID, receivables.InvoiceInput{
		CustomerID: req.CustomerID, InvoiceNumber: req.InvoiceNumber, InvoiceDate: invoiceDate, DueDate: due,
		SourceType: req.SourceType, SourceID: req.SourceID, StationID: req.StationID, CreatedBy: actor.UserID,
	})
	if err != nil {
		if isForeignKeyViolation(err) {
			writeError(w, http.StatusBadRequest, "unknown customer or station")
			return
		}
		s.logger.Error("create customer invoice", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	for _, ln := range req.Lines {
		if v, ok := parseDecimal(ln.Amount); !ok || v <= 0 {
			writeError(w, http.StatusBadRequest, "line amounts must be positive decimals")
			return
		}
		if err := s.receivables.AddInvoiceLine(ctx, tx, actor.TenantID, inv.ID, ln.Description, ln.Amount, ln.RevenueAccountKey); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if _, err := s.receivables.FinalizeInvoiceAmount(ctx, tx, actor.TenantID, inv.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer_invoice.created", EventType: "CustomerInvoiceCreated", EntityType: "customer_invoice",
		EntityID: inv.ID.String(), NewValue: map[string]any{"id": inv.ID},
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out, _ := s.receivables.GetInvoice(ctx, actor.TenantID, inv.ID)
	writeJSON(w, http.StatusCreated, toCustomerInvoiceDTO(out))
}

func (s *Server) handleListCustomerInvoices(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var customerID uuid.UUID
	if v := r.URL.Query().Get("customer_id"); v != "" {
		if id, perr := uuid.Parse(v); perr == nil {
			customerID = id
		}
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.receivables.ListInvoicesPage(r.Context(), actor.TenantID, customerID, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]customerInvoiceDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toCustomerInvoiceDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

func (s *Server) handleGetCustomerInvoice(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toCustomerInvoiceDTO(inv))
}

// handleIssueCustomerInvoice posts debit AR / credit revenue and issues it.
func (s *Server) handleIssueCustomerInvoice(w http.ResponseWriter, r *http.Request) {
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
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	inv, err := s.receivables.IssueInvoice(ctx, tx, actor.TenantID, id)
	if errors.Is(err, receivables.ErrInvoiceState) {
		writeError(w, http.StatusConflict, "only a draft invoice can be issued")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	groups, err := s.receivables.RevenueBreakdown(ctx, tx, actor.TenantID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	lines := make([]accounting.PostLine, 0, len(groups)+1)
	lines = append(lines, accounting.PostLine{SystemKey: "accounts_receivable", Debit: inv.Amount, Credit: "0", StationID: inv.StationID})
	for _, g := range groups {
		lines = append(lines, accounting.PostLine{SystemKey: g.AccountKey, Debit: "0", Credit: g.Amount, StationID: inv.StationID})
	}
	entry, err := s.accounting.PostEntry(ctx, tx, actor.TenantID, accounting.PostEntryInput{
		EntryDate: inv.InvoiceDate, SourceType: "customer_invoice", SourceID: &id, StationID: inv.StationID,
		PostedBy: actor.UserID, Lines: lines,
	})
	if code, msg := journalErrorResponse(err); code != 0 {
		writeError(w, code, msg)
		return
	}
	if err := s.receivables.SetInvoiceJournalEntry(ctx, tx, actor.TenantID, id, entry.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer_invoice.issued", EventType: "CustomerInvoiceIssued", EntityType: "customer_invoice",
		EntityID: id.String(), NewValue: map[string]any{"journal_entry_id": entry.ID, "amount": inv.Amount},
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out, _ := s.receivables.GetInvoice(ctx, actor.TenantID, id)
	writeJSON(w, http.StatusOK, toCustomerInvoiceDTO(out))
}

func (s *Server) handleCustomerInvoiceAging(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.receivables.InvoiceAging(r.Context(), actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, map[string]any{
			"customer_id": rows[i].CustomerID, "code": rows[i].Code, "name": rows[i].Name, "balance": rows[i].Balance,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

// ---- Customer payments (Stage 10) ----

func (s *Server) handlePostCustomerPayment(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		CustomerID       uuid.UUID `json:"customer_id"`
		PaymentDate      string    `json:"payment_date"`
		Method           string    `json:"method"`
		Reference        *string   `json:"reference,omitempty"`
		SourceAccountKey string    `json:"source_account_key,omitempty"`
		Allocations      []struct {
			CustomerInvoiceID uuid.UUID `json:"customer_invoice_id"`
			Amount            string    `json:"amount"`
		} `json:"allocations"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.CustomerID == uuid.Nil || req.Method == "" || len(req.Allocations) == 0 {
		writeError(w, http.StatusBadRequest, "customer_id, method, and at least one allocation are required")
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
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	pmt, err := s.receivables.CreateCustomerPayment(ctx, tx, actor.TenantID, receivables.CustomerPaymentInput{
		CustomerID: req.CustomerID, PaymentDate: date, Method: req.Method, Reference: req.Reference,
		SourceAccountKey: srcKey, CreatedBy: actor.UserID,
	})
	if err != nil {
		if isForeignKeyViolation(err) {
			writeError(w, http.StatusBadRequest, "unknown customer")
			return
		}
		s.logger.Error("create customer payment", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	lines := make([]accounting.PostLine, 0, len(req.Allocations)*2)
	for _, al := range req.Allocations {
		if v, ok := parseDecimal(al.Amount); !ok || v <= 0 {
			writeError(w, http.StatusBadRequest, "allocation amounts must be positive decimals")
			return
		}
		if _, err := s.receivables.ApplyInvoicePayment(ctx, tx, actor.TenantID, pmt.CustomerID, al.CustomerInvoiceID, al.Amount); errors.Is(err, receivables.ErrOverAllocated) {
			writeError(w, http.StatusUnprocessableEntity, "allocation exceeds the invoice's outstanding balance")
			return
		} else if errors.Is(err, receivables.ErrInvoiceNotForCustomer) {
			writeError(w, http.StatusUnprocessableEntity, "allocation targets an invoice that does not belong to the paying customer")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if err := s.receivables.AddCustomerAllocation(ctx, tx, actor.TenantID, pmt.ID, al.CustomerInvoiceID, al.Amount); err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown customer invoice")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		// Each allocation: debit bank/cash, credit AR (balanced pair).
		lines = append(lines,
			accounting.PostLine{SystemKey: srcKey, Debit: al.Amount, Credit: "0"},
			accounting.PostLine{SystemKey: "accounts_receivable", Debit: "0", Credit: al.Amount},
		)
	}
	entry, err := s.accounting.PostEntry(ctx, tx, actor.TenantID, accounting.PostEntryInput{
		EntryDate: date, SourceType: "customer_payment", SourceID: &pmt.ID, PostedBy: actor.UserID, Lines: lines,
	})
	if code, msg := journalErrorResponse(err); code != 0 {
		writeError(w, code, msg)
		return
	}
	if err := s.receivables.SetCustomerPaymentJournalEntry(ctx, tx, actor.TenantID, pmt.ID, entry.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer_payment.posted", EventType: "CustomerPaymentPosted", EntityType: "customer_payment",
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

// handleReverseCustomerPayment voids a posted customer payment: it restores the
// affected invoices' outstanding balances, posts a balanced reversal of the
// payment's journal entry (append-only — the original entry and payment row are
// preserved), and marks the payment 'voided'. Reversing a payment that is not
// posted (already voided) is refused with 409 so the operation is
// idempotent-safe.
func (s *Server) handleReverseCustomerPayment(w http.ResponseWriter, r *http.Request) {
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
	var body struct {
		Reason *string `json:"reason,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	pmt, allocs, err := s.receivables.ReverseCustomerPayment(ctx, tx, actor.TenantID, id)
	if errors.Is(err, receivables.ErrNotFound) {
		writeError(w, http.StatusNotFound, "customer payment not found")
		return
	}
	if errors.Is(err, receivables.ErrPaymentNotReversible) {
		writeError(w, http.StatusConflict, "only a posted payment can be reversed")
		return
	}
	if err != nil {
		s.logger.Error("reverse customer payment", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Reverse the payment's journal entry (debit AR / credit bank swapped back).
	var reversalEntryID *uuid.UUID
	if pmt.JournalEntryID != nil {
		rev, rerr := s.accounting.ReverseEntry(ctx, tx, actor.TenantID, *pmt.JournalEntryID, actor.UserID, body.Reason)
		if errors.Is(rerr, accounting.ErrAlreadyReversed) {
			writeError(w, http.StatusConflict, "the payment's journal entry is already reversed")
			return
		}
		if code, msg := journalErrorResponse(rerr); code != 0 {
			writeError(w, code, msg)
			return
		}
		reversalEntryID = &rev.ID
	}

	allocated := make([]map[string]any, 0, len(allocs))
	for _, a := range allocs {
		allocated = append(allocated, map[string]any{"customer_invoice_id": a.CustomerInvoiceID, "amount": a.Amount})
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer_payment.reversed", EventType: "CustomerPaymentReversed", EntityType: "customer_payment",
		EntityID: pmt.ID.String(),
		NewValue: map[string]any{"id": pmt.ID, "reversal_entry_id": reversalEntryID, "restored": allocated, "reason": body.Reason},
		IP:       clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"payment_id": pmt.ID, "status": pmt.Status, "reversal_entry_id": reversalEntryID,
	})
}

func (s *Server) handleListCustomerPayments(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.receivables.ListCustomerPaymentsPage(r.Context(), actor.TenantID, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		p := rows[i]
		out = append(out, map[string]any{
			"id": p.ID, "customer_id": p.CustomerID, "payment_date": p.PaymentDate.Format(dateLayout),
			"method": p.Method, "reference": p.Reference, "amount": p.Amount,
			"source_account_key": p.SourceAccountKey, "status": p.Status, "journal_entry_id": p.JournalEntryID,
		})
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}
