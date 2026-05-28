package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/fleet"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// validCustomerStatuses is the Phase-8 account lifecycle.
var validCustomerStatuses = map[string]bool{
	"prospect": true, "active": true, "on_hold": true, "suspended": true, "closed": true,
}

// ---- Customer status (Stage 1) ----

func (s *Server) handleSetCustomerStatus(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid customer id")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validCustomerStatuses[req.Status] {
		writeError(w, http.StatusBadRequest, "status must be prospect|active|on_hold|suspended|closed")
		return
	}
	var dto customerDTO
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer.status_changed", EventType: "CustomerStatusChanged", EntityType: "customer",
		EntityID: id.String(), NewValue: map[string]any{"status": req.Status},
	}, func(tx pgx.Tx) (string, error) {
		c, err := s.receivables.SetCustomerStatus(r.Context(), tx, actor.TenantID, id, req.Status)
		if err != nil {
			writeError(w, http.StatusNotFound, "customer not found")
			return "", err
		}
		dto = toCustomerDTO(c)
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// ---- Customer contacts (Stage 1) ----

func (s *Server) handleListCustomerContacts(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	customerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid customer id")
		return
	}
	rows, err := s.fleet.ListContacts(r.Context(), actor.TenantID, customerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		c := rows[i]
		out = append(out, map[string]any{
			"id": c.ID, "name": c.Name, "role": c.Role, "email": c.Email, "phone": c.Phone,
			"statement_preference": c.StatementPreference, "notification_preference": c.NotificationPreference,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleCreateCustomerContact(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	customerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid customer id")
		return
	}
	var req struct {
		Name                   string  `json:"name"`
		Role                   *string `json:"role,omitempty"`
		Email                  *string `json:"email,omitempty"`
		Phone                  *string `json:"phone,omitempty"`
		StatementPreference    string  `json:"statement_preference,omitempty"`
		NotificationPreference string  `json:"notification_preference,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	var contactID uuid.UUID
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer_contact.created", EventType: "CustomerContactCreated", EntityType: "customer_contact",
	}, func(tx pgx.Tx) (string, error) {
		c, err := s.fleet.CreateContact(r.Context(), tx, actor.TenantID, customerID, fleet.ContactInput{
			Name: req.Name, Role: req.Role, Email: req.Email, Phone: req.Phone,
			StatementPreference: req.StatementPreference, NotificationPreference: req.NotificationPreference,
		})
		if err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown customer")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		contactID = c.ID
		return c.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": contactID})
}

func (s *Server) handleDeleteCustomerContact(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	customerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid customer id")
		return
	}
	contactID, err := uuid.Parse(chi.URLParam(r, "contactID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid contact id")
		return
	}
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer_contact.deleted", EventType: "CustomerContactDeleted", EntityType: "customer_contact",
		EntityID: contactID.String(),
	}, func(tx pgx.Tx) (string, error) {
		if err := s.fleet.DeleteContact(r.Context(), tx, actor.TenantID, customerID, contactID); errors.Is(err, fleet.ErrNotFound) {
			writeError(w, http.StatusNotFound, "contact not found")
			return "", err
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		return contactID.String(), nil
	})
	if !ok {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Credit profile & position (Stage 2) ----

func (s *Server) handleGetCreditProfile(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	customerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid customer id")
		return
	}
	p, err := s.fleet.GetCreditProfile(r.Context(), actor.TenantID, customerID)
	if errors.Is(err, fleet.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no credit profile for this customer")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toCreditProfileMap(p))
}

func toCreditProfileMap(p *fleet.CreditProfile) map[string]any {
	return map[string]any{
		"customer_id": p.CustomerID, "payment_terms_days": p.PaymentTermsDays, "grace_days": p.GraceDays,
		"statement_cycle_days": p.StatementCycleDays, "risk_category": p.RiskCategory,
		"warning_threshold_pct": p.WarningThresholdPct, "hold": p.Hold, "hold_reason": p.HoldReason,
	}
}

func (s *Server) handleUpsertCreditProfile(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	customerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid customer id")
		return
	}
	var req struct {
		PaymentTermsDays    *int    `json:"payment_terms_days,omitempty"`
		GraceDays           *int    `json:"grace_days,omitempty"`
		StatementCycleDays  *int    `json:"statement_cycle_days,omitempty"`
		RiskCategory        string  `json:"risk_category,omitempty"`
		WarningThresholdPct *string `json:"warning_threshold_pct,omitempty"`
		ReviewDate          string  `json:"review_date,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var p *fleet.CreditProfile
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer_credit.updated", EventType: "CustomerCreditUpdated", EntityType: "customer_credit_profile",
		EntityID: customerID.String(),
	}, func(tx pgx.Tx) (string, error) {
		prof, err := s.fleet.UpsertCreditProfile(r.Context(), tx, actor.TenantID, customerID, fleet.CreditProfileInput{
			PaymentTermsDays: req.PaymentTermsDays, GraceDays: req.GraceDays, StatementCycleDays: req.StatementCycleDays,
			RiskCategory: req.RiskCategory, WarningThresholdPct: req.WarningThresholdPct, ReviewDate: parseOptDate(req.ReviewDate),
		})
		if err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown customer")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		p = prof
		return customerID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toCreditProfileMap(p))
}

func (s *Server) handleCreditPosition(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	customerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid customer id")
		return
	}
	p, err := s.fleet.CreditPosition(r.Context(), s.deps.DB, actor.TenantID, customerID)
	if errors.Is(err, fleet.ErrNotFound) {
		writeError(w, http.StatusNotFound, "customer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"customer_id": p.CustomerID, "credit_limit": p.CreditLimit, "exposure": p.Exposure,
		"available_credit": p.Available, "overdue_amount": p.Overdue, "status": p.Status,
		"hold": p.Hold, "hold_reason": p.HoldReason, "over_limit": p.OverLimit, "warning": p.Warning,
	})
}

func (s *Server) handleSetCreditHold(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	customerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid customer id")
		return
	}
	var req struct {
		Hold   bool    `json:"hold"`
		Reason *string `json:"reason,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	action := "customer_credit.hold_released"
	if req.Hold {
		action = "customer_credit.hold_applied"
	}
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: action, EventType: "CustomerCreditHold", EntityType: "customer_credit_profile",
		EntityID: customerID.String(), NewValue: map[string]any{"hold": req.Hold},
	}, func(tx pgx.Tx) (string, error) {
		if err := s.fleet.SetHold(r.Context(), tx, actor.TenantID, customerID, req.Hold, req.Reason); err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown customer")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		return customerID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"customer_id": customerID, "hold": req.Hold})
}

// ---- Customer price agreements (Stage 3) ----

func toPriceAgreementMap(a *fleet.PriceAgreement) map[string]any {
	return map[string]any{
		"id": a.ID, "customer_id": a.CustomerID, "product_id": a.ProductID, "station_id": a.StationID,
		"price_type": a.PriceType, "fixed_price": a.FixedPrice, "discount": a.Discount, "markup": a.Markup,
		"effective_from": a.EffectiveFrom.Format(dateLayout), "effective_to": fmtDate(a.EffectiveTo),
		"status": a.Status, "version": a.Version,
	}
}

func (s *Server) handleListPriceAgreements(w http.ResponseWriter, r *http.Request) {
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
	rows, err := s.fleet.ListPriceAgreements(r.Context(), actor.TenantID, customerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, toPriceAgreementMap(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleCreatePriceAgreement(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		CustomerID    uuid.UUID  `json:"customer_id"`
		ProductID     uuid.UUID  `json:"product_id"`
		StationID     *uuid.UUID `json:"station_id,omitempty"`
		PriceType     string     `json:"price_type"`
		FixedPrice    *string    `json:"fixed_price,omitempty"`
		Discount      *string    `json:"discount,omitempty"`
		Markup        *string    `json:"markup,omitempty"`
		EffectiveFrom string     `json:"effective_from,omitempty"`
		EffectiveTo   string     `json:"effective_to,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.CustomerID == uuid.Nil || req.ProductID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "customer_id and product_id are required")
		return
	}
	from := time.Now()
	if req.EffectiveFrom != "" {
		if t, derr := time.Parse(dateLayout, req.EffectiveFrom); derr == nil {
			from = t
		} else {
			writeError(w, http.StatusBadRequest, "effective_from must be YYYY-MM-DD")
			return
		}
	}
	var agreement *fleet.PriceAgreement
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer_price_agreement.created", EventType: "CustomerPriceAgreementCreated", EntityType: "customer_price_agreement",
	}, func(tx pgx.Tx) (string, error) {
		a, err := s.fleet.CreatePriceAgreement(r.Context(), tx, actor.TenantID, fleet.PriceAgreementInput{
			CustomerID: req.CustomerID, ProductID: req.ProductID, StationID: req.StationID, PriceType: req.PriceType,
			FixedPrice: req.FixedPrice, Discount: req.Discount, Markup: req.Markup,
			EffectiveFrom: from, EffectiveTo: parseOptDate(req.EffectiveTo), CreatedBy: actor.UserID,
		})
		if err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown customer, product, or station")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		agreement = a
		return a.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, toPriceAgreementMap(agreement))
}

// handleTransitionPriceAgreement applies approve|activate|expire|cancel.
func (s *Server) handleTransitionPriceAgreement(action string) http.HandlerFunc {
	statusFor := map[string]string{"approve": "approved", "activate": "active", "expire": "expired", "cancel": "cancelled"}
	return func(w http.ResponseWriter, r *http.Request) {
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
		to := statusFor[action]
		var agreement *fleet.PriceAgreement
		ok := s.txAudit(w, r, audit.TxRecord{
			TenantID: actor.TenantID, ActorID: actor.UserID,
			Action: "customer_price_agreement." + to, EventType: "CustomerPriceAgreement", EntityType: "customer_price_agreement",
			EntityID: id.String(),
		}, func(tx pgx.Tx) (string, error) {
			var approver *uuid.UUID
			if to == "approved" {
				approver = &actor.UserID
			}
			a, err := s.fleet.TransitionPriceAgreement(r.Context(), tx, actor.TenantID, id, to, approver)
			if errors.Is(err, fleet.ErrBadState) {
				writeError(w, http.StatusConflict, "agreement is not in the required state for this action")
				return "", err
			}
			if errors.Is(err, fleet.ErrConflict) {
				writeError(w, http.StatusConflict, "another active agreement exists for this customer/product/scope")
				return "", err
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return "", err
			}
			agreement = a
			return id.String(), nil
		})
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, toPriceAgreementMap(agreement))
	}
}
