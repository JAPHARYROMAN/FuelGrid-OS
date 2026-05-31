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
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// ---- DTOs ----

type accountDTO struct {
	ID            uuid.UUID  `json:"id"`
	Code          string     `json:"code"`
	Name          string     `json:"name"`
	Type          string     `json:"type"`
	NormalBalance string     `json:"normal_balance"`
	ParentID      *uuid.UUID `json:"parent_id,omitempty"`
	SystemKey     *string    `json:"system_key,omitempty"`
	Status        string     `json:"status"`
}

func toAccountDTO(a *accounting.Account) accountDTO {
	return accountDTO{
		ID: a.ID, Code: a.Code, Name: a.Name, Type: a.Type, NormalBalance: a.NormalBalance,
		ParentID: a.ParentID, SystemKey: a.SystemKey, Status: a.Status,
	}
}

type periodDTO struct {
	ID        uuid.UUID  `json:"id"`
	StartDate string     `json:"start_date"`
	EndDate   string     `json:"end_date"`
	Status    string     `json:"status"`
	ClosedBy  *uuid.UUID `json:"closed_by,omitempty"`
	ClosedAt  *string    `json:"closed_at,omitempty"`
	LockedBy  *uuid.UUID `json:"locked_by,omitempty"`
	LockedAt  *string    `json:"locked_at,omitempty"`
}

func toPeriodDTO(p *accounting.Period) periodDTO {
	return periodDTO{
		ID: p.ID, StartDate: p.StartDate.Format(dateLayout), EndDate: p.EndDate.Format(dateLayout),
		Status: p.Status, ClosedBy: p.ClosedBy, ClosedAt: fmtTime(p.ClosedAt),
		LockedBy: p.LockedBy, LockedAt: fmtTime(p.LockedAt),
	}
}

type journalLineDTO struct {
	ID        uuid.UUID  `json:"id"`
	AccountID uuid.UUID  `json:"account_id"`
	Debit     string     `json:"debit"`
	Credit    string     `json:"credit"`
	StationID *uuid.UUID `json:"station_id,omitempty"`
	Memo      *string    `json:"memo,omitempty"`
}

type journalEntryDTO struct {
	ID                uuid.UUID        `json:"id"`
	EntryNumber       int64            `json:"entry_number"`
	PeriodID          uuid.UUID        `json:"period_id"`
	EntryDate         string           `json:"entry_date"`
	SourceType        string           `json:"source_type"`
	SourceID          *uuid.UUID       `json:"source_id,omitempty"`
	StationID         *uuid.UUID       `json:"station_id,omitempty"`
	Status            string           `json:"status"`
	Memo              *string          `json:"memo,omitempty"`
	ReversesEntryID   *uuid.UUID       `json:"reverses_entry_id,omitempty"`
	ReversedByEntryID *uuid.UUID       `json:"reversed_by_entry_id,omitempty"`
	Total             string           `json:"total,omitempty"`
	Lines             []journalLineDTO `json:"lines,omitempty"`
}

func toJournalEntryDTO(e *accounting.JournalEntry) journalEntryDTO {
	dto := journalEntryDTO{
		ID: e.ID, EntryNumber: e.EntryNumber, PeriodID: e.PeriodID,
		EntryDate: e.EntryDate.Format(dateLayout), SourceType: e.SourceType, SourceID: e.SourceID,
		StationID: e.StationID, Status: e.Status, Memo: e.Memo,
		ReversesEntryID: e.ReversesEntryID, ReversedByEntryID: e.ReversedByEntryID, Total: e.Total,
	}
	for i := range e.Lines {
		l := e.Lines[i]
		dto.Lines = append(dto.Lines, journalLineDTO{
			ID: l.ID, AccountID: l.AccountID, Debit: l.Debit, Credit: l.Credit,
			StationID: l.StationID, Memo: l.Memo,
		})
	}
	return dto
}

// txAudit runs fn inside a tx, writes the audit record, and commits — the
// common shape for the finance write handlers below.
func (s *Server) txAudit(w http.ResponseWriter, r *http.Request, rec audit.TxRecord, fn func(tx pgx.Tx) (string, error)) bool {
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	defer func() { _ = tx.Rollback(ctx) }()
	entityID, err := fn(tx)
	if err != nil {
		return false // fn already wrote the response
	}
	if entityID != "" {
		rec.EntityID = entityID
	}
	if rec.NewValue == nil {
		rec.NewValue = map[string]any{"id": entityID}
	}
	rec.IP, rec.UserAgent, rec.RequestID = clientIP(r), r.UserAgent(), chimiddleware.GetReqID(ctx)
	if err := audit.WriteWithOutbox(ctx, tx, rec); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	return true
}

// ---- Accounts ----

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.accounting.ListAccountsPage(r.Context(), actor.TenantID, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]accountDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toAccountDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

func (s *Server) handleSeedDefaultChart(w http.ResponseWriter, r *http.Request) {
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
	n, err := s.accounting.SeedDefaultChart(ctx, tx, actor.TenantID)
	if err != nil {
		s.logger.Error("seed chart", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "chart.seeded", EventType: "ChartSeeded", EntityType: "accounts",
		EntityID: actor.TenantID.String(), NewValue: map[string]any{"created": n},
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": n})
}

type createAccountRequest struct {
	Code          string     `json:"code"`
	Name          string     `json:"name"`
	Type          string     `json:"type"`
	NormalBalance string     `json:"normal_balance"`
	ParentID      *uuid.UUID `json:"parent_id,omitempty"`
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req createAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Code == "" || req.Name == "" || req.Type == "" || req.NormalBalance == "" {
		writeError(w, http.StatusBadRequest, "code, name, type, normal_balance are required")
		return
	}
	var created *accounting.Account
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "account.created", EventType: "AccountCreated", EntityType: "account",
	}, func(tx pgx.Tx) (string, error) {
		a, err := s.accounting.CreateAccount(r.Context(), tx, actor.TenantID, accounting.AccountInput{
			Code: req.Code, Name: req.Name, Type: req.Type, NormalBalance: req.NormalBalance, ParentID: req.ParentID,
		})
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "an account with that code already exists")
			return "", err
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		created = a
		return a.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, toAccountDTO(created))
}

type updateAccountRequest struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid account id")
		return
	}
	var req updateAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ctx := r.Context()
	if req.Status == "inactive" {
		has, err := s.accounting.AccountHasPostings(ctx, actor.TenantID, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if has {
			writeError(w, http.StatusConflict, "account has postings and cannot be deactivated")
			return
		}
	}
	var updated *accounting.Account
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "account.updated", EventType: "AccountUpdated", EntityType: "account", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		a, err := s.accounting.UpdateAccount(ctx, tx, actor.TenantID, id, req.Name, req.Status)
		if errors.Is(err, accounting.ErrAccountNotFound) {
			writeError(w, http.StatusNotFound, "account not found")
			return "", err
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		updated = a
		return a.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toAccountDTO(updated))
}

// ---- Periods ----

func (s *Server) handleListPeriods(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.accounting.ListPeriodsPage(r.Context(), actor.TenantID, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]periodDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toPeriodDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

type createPeriodRequest struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

func (s *Server) handleCreatePeriod(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req createPeriodRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	start, err1 := time.Parse(dateLayout, req.StartDate)
	end, err2 := time.Parse(dateLayout, req.EndDate)
	if err1 != nil || err2 != nil {
		writeError(w, http.StatusBadRequest, "start_date and end_date must be YYYY-MM-DD")
		return
	}
	var created *accounting.Period
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "accounting_period.created", EventType: "AccountingPeriodCreated", EntityType: "accounting_period",
	}, func(tx pgx.Tx) (string, error) {
		p, err := s.accounting.CreatePeriod(r.Context(), tx, actor.TenantID, start, end)
		if errors.Is(err, accounting.ErrPeriodOverlap) {
			writeError(w, http.StatusConflict, "period overlaps an existing period")
			return "", err
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		created = p
		return p.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, toPeriodDTO(created))
}

func (s *Server) handlePeriodTransition(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, err := identity.Require(r.Context())
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid period id")
			return
		}
		ctx := r.Context()
		var result *accounting.Period
		ok := s.txAudit(w, r, audit.TxRecord{
			TenantID: actor.TenantID, ActorID: actor.UserID,
			Action: "accounting_period." + action, EventType: "AccountingPeriod" + action,
			EntityType: "accounting_period", EntityID: id.String(),
		}, func(tx pgx.Tx) (string, error) {
			// ACCT-004: closing or locking a period is refused while the
			// close checklist has unresolved blockers (unposted cash recons,
			// in-flight deposits, unmatched bank lines, expenses awaiting
			// posting, draft invoices). Checked inside the tx for consistency.
			if action == "closed" || action == "locked" {
				_, blockers, cErr := s.periodCloseChecklist(ctx, tx, actor.TenantID)
				if cErr != nil {
					writeError(w, http.StatusInternalServerError, "internal error")
					return "", cErr
				}
				if blockers > 0 {
					writeError(w, http.StatusUnprocessableEntity, "period has unresolved close-checklist blockers; clear them before closing or locking")
					return "", errors.New("period close blocked by checklist")
				}
			}
			var p *accounting.Period
			var tErr error
			switch action {
			case "start_close":
				p, tErr = s.accounting.StartClose(ctx, tx, actor.TenantID, id, actor.UserID)
			case "closed":
				p, tErr = s.accounting.ClosePeriod(ctx, tx, actor.TenantID, id, actor.UserID)
			case "reopened":
				p, tErr = s.accounting.ReopenPeriod(ctx, tx, actor.TenantID, id, actor.UserID)
			case "locked":
				p, tErr = s.accounting.LockPeriod(ctx, tx, actor.TenantID, id, actor.UserID)
			}
			if errors.Is(tErr, accounting.ErrPeriodNotFound) {
				writeError(w, http.StatusNotFound, "period not found")
				return "", tErr
			}
			if errors.Is(tErr, accounting.ErrPeriodTransition) {
				writeError(w, http.StatusConflict, "invalid period transition")
				return "", tErr
			}
			if tErr != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return "", tErr
			}
			result = p
			return id.String(), nil
		})
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, toPeriodDTO(result))
	}
}

// ---- Journal ----

func (s *Server) handleListJournalEntries(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.accounting.ListEntriesPage(r.Context(), actor.TenantID, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]journalEntryDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toJournalEntryDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

func (s *Server) handleGetJournalEntry(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid entry id")
		return
	}
	e, err := s.accounting.GetEntry(r.Context(), actor.TenantID, id)
	if errors.Is(err, accounting.ErrEntryNotFound) {
		writeError(w, http.StatusNotFound, "journal entry not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toJournalEntryDTO(e))
}

type postAdjustmentRequest struct {
	EntryDate string  `json:"entry_date"`
	Memo      *string `json:"memo,omitempty"`
	Lines     []struct {
		AccountID *uuid.UUID `json:"account_id,omitempty"`
		SystemKey string     `json:"system_key,omitempty"`
		Debit     string     `json:"debit"`
		Credit    string     `json:"credit"`
		Memo      *string    `json:"memo,omitempty"`
	} `json:"lines"`
}

func (s *Server) handlePostAdjustment(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req postAdjustmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	date, derr := time.Parse(dateLayout, req.EntryDate)
	if derr != nil {
		writeError(w, http.StatusBadRequest, "entry_date must be YYYY-MM-DD")
		return
	}
	if len(req.Lines) < 2 {
		writeError(w, http.StatusBadRequest, "an adjustment needs at least two lines")
		return
	}
	lines := make([]accounting.PostLine, 0, len(req.Lines))
	for _, l := range req.Lines {
		pl := accounting.PostLine{SystemKey: l.SystemKey, Debit: l.Debit, Credit: l.Credit, Memo: l.Memo}
		if l.AccountID != nil {
			pl.AccountID = *l.AccountID
		}
		lines = append(lines, pl)
	}
	var posted *accounting.JournalEntry
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "journal_entry.posted", EventType: "JournalEntryPosted", EntityType: "journal_entry",
	}, func(tx pgx.Tx) (string, error) {
		e, err := s.accounting.PostEntry(r.Context(), tx, actor.TenantID, accounting.PostEntryInput{
			EntryDate: date, SourceType: "adjustment", Memo: req.Memo, PostedBy: actor.UserID,
			AllowClosed: true, Lines: lines,
		})
		if code, msg := journalErrorResponse(err); code != 0 {
			writeError(w, code, msg)
			return "", err
		}
		posted = e
		return e.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, toJournalEntryDTO(posted))
}

func (s *Server) handleReverseJournalEntry(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid entry id")
		return
	}
	var body struct {
		Memo *string `json:"memo,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	var rev *accounting.JournalEntry
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "journal_entry.reversed", EventType: "JournalEntryReversed", EntityType: "journal_entry", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		e, err := s.accounting.ReverseEntry(r.Context(), tx, actor.TenantID, id, actor.UserID, body.Memo)
		if errors.Is(err, accounting.ErrEntryNotFound) {
			writeError(w, http.StatusNotFound, "journal entry not found")
			return "", err
		}
		if errors.Is(err, accounting.ErrAlreadyReversed) {
			writeError(w, http.StatusConflict, "journal entry already reversed")
			return "", err
		}
		if errors.Is(err, accounting.ErrPeriodLocked) {
			writeError(w, http.StatusConflict, "the entry's period is locked")
			return "", err
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		rev = e
		return e.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, toJournalEntryDTO(rev))
}

// journalErrorResponse maps posting errors to HTTP responses; returns (0,"")
// when err is nil.
func journalErrorResponse(err error) (int, string) {
	switch {
	case err == nil:
		return 0, ""
	case errors.Is(err, accounting.ErrUnbalanced):
		return http.StatusUnprocessableEntity, "entry debits do not equal credits"
	case errors.Is(err, accounting.ErrNoLines):
		return http.StatusBadRequest, "entry has no lines"
	case errors.Is(err, accounting.ErrNoPeriod):
		return http.StatusUnprocessableEntity, "no accounting period covers this date"
	case errors.Is(err, accounting.ErrPeriodClosed):
		return http.StatusConflict, "the period is closed"
	case errors.Is(err, accounting.ErrPeriodLocked):
		return http.StatusConflict, "the period is locked"
	case errors.Is(err, accounting.ErrSystemAccount):
		return http.StatusBadRequest, "a referenced system account is not configured; seed the chart of accounts"
	default:
		return http.StatusInternalServerError, "internal error"
	}
}
