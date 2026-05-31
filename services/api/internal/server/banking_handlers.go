package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/accounting"
	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/banking"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// ---- DTOs ----

type cashReconDTO struct {
	ID             uuid.UUID  `json:"id"`
	StationID      uuid.UUID  `json:"station_id"`
	OperatingDayID uuid.UUID  `json:"operating_day_id"`
	ExpectedCash   string     `json:"expected_cash"`
	CountedCash    string     `json:"counted_cash"`
	Variance       string     `json:"variance"`
	Status         string     `json:"status"`
	Notes          *string    `json:"notes,omitempty"`
	JournalEntryID *uuid.UUID `json:"journal_entry_id,omitempty"`
	ReviewedBy     *uuid.UUID `json:"reviewed_by,omitempty"`
}

func toCashReconDTO(c *banking.CashReconciliation) cashReconDTO {
	return cashReconDTO{
		ID: c.ID, StationID: c.StationID, OperatingDayID: c.OperatingDayID,
		ExpectedCash: c.ExpectedCash, CountedCash: c.CountedCash, Variance: c.Variance,
		Status: c.Status, Notes: c.Notes, JournalEntryID: c.JournalEntryID, ReviewedBy: c.ReviewedBy,
	}
}

type bankAccountDTO struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	AccountNumber *string   `json:"account_number,omitempty"`
	Currency      string    `json:"currency"`
	Status        string    `json:"status"`
}

func toBankAccountDTO(a *banking.BankAccount) bankAccountDTO {
	return bankAccountDTO{ID: a.ID, Name: a.Name, AccountNumber: a.AccountNumber, Currency: a.Currency, Status: a.Status}
}

type bankDepositDTO struct {
	ID               uuid.UUID  `json:"id"`
	StationID        uuid.UUID  `json:"station_id"`
	BankAccountID    uuid.UUID  `json:"bank_account_id"`
	SlipNumber       *string    `json:"slip_number,omitempty"`
	Amount           string     `json:"amount"`
	Reference        *string    `json:"reference,omitempty"`
	ExpectedBankDate *string    `json:"expected_bank_date,omitempty"`
	ActualBankDate   *string    `json:"actual_bank_date,omitempty"`
	Status           string     `json:"status"`
	PreparedEntryID  *uuid.UUID `json:"prepared_entry_id,omitempty"`
	ConfirmedEntryID *uuid.UUID `json:"confirmed_entry_id,omitempty"`
}

func toBankDepositDTO(d *banking.BankDeposit) bankDepositDTO {
	return bankDepositDTO{
		ID: d.ID, StationID: d.StationID, BankAccountID: d.BankAccountID, SlipNumber: d.SlipNumber,
		Amount: d.Amount, Reference: d.Reference, ExpectedBankDate: fmtDate(d.ExpectedBankDate),
		ActualBankDate: fmtDate(d.ActualBankDate), Status: d.Status,
		PreparedEntryID: d.PreparedEntryID, ConfirmedEntryID: d.ConfirmedEntryID,
	}
}

type statementLineDTO struct {
	ID             uuid.UUID  `json:"id"`
	ImportID       uuid.UUID  `json:"import_id"`
	BankAccountID  uuid.UUID  `json:"bank_account_id"`
	TxnDate        string     `json:"txn_date"`
	ValueDate      *string    `json:"value_date,omitempty"`
	Amount         string     `json:"amount"`
	Reference      *string    `json:"reference,omitempty"`
	Description    *string    `json:"description,omitempty"`
	Status         string     `json:"status"`
	MatchedDocType *string    `json:"matched_doc_type,omitempty"`
	MatchedDocID   *uuid.UUID `json:"matched_doc_id,omitempty"`
	JournalEntryID *uuid.UUID `json:"journal_entry_id,omitempty"`
}

func toStatementLineDTO(l *banking.StatementLine) statementLineDTO {
	return statementLineDTO{
		ID: l.ID, ImportID: l.ImportID, BankAccountID: l.BankAccountID, TxnDate: l.TxnDate.Format(dateLayout),
		ValueDate: fmtDate(l.ValueDate), Amount: l.Amount, Reference: l.Reference, Description: l.Description,
		Status: l.Status, MatchedDocType: l.MatchedDocType, MatchedDocID: l.MatchedDocID, JournalEntryID: l.JournalEntryID,
	}
}

// absMoney strips a leading minus from a decimal string — magnitude without
// float arithmetic.
func absMoney(s string) string { return strings.TrimPrefix(s, "-") }

func isZeroMoney(s string) bool {
	v, ok := parseDecimal(s)
	return !ok || v == 0
}

// ---- Cash reconciliation (Stage 4) ----

func (s *Server) handleCreateCashReconciliation(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		OperatingDayID uuid.UUID `json:"operating_day_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OperatingDayID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "operating_day_id is required")
		return
	}
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	c, err := s.banking.CreateCashReconciliation(ctx, tx, actor.TenantID, stationID, req.OperatingDayID, actor.UserID)
	if errors.Is(err, banking.ErrDuplicate) {
		writeError(w, http.StatusConflict, "a cash reconciliation already exists for this operating day")
		return
	}
	if err != nil {
		if isForeignKeyViolation(err) {
			writeError(w, http.StatusBadRequest, "unknown station or operating day")
			return
		}
		s.logger.Error("create cash reconciliation", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "cash_reconciliation.created", EventType: "CashReconciliationCreated", EntityType: "cash_reconciliation",
		EntityID: c.ID.String(), NewValue: map[string]any{"id": c.ID, "expected_cash": c.ExpectedCash},
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toCashReconDTO(c))
}

func (s *Server) handleListCashReconciliations(w http.ResponseWriter, r *http.Request) {
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
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.banking.ListCashReconciliationsPage(r.Context(), actor.TenantID, stationID, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]cashReconDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toCashReconDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

func (s *Server) handleGetCashReconciliation(w http.ResponseWriter, r *http.Request) {
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
	c, err := s.banking.GetCashReconciliation(r.Context(), actor.TenantID, id)
	if errors.Is(err, banking.ErrNotFound) {
		writeError(w, http.StatusNotFound, "cash reconciliation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toCashReconDTO(c))
}

func (s *Server) handleSubmitCashReconciliation(w http.ResponseWriter, r *http.Request) {
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
		CountedCash string  `json:"counted_cash"`
		Notes       *string `json:"notes,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if v, ok := parseDecimal(req.CountedCash); !ok || v < 0 {
		writeError(w, http.StatusBadRequest, "counted_cash must be a non-negative decimal")
		return
	}
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	c, err := s.banking.SubmitCashReconciliation(ctx, tx, actor.TenantID, id, req.CountedCash, req.Notes)
	if errors.Is(err, banking.ErrBadState) {
		writeError(w, http.StatusConflict, "reconciliation is not in a submittable state")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "cash_reconciliation.submitted", EventType: "CashReconciliationSubmitted", EntityType: "cash_reconciliation",
		EntityID: c.ID.String(), NewValue: map[string]any{"counted_cash": c.CountedCash, "variance": c.Variance},
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toCashReconDTO(c))
}

// handleApproveCashReconciliation posts the balanced cash entry: debit cash on
// hand (counted), credit sales clearing (expected), with the over/short
// remainder to the cash over/short account.
func (s *Server) handleApproveCashReconciliation(w http.ResponseWriter, r *http.Request) {
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

	p, err := s.banking.PostingFor(ctx, tx, actor.TenantID, id)
	if errors.Is(err, banking.ErrNotFound) {
		writeError(w, http.StatusNotFound, "cash reconciliation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if p.Status != "submitted" {
		writeError(w, http.StatusConflict, "reconciliation must be submitted before approval")
		return
	}

	// Separation of duties: the approver must not be the person who created /
	// submitted the reconciliation. The row is locked for the rest of the tx.
	var createdBy uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT created_by FROM cash_reconciliations
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, actor.TenantID, id).Scan(&createdBy); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if createdBy == actor.UserID {
		writeError(w, http.StatusForbidden, "separation of duties: you cannot approve a cash reconciliation you submitted")
		return
	}

	station := p.StationID
	lines := make([]accounting.PostLine, 0, 4)
	if !isZeroMoney(p.Counted) {
		lines = append(lines, accounting.PostLine{SystemKey: "cash_on_hand", Debit: p.Counted, Credit: "0", StationID: &station})
	}
	if !isZeroMoney(p.Expected) {
		lines = append(lines, accounting.PostLine{SystemKey: "sales_clearing", Debit: "0", Credit: p.Expected, StationID: &station})
	}
	if !isZeroMoney(p.ShortAmount) {
		lines = append(lines, accounting.PostLine{SystemKey: "cash_over_short", Debit: p.ShortAmount, Credit: "0", StationID: &station})
	}
	if !isZeroMoney(p.OverAmount) {
		lines = append(lines, accounting.PostLine{SystemKey: "cash_over_short", Debit: "0", Credit: p.OverAmount, StationID: &station})
	}
	if len(lines) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "nothing to post: counted and expected cash are both zero")
		return
	}

	entry, err := s.accounting.PostEntry(ctx, tx, actor.TenantID, accounting.PostEntryInput{
		EntryDate: p.BusinessDate, SourceType: "cash_reconciliation", SourceID: &id, StationID: &station,
		PostedBy: actor.UserID, Lines: lines,
	})
	if code, msg := journalErrorResponse(err); code != 0 {
		writeError(w, code, msg)
		return
	}
	if err := s.banking.MarkCashReconciliationPosted(ctx, tx, actor.TenantID, id, actor.UserID, &entry.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "cash_reconciliation.posted", EventType: "CashReconciliationPosted", EntityType: "cash_reconciliation",
		EntityID: id.String(), NewValue: map[string]any{"journal_entry_id": entry.ID},
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	c, _ := s.banking.GetCashReconciliation(ctx, actor.TenantID, id)
	writeJSON(w, http.StatusOK, toCashReconDTO(c))
}

// ---- Bank accounts (Stage 5) ----

func (s *Server) handleCreateBankAccount(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Name          string  `json:"name"`
		AccountNumber *string `json:"account_number,omitempty"`
		Currency      string  `json:"currency,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	var acct *banking.BankAccount
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "bank_account.created", EventType: "BankAccountCreated", EntityType: "bank_account",
	}, func(tx pgx.Tx) (string, error) {
		a, err := s.banking.CreateBankAccount(r.Context(), tx, actor.TenantID, req.Name, req.AccountNumber, req.Currency)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		acct = a
		return a.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, toBankAccountDTO(acct))
}

func (s *Server) handleListBankAccounts(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.banking.ListBankAccountsPage(r.Context(), actor.TenantID, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]bankAccountDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toBankAccountDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

// ---- Bank deposits (Stage 5) ----

func (s *Server) handleCreateBankDeposit(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		StationID        uuid.UUID `json:"station_id"`
		BankAccountID    uuid.UUID `json:"bank_account_id"`
		SlipNumber       *string   `json:"slip_number,omitempty"`
		Reference        *string   `json:"reference,omitempty"`
		ExpectedBankDate string    `json:"expected_bank_date"`
		Lines            []struct {
			CashReconciliationID uuid.UUID `json:"cash_reconciliation_id"`
			Amount               string    `json:"amount"`
		} `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.StationID == uuid.Nil || req.BankAccountID == uuid.Nil || len(req.Lines) == 0 {
		writeError(w, http.StatusBadRequest, "station_id, bank_account_id, and at least one line are required")
		return
	}
	var expected *time.Time
	if req.ExpectedBankDate != "" {
		t, derr := time.Parse(dateLayout, req.ExpectedBankDate)
		if derr != nil {
			writeError(w, http.StatusBadRequest, "expected_bank_date must be YYYY-MM-DD")
			return
		}
		expected = &t
	}
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	d, err := s.banking.CreateDeposit(ctx, tx, actor.TenantID, banking.DepositInput{
		StationID: req.StationID, BankAccountID: req.BankAccountID, SlipNumber: req.SlipNumber,
		Reference: req.Reference, ExpectedBankDate: expected, CreatedBy: actor.UserID,
	})
	if err != nil {
		if isForeignKeyViolation(err) {
			writeError(w, http.StatusBadRequest, "unknown station or bank account")
			return
		}
		s.logger.Error("create deposit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	for _, ln := range req.Lines {
		if v, ok := parseDecimal(ln.Amount); !ok || v <= 0 {
			writeError(w, http.StatusBadRequest, "deposit line amounts must be positive decimals")
			return
		}
		if err := s.banking.AddDepositLine(ctx, tx, actor.TenantID, d.ID, ln.CashReconciliationID, ln.Amount); errors.Is(err, banking.ErrDuplicate) {
			writeError(w, http.StatusConflict, "a cash reconciliation has already been deposited")
			return
		} else if err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown cash reconciliation")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "bank_deposit.created", EventType: "BankDepositCreated", EntityType: "bank_deposit",
		EntityID: d.ID.String(), NewValue: map[string]any{"id": d.ID},
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out, _ := s.banking.GetDeposit(ctx, actor.TenantID, d.ID)
	writeJSON(w, http.StatusCreated, toBankDepositDTO(out))
}

func (s *Server) handleListBankDeposits(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var stationID uuid.UUID
	if v := r.URL.Query().Get("station_id"); v != "" {
		if id, perr := uuid.Parse(v); perr == nil {
			stationID = id
		}
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.banking.ListDepositsPage(r.Context(), actor.TenantID, stationID, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]bankDepositDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toBankDepositDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

// handlePrepareBankDeposit posts cash on hand -> bank clearing for the deposit's
// total and moves it to prepared.
func (s *Server) handlePrepareBankDeposit(w http.ResponseWriter, r *http.Request) {
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
	dep, err := s.banking.GetDeposit(ctx, actor.TenantID, id)
	if errors.Is(err, banking.ErrNotFound) {
		writeError(w, http.StatusNotFound, "deposit not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	postDate := time.Now()
	if dep.ExpectedBankDate != nil {
		postDate = *dep.ExpectedBankDate
	}
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	amount, err := s.banking.PrepareDeposit(ctx, tx, actor.TenantID, id)
	if errors.Is(err, banking.ErrBadState) {
		writeError(w, http.StatusConflict, "deposit is not in draft state")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if isZeroMoney(amount) {
		writeError(w, http.StatusUnprocessableEntity, "deposit has no amount to prepare")
		return
	}
	station := dep.StationID
	entry, err := s.accounting.PostEntry(ctx, tx, actor.TenantID, accounting.PostEntryInput{
		EntryDate: postDate, SourceType: "bank_deposit", SourceID: &id, StationID: &station,
		PostedBy: actor.UserID, Lines: []accounting.PostLine{
			{SystemKey: "bank_clearing", Debit: amount, Credit: "0", StationID: &station},
			{SystemKey: "cash_on_hand", Debit: "0", Credit: amount, StationID: &station},
		},
	})
	if code, msg := journalErrorResponse(err); code != 0 {
		writeError(w, code, msg)
		return
	}
	if err := s.banking.SetDepositPreparedEntry(ctx, tx, actor.TenantID, id, entry.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "bank_deposit.prepared", EventType: "BankDepositPrepared", EntityType: "bank_deposit",
		EntityID: id.String(), NewValue: map[string]any{"amount": amount, "journal_entry_id": entry.ID},
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out, _ := s.banking.GetDeposit(ctx, actor.TenantID, id)
	writeJSON(w, http.StatusOK, toBankDepositDTO(out))
}

// handleConfirmBankDeposit posts bank clearing -> bank for the deposit and marks
// it posted, recording the actual bank date.
func (s *Server) handleConfirmBankDeposit(w http.ResponseWriter, r *http.Request) {
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
		ActualBankDate string  `json:"actual_bank_date"`
		Reference      *string `json:"reference,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	actual, derr := time.Parse(dateLayout, req.ActualBankDate)
	if derr != nil {
		writeError(w, http.StatusBadRequest, "actual_bank_date must be YYYY-MM-DD")
		return
	}
	ctx := r.Context()
	dep, err := s.banking.GetDeposit(ctx, actor.TenantID, id)
	if errors.Is(err, banking.ErrNotFound) {
		writeError(w, http.StatusNotFound, "deposit not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	amount, err := s.banking.ConfirmDeposit(ctx, tx, actor.TenantID, id, actual, req.Reference)
	if errors.Is(err, banking.ErrBadState) {
		writeError(w, http.StatusConflict, "deposit must be prepared before confirmation")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	station := dep.StationID
	entry, err := s.accounting.PostEntry(ctx, tx, actor.TenantID, accounting.PostEntryInput{
		EntryDate: actual, SourceType: "bank_deposit", SourceID: &id, StationID: &station,
		PostedBy: actor.UserID, Lines: []accounting.PostLine{
			{SystemKey: "bank", Debit: amount, Credit: "0", StationID: &station},
			{SystemKey: "bank_clearing", Debit: "0", Credit: amount, StationID: &station},
		},
	})
	if code, msg := journalErrorResponse(err); code != 0 {
		writeError(w, code, msg)
		return
	}
	if err := s.banking.SetDepositConfirmedEntry(ctx, tx, actor.TenantID, id, entry.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "bank_deposit.confirmed", EventType: "BankDepositConfirmed", EntityType: "bank_deposit",
		EntityID: id.String(), NewValue: map[string]any{"amount": amount, "journal_entry_id": entry.ID},
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out, _ := s.banking.GetDeposit(ctx, actor.TenantID, id)
	writeJSON(w, http.StatusOK, toBankDepositDTO(out))
}

// ---- Bank statement import & matching (Stage 6) ----

func (s *Server) handleImportBankStatement(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		BankAccountID  uuid.UUID `json:"bank_account_id"`
		StatementStart string    `json:"statement_start,omitempty"`
		StatementEnd   string    `json:"statement_end,omitempty"`
		Lines          []struct {
			TxnDate     string  `json:"txn_date"`
			ValueDate   string  `json:"value_date,omitempty"`
			Amount      string  `json:"amount"`
			Reference   *string `json:"reference,omitempty"`
			Description *string `json:"description,omitempty"`
		} `json:"lines"`
	}
	body, rerr := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if rerr != nil {
		writeError(w, http.StatusBadRequest, "could not read body")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.BankAccountID == uuid.Nil || len(req.Lines) == 0 {
		writeError(w, http.StatusBadRequest, "bank_account_id and at least one line are required")
		return
	}
	lines := make([]banking.StatementLineInput, 0, len(req.Lines))
	for _, ln := range req.Lines {
		txn, derr := time.Parse(dateLayout, ln.TxnDate)
		if derr != nil {
			writeError(w, http.StatusBadRequest, "txn_date must be YYYY-MM-DD")
			return
		}
		if _, ok := parseDecimal(ln.Amount); !ok {
			writeError(w, http.StatusBadRequest, "line amount must be a decimal")
			return
		}
		var value *time.Time
		if ln.ValueDate != "" {
			if t, verr := time.Parse(dateLayout, ln.ValueDate); verr == nil {
				value = &t
			}
		}
		lines = append(lines, banking.StatementLineInput{
			TxnDate: txn, ValueDate: value, Amount: ln.Amount, Reference: ln.Reference, Description: ln.Description,
		})
	}
	start := parseOptDate(req.StatementStart)
	end := parseOptDate(req.StatementEnd)
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	importID, inserted, err := s.banking.ImportStatement(ctx, tx, actor.TenantID, req.BankAccountID, start, end, hash, actor.UserID, lines)
	if errors.Is(err, banking.ErrDuplicate) {
		writeError(w, http.StatusConflict, "this statement has already been imported")
		return
	}
	if err != nil {
		if isForeignKeyViolation(err) {
			writeError(w, http.StatusBadRequest, "unknown bank account")
			return
		}
		s.logger.Error("import statement", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "bank_statement.imported", EventType: "BankStatementImported", EntityType: "bank_statement_import",
		EntityID: importID.String(), NewValue: map[string]any{"import_id": importID, "lines": inserted},
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"import_id": importID, "lines": inserted})
}

func (s *Server) handleListBankStatementLines(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var accountID uuid.UUID
	if v := r.URL.Query().Get("bank_account_id"); v != "" {
		if id, perr := uuid.Parse(v); perr == nil {
			accountID = id
		}
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.banking.ListStatementLinesPage(r.Context(), actor.TenantID, accountID, r.URL.Query().Get("status"), limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]statementLineDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toStatementLineDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

func (s *Server) handleMatchBankStatementLine(w http.ResponseWriter, r *http.Request) {
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
		DocType string    `json:"doc_type"`
		DocID   uuid.UUID `json:"doc_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DocType == "" || req.DocID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "doc_type and doc_id are required")
		return
	}
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "bank_statement_line.matched", EventType: "BankStatementLineMatched", EntityType: "bank_statement_line",
		EntityID: id.String(), NewValue: map[string]any{"doc_type": req.DocType, "doc_id": req.DocID},
	}, func(tx pgx.Tx) (string, error) {
		if err := s.banking.MatchLine(r.Context(), tx, actor.TenantID, id, req.DocType, req.DocID); errors.Is(err, banking.ErrBadState) {
			writeError(w, http.StatusConflict, "line cannot be matched from its current state")
			return "", err
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "matched"})
}

func (s *Server) handleUnmatchBankStatementLine(w http.ResponseWriter, r *http.Request) {
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
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "bank_statement_line.unmatched", EventType: "BankStatementLineUnmatched", EntityType: "bank_statement_line",
		EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		if err := s.banking.UnmatchLine(r.Context(), tx, actor.TenantID, id); errors.Is(err, banking.ErrNotFound) {
			writeError(w, http.StatusNotFound, "line not found")
			return "", err
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "unmatched"})
}

// handleBankFeeStatementLine posts a bank fee (debit operating expense, credit
// bank) for the line's magnitude and flags it.
func (s *Server) handleBankFeeStatementLine(w http.ResponseWriter, r *http.Request) {
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
	line, err := s.banking.GetStatementLine(ctx, actor.TenantID, id)
	if errors.Is(err, banking.ErrNotFound) {
		writeError(w, http.StatusNotFound, "line not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	amount := absMoney(line.Amount)
	if isZeroMoney(amount) {
		writeError(w, http.StatusUnprocessableEntity, "line amount is zero")
		return
	}
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	entry, err := s.accounting.PostEntry(ctx, tx, actor.TenantID, accounting.PostEntryInput{
		EntryDate: line.TxnDate, SourceType: "bank_fee", SourceID: &id, PostedBy: actor.UserID,
		Lines: []accounting.PostLine{
			{SystemKey: "operating_expense", Debit: amount, Credit: "0"},
			{SystemKey: "bank", Debit: "0", Credit: amount},
		},
	})
	if code, msg := journalErrorResponse(err); code != 0 {
		writeError(w, code, msg)
		return
	}
	if err := s.banking.MarkLine(ctx, tx, actor.TenantID, id, "bank_fee", &entry.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "bank_statement_line.bank_fee", EventType: "BankStatementLineBankFee", EntityType: "bank_statement_line",
		EntityID: id.String(), NewValue: map[string]any{"journal_entry_id": entry.ID, "amount": amount},
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "bank_fee", "journal_entry_id": entry.ID})
}

// parseOptDate parses an optional YYYY-MM-DD value, returning nil on empty/bad.
func parseOptDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	if t, err := time.Parse(dateLayout, s); err == nil {
		return &t
	}
	return nil
}
