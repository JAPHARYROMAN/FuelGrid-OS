package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/fleet"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// ---- Vehicles (Stage 4) ----

func vehicleMap(v *fleet.Vehicle) map[string]any {
	return map[string]any{
		"id": v.ID, "customer_id": v.CustomerID, "registration": v.Registration, "fleet_number": v.FleetNumber,
		"vin": v.VIN, "vehicle_type": v.VehicleType, "default_product_id": v.DefaultProductID,
		"tank_capacity": v.TankCapacity, "odometer_required": v.OdometerRequired, "status": v.Status,
	}
}

func (s *Server) handleListVehicles(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.fleet.ListVehiclesPage(r.Context(), actor.TenantID, queryUUID(r, "customer_id"), limit+1, offset)
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
		out = append(out, vehicleMap(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

func (s *Server) handleCreateVehicle(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		CustomerID       uuid.UUID  `json:"customer_id"`
		Registration     string     `json:"registration"`
		FleetNumber      *string    `json:"fleet_number,omitempty"`
		VIN              *string    `json:"vin,omitempty"`
		VehicleType      *string    `json:"vehicle_type,omitempty"`
		DefaultProductID *uuid.UUID `json:"default_product_id,omitempty"`
		TankCapacity     *string    `json:"tank_capacity,omitempty"`
		OdometerRequired bool       `json:"odometer_required,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CustomerID == uuid.Nil || req.Registration == "" {
		writeError(w, http.StatusBadRequest, "customer_id and registration are required")
		return
	}
	var v *fleet.Vehicle
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer_vehicle.created", EventType: "CustomerVehicleCreated", EntityType: "customer_vehicle",
	}, func(tx pgx.Tx) (string, error) {
		veh, err := s.fleet.CreateVehicle(r.Context(), tx, actor.TenantID, fleet.VehicleInput{
			CustomerID: req.CustomerID, Registration: req.Registration, FleetNumber: req.FleetNumber, VIN: req.VIN,
			VehicleType: req.VehicleType, DefaultProductID: req.DefaultProductID, TankCapacity: req.TankCapacity,
			OdometerRequired: req.OdometerRequired,
		})
		if errors.Is(err, fleet.ErrConflict) {
			writeError(w, http.StatusConflict, "a vehicle with this registration already exists")
			return "", err
		}
		if err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown customer or product")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		v = veh
		return veh.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, vehicleMap(v))
}

func (s *Server) handleSetVehicleStatus(w http.ResponseWriter, r *http.Request) {
	s.fleetStatusTransition(w, r, "customer_vehicle", map[string]bool{"active": true, "on_hold": true, "retired": true},
		func(tx pgx.Tx, actor identity.Actor, id uuid.UUID, status string) (map[string]any, error) {
			v, err := s.fleet.SetVehicleStatus(r.Context(), tx, actor.TenantID, id, status)
			if err != nil {
				return nil, err
			}
			return vehicleMap(v), nil
		})
}

// ---- Drivers (Stage 5) ----

func driverMap(d *fleet.Driver) map[string]any {
	return map[string]any{
		"id": d.ID, "customer_id": d.CustomerID, "name": d.Name, "phone": d.Phone,
		"license_number": d.LicenseNumber, "has_pin": d.HasPIN, "status": d.Status,
		"allowed_product_ids": d.AllowedProductIDs, "assignment_rule": d.AssignmentRule,
	}
}

func (s *Server) handleListDrivers(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.fleet.ListDriversPage(r.Context(), actor.TenantID, queryUUID(r, "customer_id"), limit+1, offset)
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
		out = append(out, driverMap(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

func (s *Server) handleCreateDriver(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		CustomerID        uuid.UUID   `json:"customer_id"`
		Name              string      `json:"name"`
		Phone             *string     `json:"phone,omitempty"`
		LicenseNumber     *string     `json:"license_number,omitempty"`
		PIN               *string     `json:"pin,omitempty"`
		AllowedProductIDs []uuid.UUID `json:"allowed_product_ids,omitempty"`
		AssignmentRule    string      `json:"assignment_rule,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CustomerID == uuid.Nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "customer_id and name are required")
		return
	}
	var d *fleet.Driver
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer_driver.created", EventType: "CustomerDriverCreated", EntityType: "customer_driver",
	}, func(tx pgx.Tx) (string, error) {
		drv, err := s.fleet.CreateDriver(r.Context(), tx, actor.TenantID, fleet.DriverInput{
			CustomerID: req.CustomerID, Name: req.Name, Phone: req.Phone, LicenseNumber: req.LicenseNumber,
			PIN: req.PIN, AllowedProductIDs: req.AllowedProductIDs, AssignmentRule: req.AssignmentRule,
		})
		if err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown customer")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		d = drv
		return drv.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, driverMap(d))
}

func (s *Server) handleSetDriverStatus(w http.ResponseWriter, r *http.Request) {
	s.fleetStatusTransition(w, r, "customer_driver", map[string]bool{"active": true, "on_hold": true, "inactive": true},
		func(tx pgx.Tx, actor identity.Actor, id uuid.UUID, status string) (map[string]any, error) {
			d, err := s.fleet.SetDriverStatus(r.Context(), tx, actor.TenantID, id, status)
			if err != nil {
				return nil, err
			}
			return driverMap(d), nil
		})
}

func (s *Server) handleResetDriverPIN(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid driver id")
		return
	}
	var req struct {
		PIN string `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer_driver.pin_reset", EventType: "CustomerDriverPinReset", EntityType: "customer_driver",
		EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		if err := s.fleet.ResetDriverPIN(r.Context(), tx, actor.TenantID, id, req.PIN); errors.Is(err, fleet.ErrNotFound) {
			writeError(w, http.StatusNotFound, "driver not found")
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
	writeJSON(w, http.StatusOK, map[string]any{"driver_id": id, "pin_set": req.PIN != ""})
}

// ---- Credentials (Stage 6) ----

func credentialMap(c *fleet.Credential) map[string]any {
	return map[string]any{
		"id": c.ID, "customer_id": c.CustomerID, "vehicle_id": c.VehicleID, "driver_id": c.DriverID,
		"credential_type": c.CredentialType, "masked_label": c.MaskedLabel, "status": c.Status,
		"issued_at": c.IssuedAt.Format(dateLayout), "expiry_date": fmtDate(c.ExpiryDate),
	}
}

func (s *Server) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.fleet.ListCredentialsPage(r.Context(), actor.TenantID, queryUUID(r, "customer_id"), limit+1, offset)
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
		out = append(out, credentialMap(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

func (s *Server) handleIssueCredential(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		CustomerID     uuid.UUID  `json:"customer_id"`
		VehicleID      *uuid.UUID `json:"vehicle_id,omitempty"`
		DriverID       *uuid.UUID `json:"driver_id,omitempty"`
		CredentialType string     `json:"credential_type"`
		Token          string     `json:"token"`
		ExpiryDate     string     `json:"expiry_date,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CustomerID == uuid.Nil || req.Token == "" {
		writeError(w, http.StatusBadRequest, "customer_id and token are required")
		return
	}
	var c *fleet.Credential
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "fuel_credential.issued", EventType: "FuelCredentialIssued", EntityType: "fuel_credential",
	}, func(tx pgx.Tx) (string, error) {
		cred, err := s.fleet.IssueCredential(r.Context(), tx, actor.TenantID, fleet.CredentialInput{
			CustomerID: req.CustomerID, VehicleID: req.VehicleID, DriverID: req.DriverID,
			CredentialType: req.CredentialType, RawToken: req.Token, ExpiryDate: parseOptDate(req.ExpiryDate),
		})
		if errors.Is(err, fleet.ErrConflict) {
			writeError(w, http.StatusConflict, "a credential with this token already exists")
			return "", err
		}
		if err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown customer, vehicle, or driver")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		c = cred
		return cred.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, credentialMap(c))
}

func (s *Server) handleSetCredentialStatus(w http.ResponseWriter, r *http.Request) {
	s.fleetStatusTransition(w, r, "fuel_credential",
		map[string]bool{"issued": true, "active": true, "suspended": true, "expired": true, "revoked": true},
		func(tx pgx.Tx, actor identity.Actor, id uuid.UUID, status string) (map[string]any, error) {
			c, err := s.fleet.SetCredentialStatus(r.Context(), tx, actor.TenantID, id, status)
			if err != nil {
				return nil, err
			}
			return credentialMap(c), nil
		})
}

// handleValidateCredential resolves a raw token to its customer/vehicle/driver
// context plus usability — the forecourt entry point for fleet authorization.
func (s *Server) handleValidateCredential(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	vc, err := s.fleet.ValidateCredential(ctx, tx, actor.TenantID, req.Token)
	if errors.Is(err, fleet.ErrNotFound) {
		writeError(w, http.StatusNotFound, "credential not recognized")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	resp := credentialMap(&vc.Credential)
	resp["customer_name"] = vc.CustomerName
	resp["expired"] = vc.Expired
	resp["usable"] = vc.Usable
	writeJSON(w, http.StatusOK, resp)
}

// ---- shared helpers ----

// fleetStatusTransition validates the status and applies a repo transition
// inside an audited tx.
func (s *Server) fleetStatusTransition(w http.ResponseWriter, r *http.Request, entity string, allowed map[string]bool, apply func(tx pgx.Tx, actor identity.Actor, id uuid.UUID, status string) (map[string]any, error)) {
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
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !allowed[req.Status] {
		writeError(w, http.StatusBadRequest, "invalid status for this entity")
		return
	}
	var result map[string]any
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: entity + ".status_changed", EventType: "FleetStatusChanged", EntityType: entity,
		EntityID: id.String(), NewValue: map[string]any{"status": req.Status},
	}, func(tx pgx.Tx) (string, error) {
		res, err := apply(tx, actor, id, req.Status)
		if errors.Is(err, fleet.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return "", err
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		result = res
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// queryUUID parses a uuid query param, returning uuid.Nil when absent/invalid.
func queryUUID(r *http.Request, key string) uuid.UUID {
	if v := r.URL.Query().Get(key); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			return id
		}
	}
	return uuid.Nil
}
