package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/accounting"
	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/expenses"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

type expenseDTO struct {
	ID             uuid.UUID  `json:"id"`
	StationID      *uuid.UUID `json:"station_id,omitempty"`
	CategoryID     *uuid.UUID `json:"category_id,omitempty"`
	Payee          *string    `json:"payee,omitempty"`
	ExpenseDate    string     `json:"expense_date"`
	Amount         string     `json:"amount"`
	AccountKey     string     `json:"account_key"`
	PaymentMode    string     `json:"payment_mode"`
	Reference      *string    `json:"reference,omitempty"`
	Status         string     `json:"status"`
	JournalEntryID *uuid.UUID `json:"journal_entry_id,omitempty"`
	ApprovedBy     *uuid.UUID `json:"approved_by,omitempty"`
}

func toExpenseDTO(e *expenses.Expense) expenseDTO {
	return expenseDTO{
		ID: e.ID, StationID: e.StationID, CategoryID: e.CategoryID, Payee: e.Payee,
		ExpenseDate: e.ExpenseDate.Format(dateLayout), Amount: e.Amount, AccountKey: e.AccountKey,
		PaymentMode: e.PaymentMode, Reference: e.Reference, Status: e.Status,
		JournalEntryID: e.JournalEntryID, ApprovedBy: e.ApprovedBy,
	}
}

// paymentModeAccount maps an expense payment mode to the system account credited
// when the expense posts.
func paymentModeAccount(mode string) string {
	switch mode {
	case "bank":
		return "bank"
	case "payable":
		return "accounts_payable"
	case "petty_cash":
		return "petty_cash"
	default:
		return "cash_on_hand"
	}
}

// ---- Expense categories ----

func (s *Server) handleCreateExpenseCategory(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Name       string `json:"name"`
		AccountKey string `json:"account_key,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	var cat *expenses.Category
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "expense_category.created", EventType: "ExpenseCategoryCreated", EntityType: "expense_category",
	}, func(tx pgx.Tx) (string, error) {
		c, err := s.expenses.CreateCategory(r.Context(), tx, actor.TenantID, req.Name, req.AccountKey)
		if err != nil {
			if isUniqueViolation(err) {
				writeError(w, http.StatusConflict, "a category with this name already exists")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		cat = c
		return c.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": cat.ID, "name": cat.Name, "account_key": cat.AccountKey, "status": cat.Status,
	})
}

func (s *Server) handleListExpenseCategories(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.expenses.ListCategories(r.Context(), actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, map[string]any{
			"id": rows[i].ID, "name": rows[i].Name, "account_key": rows[i].AccountKey, "status": rows[i].Status,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

// ---- Expenses ----

func (s *Server) handleCreateExpense(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		StationID   *uuid.UUID `json:"station_id,omitempty"`
		CategoryID  *uuid.UUID `json:"category_id,omitempty"`
		Payee       *string    `json:"payee,omitempty"`
		ExpenseDate string     `json:"expense_date,omitempty"`
		Amount      string     `json:"amount"`
		AccountKey  string     `json:"account_key,omitempty"`
		PaymentMode string     `json:"payment_mode,omitempty"`
		Reference   *string    `json:"reference,omitempty"`
		Notes       *string    `json:"notes,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if v, ok := parseDecimal(req.Amount); !ok || v <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be a positive decimal")
		return
	}
	expenseDate := time.Now()
	if req.ExpenseDate != "" {
		t, derr := time.Parse(dateLayout, req.ExpenseDate)
		if derr != nil {
			writeError(w, http.StatusBadRequest, "expense_date must be YYYY-MM-DD")
			return
		}
		expenseDate = t
	}
	var exp *expenses.Expense
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "expense.created", EventType: "ExpenseCreated", EntityType: "expense",
	}, func(tx pgx.Tx) (string, error) {
		e, err := s.expenses.CreateExpense(r.Context(), tx, actor.TenantID, expenses.ExpenseInput{
			StationID: req.StationID, CategoryID: req.CategoryID, Payee: req.Payee, ExpenseDate: expenseDate,
			Amount: req.Amount, AccountKey: req.AccountKey, PaymentMode: req.PaymentMode,
			Reference: req.Reference, Notes: req.Notes, CreatedBy: actor.UserID,
		})
		if err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown station or category")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		exp = e
		return e.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, toExpenseDTO(exp))
}

func (s *Server) handleListExpenses(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.expenses.ListExpenses(r.Context(), actor.TenantID, r.URL.Query().Get("status"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]expenseDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toExpenseDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleGetExpense(w http.ResponseWriter, r *http.Request) {
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
	e, err := s.expenses.GetExpense(r.Context(), actor.TenantID, id)
	if errors.Is(err, expenses.ErrNotFound) {
		writeError(w, http.StatusNotFound, "expense not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toExpenseDTO(e))
}

func (s *Server) handleSubmitExpense(w http.ResponseWriter, r *http.Request) {
	s.expenseTransition(w, r, "submitted", func(tx pgx.Tx, actor identity.Actor, id uuid.UUID) (*expenses.Expense, error) {
		return s.expenses.SubmitExpense(r.Context(), tx, actor.TenantID, id)
	})
}

func (s *Server) handleApproveExpense(w http.ResponseWriter, r *http.Request) {
	s.expenseTransition(w, r, "approved", func(tx pgx.Tx, actor identity.Actor, id uuid.UUID) (*expenses.Expense, error) {
		return s.expenses.ApproveExpense(r.Context(), tx, actor.TenantID, id, actor.UserID)
	})
}

// expenseTransition runs a draft->submitted / submitted->approved transition
// inside an audited tx.
func (s *Server) expenseTransition(w http.ResponseWriter, r *http.Request, action string, fn func(tx pgx.Tx, actor identity.Actor, id uuid.UUID) (*expenses.Expense, error)) {
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
	var exp *expenses.Expense
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "expense." + action, EventType: "Expense" + action, EntityType: "expense", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		e, err := fn(tx, actor, id)
		if errors.Is(err, expenses.ErrSelfApproval) {
			writeError(w, http.StatusForbidden, "separation of duties: you cannot approve an expense you created")
			return "", err
		}
		if errors.Is(err, expenses.ErrBadState) {
			writeError(w, http.StatusConflict, "expense is not in the required state for this action")
			return "", err
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		exp = e
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toExpenseDTO(exp))
}

// handlePostExpense posts the approved expense: debit the expense account,
// credit the payment-mode account, and finalize it.
func (s *Server) handlePostExpense(w http.ResponseWriter, r *http.Request) {
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
	exp, err := s.expenses.GetExpense(ctx, actor.TenantID, id)
	if errors.Is(err, expenses.ErrNotFound) {
		writeError(w, http.StatusNotFound, "expense not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if exp.Status != "approved" {
		writeError(w, http.StatusConflict, "expense must be approved before posting")
		return
	}
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	entry, err := s.accounting.PostEntry(ctx, tx, actor.TenantID, accounting.PostEntryInput{
		EntryDate: exp.ExpenseDate, SourceType: "expense", SourceID: &id, StationID: exp.StationID,
		PostedBy: actor.UserID, Lines: []accounting.PostLine{
			{SystemKey: exp.AccountKey, Debit: exp.Amount, Credit: "0", StationID: exp.StationID},
			{SystemKey: paymentModeAccount(exp.PaymentMode), Debit: "0", Credit: exp.Amount, StationID: exp.StationID},
		},
	})
	if code, msg := journalErrorResponse(err); code != 0 {
		writeError(w, code, msg)
		return
	}
	if _, err := s.expenses.MarkExpensePosted(ctx, tx, actor.TenantID, id, entry.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "expense.posted", EventType: "ExpensePosted", EntityType: "expense",
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
	out, _ := s.expenses.GetExpense(ctx, actor.TenantID, id)
	writeJSON(w, http.StatusOK, toExpenseDTO(out))
}

// ---- Petty cash (Stage 12) ----

func (s *Server) handleCreatePettyCashFloat(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		StationID uuid.UUID `json:"station_id"`
		Name      string    `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.StationID == uuid.Nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "station_id and name are required")
		return
	}
	var fl *expenses.Float
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "petty_cash.float_created", EventType: "PettyCashFloatCreated", EntityType: "petty_cash_float",
	}, func(tx pgx.Tx) (string, error) {
		f, err := s.expenses.CreateFloat(r.Context(), tx, actor.TenantID, req.StationID, req.Name, actor.UserID)
		if err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown station")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		fl = f
		return f.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, toFloatMap(fl))
}

func toFloatMap(f *expenses.Float) map[string]any {
	return map[string]any{
		"id": f.ID, "station_id": f.StationID, "name": f.Name, "balance": f.Balance, "status": f.Status,
	}
}

func (s *Server) handleListPettyCashFloats(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.expenses.ListFloats(r.Context(), actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, toFloatMap(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleGetPettyCashFloat(w http.ResponseWriter, r *http.Request) {
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
	f, err := s.expenses.GetFloat(r.Context(), actor.TenantID, id)
	if errors.Is(err, expenses.ErrNotFound) {
		writeError(w, http.StatusNotFound, "float not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toFloatMap(f))
}

// handlePettyCashTransaction records a float transaction and posts its journal
// entry. Every money-moving type is journaled so the float can never drift from
// the GL: top-up/reimbursement move bank -> petty cash; spend moves petty cash
// -> expense; adjustment books an over/short gain into petty cash; transfer
// returns petty cash -> bank.
func (s *Server) handlePettyCashTransaction(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	floatID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		TxnType     string  `json:"txn_type"`
		Amount      string  `json:"amount"`
		Date        string  `json:"date,omitempty"`
		Description *string `json:"description,omitempty"`
		AccountKey  *string `json:"account_key,omitempty"`
		Overdraw    bool    `json:"overdraw,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	switch req.TxnType {
	case "topup", "spend", "reimbursement", "adjustment", "transfer":
	default:
		writeError(w, http.StatusBadRequest, "txn_type must be topup|spend|reimbursement|adjustment|transfer")
		return
	}
	if v, ok := parseDecimal(req.Amount); !ok || v <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be a positive decimal")
		return
	}
	postDate := time.Now()
	if req.Date != "" {
		t, derr := time.Parse(dateLayout, req.Date)
		if derr != nil {
			writeError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
			return
		}
		postDate = t
	}
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	txn, err := s.expenses.RecordTransaction(ctx, tx, actor.TenantID, floatID, req.TxnType, req.Amount, req.Description, req.AccountKey, req.Overdraw, actor.UserID)
	switch {
	case errors.Is(err, expenses.ErrNotFound):
		writeError(w, http.StatusNotFound, "float not found")
		return
	case errors.Is(err, expenses.ErrFloatBusy):
		writeError(w, http.StatusConflict, "float is not active")
		return
	case errors.Is(err, expenses.ErrOverdraw):
		writeError(w, http.StatusUnprocessableEntity, "transaction would overdraw the float; set overdraw to override")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Build the journal lines for the money-moving types.
	var lines []accounting.PostLine
	switch req.TxnType {
	case "topup", "reimbursement":
		lines = []accounting.PostLine{
			{SystemKey: "petty_cash", Debit: req.Amount, Credit: "0"},
			{SystemKey: "bank", Debit: "0", Credit: req.Amount},
		}
	case "spend":
		key := "operating_expense"
		if req.AccountKey != nil && *req.AccountKey != "" {
			key = *req.AccountKey
		}
		lines = []accounting.PostLine{
			{SystemKey: key, Debit: req.Amount, Credit: "0"},
			{SystemKey: "petty_cash", Debit: "0", Credit: req.Amount},
		}
	case "adjustment":
		// An adjustment increases the float with no external source — the
		// offset is cash over/short (an unexplained gain), mirroring how a
		// reconciliation overage is booked. Without this, the float balance
		// moved but the GL never saw it (audit ACCT-012).
		lines = []accounting.PostLine{
			{SystemKey: "petty_cash", Debit: req.Amount, Credit: "0"},
			{SystemKey: "cash_over_short", Debit: "0", Credit: req.Amount},
		}
	case "transfer":
		// A transfer returns float cash to the bank — the reverse of a top-up.
		lines = []accounting.PostLine{
			{SystemKey: "bank", Debit: req.Amount, Credit: "0"},
			{SystemKey: "petty_cash", Debit: "0", Credit: req.Amount},
		}
	}
	if len(lines) > 0 {
		entry, perr := s.accounting.PostEntry(ctx, tx, actor.TenantID, accounting.PostEntryInput{
			EntryDate: postDate, SourceType: "petty_cash", SourceID: &txn.ID, PostedBy: actor.UserID, Lines: lines,
		})
		if code, msg := journalErrorResponse(perr); code != 0 {
			writeError(w, code, msg)
			return
		}
		if err := s.expenses.SetTransactionJournalEntry(ctx, tx, actor.TenantID, txn.ID, entry.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		// Reflect the linked entry on the response (RecordTransaction returns the
		// row before the journal is posted, so its JournalEntryID was nil).
		entryID := entry.ID
		txn.JournalEntryID = &entryID
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "petty_cash." + req.TxnType, EventType: "PettyCashTransaction", EntityType: "petty_cash_transaction",
		EntityID: txn.ID.String(), NewValue: map[string]any{"balance_after": txn.BalanceAfter},
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": txn.ID, "txn_type": txn.TxnType, "amount": txn.Amount, "balance_after": txn.BalanceAfter,
		"overdraw": txn.Overdraw, "journal_entry_id": txn.JournalEntryID,
	})
}

func (s *Server) handleListPettyCashTransactions(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	floatID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	rows, err := s.expenses.ListTransactions(r.Context(), actor.TenantID, floatID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		t := rows[i]
		out = append(out, map[string]any{
			"id": t.ID, "txn_type": t.TxnType, "amount": t.Amount, "balance_after": t.BalanceAfter,
			"description": t.Description, "account_key": t.AccountKey, "overdraw": t.Overdraw,
			"journal_entry_id": t.JournalEntryID, "created_at": t.CreatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

// handleReconcilePettyCash counts a float against its expected balance and posts
// the variance to cash over/short.
func (s *Server) handleReconcilePettyCash(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	floatID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		CountedCash string `json:"counted_cash"`
		Date        string `json:"date,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if v, ok := parseDecimal(req.CountedCash); !ok || v < 0 {
		writeError(w, http.StatusBadRequest, "counted_cash must be a non-negative decimal")
		return
	}
	postDate := time.Now()
	if req.Date != "" {
		t, derr := time.Parse(dateLayout, req.Date)
		if derr != nil {
			writeError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
			return
		}
		postDate = t
	}
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rec, err := s.expenses.ReconcileFloat(ctx, tx, actor.TenantID, floatID, req.CountedCash, actor.UserID)
	if errors.Is(err, expenses.ErrNotFound) {
		writeError(w, http.StatusNotFound, "float not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var lines []accounting.PostLine
	if !isZeroMoney(rec.ShortAmount) {
		lines = []accounting.PostLine{
			{SystemKey: "cash_over_short", Debit: rec.ShortAmount, Credit: "0"},
			{SystemKey: "petty_cash", Debit: "0", Credit: rec.ShortAmount},
		}
	} else if !isZeroMoney(rec.OverAmount) {
		lines = []accounting.PostLine{
			{SystemKey: "petty_cash", Debit: rec.OverAmount, Credit: "0"},
			{SystemKey: "cash_over_short", Debit: "0", Credit: rec.OverAmount},
		}
	}
	if len(lines) > 0 {
		entry, perr := s.accounting.PostEntry(ctx, tx, actor.TenantID, accounting.PostEntryInput{
			EntryDate: postDate, SourceType: "petty_cash_reconciliation", SourceID: &rec.ID, PostedBy: actor.UserID, Lines: lines,
		})
		if code, msg := journalErrorResponse(perr); code != 0 {
			writeError(w, code, msg)
			return
		}
		if err := s.expenses.SetReconciliationJournalEntry(ctx, tx, actor.TenantID, rec.ID, entry.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "petty_cash.reconciled", EventType: "PettyCashReconciled", EntityType: "petty_cash_reconciliation",
		EntityID: rec.ID.String(), NewValue: map[string]any{"variance": rec.Variance},
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": rec.ID, "expected_balance": rec.ExpectedBalance, "counted_cash": rec.CountedCash, "variance": rec.Variance,
	})
}
