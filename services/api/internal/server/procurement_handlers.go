package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/events"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/inventory"
	"github.com/japharyroman/fuelgrid-os/internal/procurement"
)

var decimalPattern = regexp.MustCompile(`^\d+(\.\d{1,4})?$`)

type supplierDTO struct {
	ID               uuid.UUID   `json:"id"`
	TenantID         uuid.UUID   `json:"tenant_id"`
	Code             string      `json:"code"`
	Name             string      `json:"name"`
	ContactName      *string     `json:"contact_name,omitempty"`
	ContactEmail     *string     `json:"contact_email,omitempty"`
	ContactPhone     *string     `json:"contact_phone,omitempty"`
	PaymentTermsDays int         `json:"payment_terms_days"`
	Status           string      `json:"status"`
	DeactivatedAt    *string     `json:"deactivated_at,omitempty"`
	ProductIDs       []uuid.UUID `json:"product_ids"`
}

func toSupplierDTO(sup *procurement.Supplier) supplierDTO {
	return supplierDTO{
		ID: sup.ID, TenantID: sup.TenantID, Code: sup.Code, Name: sup.Name,
		ContactName: sup.ContactName, ContactEmail: sup.ContactEmail, ContactPhone: sup.ContactPhone,
		PaymentTermsDays: sup.PaymentTermsDays, Status: sup.Status,
		DeactivatedAt: fmtTime(sup.DeactivatedAt), ProductIDs: sup.ProductIDs,
	}
}

type purchaseOrderLineDTO struct {
	ID              uuid.UUID `json:"id"`
	TenantID        uuid.UUID `json:"tenant_id"`
	PurchaseOrderID uuid.UUID `json:"purchase_order_id"`
	ProductID       uuid.UUID `json:"product_id"`
	OrderedLitres   float64   `json:"ordered_litres"`
	UnitPrice       string    `json:"unit_price"`
	ReceivedLitres  float64   `json:"received_litres"`
}

type purchaseOrderDTO struct {
	ID                   uuid.UUID              `json:"id"`
	TenantID             uuid.UUID              `json:"tenant_id"`
	StationID            uuid.UUID              `json:"station_id"`
	SupplierID           uuid.UUID              `json:"supplier_id"`
	ExpectedDeliveryDate *string                `json:"expected_delivery_date,omitempty"`
	Status               string                 `json:"status"`
	RaisedBy             uuid.UUID              `json:"raised_by"`
	SubmittedBy          *uuid.UUID             `json:"submitted_by,omitempty"`
	SubmittedAt          *string                `json:"submitted_at,omitempty"`
	ConfirmedBy          *uuid.UUID             `json:"confirmed_by,omitempty"`
	ConfirmedAt          *string                `json:"confirmed_at,omitempty"`
	CancelledBy          *uuid.UUID             `json:"cancelled_by,omitempty"`
	CancelledAt          *string                `json:"cancelled_at,omitempty"`
	ClosedBy             *uuid.UUID             `json:"closed_by,omitempty"`
	ClosedAt             *string                `json:"closed_at,omitempty"`
	Notes                *string                `json:"notes,omitempty"`
	CreatedAt            string                 `json:"created_at"`
	Lines                []purchaseOrderLineDTO `json:"lines"`
}

func toPurchaseOrderDTO(po *procurement.PurchaseOrder) purchaseOrderDTO {
	lines := make([]purchaseOrderLineDTO, 0, len(po.Lines))
	for i := range po.Lines {
		ln := po.Lines[i]
		lines = append(lines, purchaseOrderLineDTO{
			ID: ln.ID, TenantID: ln.TenantID, PurchaseOrderID: ln.PurchaseOrderID,
			ProductID: ln.ProductID, OrderedLitres: ln.OrderedLitres,
			UnitPrice: ln.UnitPrice, ReceivedLitres: ln.ReceivedLitres,
		})
	}
	return purchaseOrderDTO{
		ID: po.ID, TenantID: po.TenantID, StationID: po.StationID, SupplierID: po.SupplierID,
		ExpectedDeliveryDate: fmtDate(po.ExpectedDeliveryDate), Status: po.Status,
		RaisedBy: po.RaisedBy, SubmittedBy: po.SubmittedBy, SubmittedAt: fmtTime(po.SubmittedAt),
		ConfirmedBy: po.ConfirmedBy, ConfirmedAt: fmtTime(po.ConfirmedAt),
		CancelledBy: po.CancelledBy, CancelledAt: fmtTime(po.CancelledAt),
		ClosedBy: po.ClosedBy, ClosedAt: fmtTime(po.ClosedAt), Notes: po.Notes,
		CreatedAt: po.CreatedAt.Format(time.RFC3339), Lines: lines,
	}
}

type supplierInvoiceLineDTO struct {
	ID                uuid.UUID  `json:"id"`
	TenantID          uuid.UUID  `json:"tenant_id"`
	SupplierInvoiceID uuid.UUID  `json:"supplier_invoice_id"`
	PurchaseOrderID   uuid.UUID  `json:"purchase_order_id"`
	POLineID          uuid.UUID  `json:"po_line_id"`
	DeliveryID        *uuid.UUID `json:"delivery_id,omitempty"`
	ProductID         uuid.UUID  `json:"product_id"`
	InvoicedLitres    float64    `json:"invoiced_litres"`
	UnitPrice         string     `json:"unit_price"`
	Amount            string     `json:"amount"`
}

type procurementDiscrepancyDTO struct {
	ID                uuid.UUID  `json:"id"`
	TenantID          uuid.UUID  `json:"tenant_id"`
	SupplierInvoiceID uuid.UUID  `json:"supplier_invoice_id"`
	PurchaseOrderID   uuid.UUID  `json:"purchase_order_id"`
	DeliveryID        *uuid.UUID `json:"delivery_id,omitempty"`
	POLineID          *uuid.UUID `json:"po_line_id,omitempty"`
	Type              string     `json:"type"`
	Severity          string     `json:"severity"`
	Detail            string     `json:"detail"`
	VarianceLitres    *float64   `json:"variance_litres,omitempty"`
	VarianceAmount    *string    `json:"variance_amount,omitempty"`
	Status            string     `json:"status"`
	RaisedAt          string     `json:"raised_at"`
	ResolvedBy        *uuid.UUID `json:"resolved_by,omitempty"`
	ResolvedAt        *string    `json:"resolved_at,omitempty"`
}

type supplierInvoiceDTO struct {
	ID              uuid.UUID                   `json:"id"`
	TenantID        uuid.UUID                   `json:"tenant_id"`
	SupplierID      uuid.UUID                   `json:"supplier_id"`
	PurchaseOrderID uuid.UUID                   `json:"purchase_order_id"`
	StationID       uuid.UUID                   `json:"station_id"`
	InvoiceNumber   string                      `json:"invoice_number"`
	Status          string                      `json:"status"`
	ReceivedAt      string                      `json:"received_at"`
	DueDate         *string                     `json:"due_date,omitempty"`
	TotalAmount     string                      `json:"total_amount"`
	RecordedBy      uuid.UUID                   `json:"recorded_by"`
	ApprovedBy      *uuid.UUID                  `json:"approved_by,omitempty"`
	ApprovedAt      *string                     `json:"approved_at,omitempty"`
	Notes           *string                     `json:"notes,omitempty"`
	Lines           []supplierInvoiceLineDTO    `json:"lines"`
	Discrepancies   []procurementDiscrepancyDTO `json:"discrepancies"`
}

func toSupplierInvoiceDTO(inv *procurement.SupplierInvoice) supplierInvoiceDTO {
	lines := make([]supplierInvoiceLineDTO, 0, len(inv.Lines))
	for i := range inv.Lines {
		ln := inv.Lines[i]
		lines = append(lines, supplierInvoiceLineDTO{
			ID: ln.ID, TenantID: ln.TenantID, SupplierInvoiceID: ln.SupplierInvoiceID,
			PurchaseOrderID: ln.PurchaseOrderID, POLineID: ln.POLineID, DeliveryID: ln.DeliveryID,
			ProductID: ln.ProductID, InvoicedLitres: ln.InvoicedLitres, UnitPrice: ln.UnitPrice,
			Amount: ln.Amount,
		})
	}
	discs := make([]procurementDiscrepancyDTO, 0, len(inv.Discrepancies))
	for i := range inv.Discrepancies {
		discs = append(discs, toProcurementDiscrepancyDTO(&inv.Discrepancies[i]))
	}
	return supplierInvoiceDTO{
		ID: inv.ID, TenantID: inv.TenantID, SupplierID: inv.SupplierID,
		PurchaseOrderID: inv.PurchaseOrderID, StationID: inv.StationID,
		InvoiceNumber: inv.InvoiceNumber, Status: inv.Status,
		ReceivedAt: inv.ReceivedAt.Format(time.RFC3339), DueDate: fmtDate(inv.DueDate),
		TotalAmount: inv.TotalAmount, RecordedBy: inv.RecordedBy,
		ApprovedBy: inv.ApprovedBy, ApprovedAt: fmtTime(inv.ApprovedAt),
		Notes: inv.Notes, Lines: lines, Discrepancies: discs,
	}
}

func toProcurementDiscrepancyDTO(d *procurement.ProcurementDiscrepancy) procurementDiscrepancyDTO {
	return procurementDiscrepancyDTO{
		ID: d.ID, TenantID: d.TenantID, SupplierInvoiceID: d.SupplierInvoiceID,
		PurchaseOrderID: d.PurchaseOrderID, DeliveryID: d.DeliveryID, POLineID: d.POLineID,
		Type: d.Type, Severity: d.Severity, Detail: d.Detail,
		VarianceLitres: d.VarianceLitres, VarianceAmount: d.VarianceAmount,
		Status: d.Status, RaisedAt: d.RaisedAt.Format(time.RFC3339),
		ResolvedBy: d.ResolvedBy, ResolvedAt: fmtTime(d.ResolvedAt),
	}
}

func (s *Server) handleListSuppliers(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.procurement.ListSuppliers(r.Context(), actor.TenantID)
	if err != nil {
		s.logger.Error("list suppliers", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]supplierDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toSupplierDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleGetSupplier(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid supplier id")
		return
	}
	sup, err := s.procurement.GetSupplier(r.Context(), actor.TenantID, id)
	if errors.Is(err, procurement.ErrNotFound) {
		writeError(w, http.StatusNotFound, "supplier not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toSupplierDTO(sup))
}

type supplierRequest struct {
	Code             string      `json:"code"`
	Name             string      `json:"name"`
	ContactName      *string     `json:"contact_name,omitempty"`
	ContactEmail     *string     `json:"contact_email,omitempty"`
	ContactPhone     *string     `json:"contact_phone,omitempty"`
	PaymentTermsDays int         `json:"payment_terms_days,omitempty"`
	ProductIDs       []uuid.UUID `json:"product_ids,omitempty"`
}

func (s *Server) handleCreateSupplier(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req supplierRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	req.Name = strings.TrimSpace(req.Name)
	if req.Code == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "code and name are required")
		return
	}
	if req.PaymentTermsDays < 0 {
		writeError(w, http.StatusBadRequest, "payment_terms_days must be non-negative")
		return
	}
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	sup, err := s.procurement.CreateSupplier(ctx, tx, actor.TenantID, procurement.SupplierInput{
		Code: req.Code, Name: req.Name, ContactName: req.ContactName,
		ContactEmail: req.ContactEmail, ContactPhone: req.ContactPhone,
		PaymentTermsDays: req.PaymentTermsDays, ProductIDs: req.ProductIDs,
	})
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "a supplier with that code already exists")
		return
	}
	if isForeignKeyViolation(err) {
		writeError(w, http.StatusNotFound, "one or more products were not found")
		return
	}
	if err != nil {
		s.logger.Error("create supplier", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "supplier.created", EventType: "SupplierCreated",
		EntityType: "supplier", EntityID: sup.ID.String(),
		NewValue: toSupplierDTO(sup),
		IP:       clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toSupplierDTO(sup))
}

type supplierUpdateRequest struct {
	Code             *string      `json:"code,omitempty"`
	Name             *string      `json:"name,omitempty"`
	ContactName      *string      `json:"contact_name,omitempty"`
	ContactEmail     *string      `json:"contact_email,omitempty"`
	ContactPhone     *string      `json:"contact_phone,omitempty"`
	PaymentTermsDays *int         `json:"payment_terms_days,omitempty"`
	Status           *string      `json:"status,omitempty"`
	ProductIDs       *[]uuid.UUID `json:"product_ids,omitempty"`
}

func (s *Server) handleUpdateSupplier(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid supplier id")
		return
	}
	var req supplierUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.PaymentTermsDays != nil && *req.PaymentTermsDays < 0 {
		writeError(w, http.StatusBadRequest, "payment_terms_days must be non-negative")
		return
	}
	if req.Status != nil && *req.Status != "active" && *req.Status != "inactive" && *req.Status != "deactivated" {
		writeError(w, http.StatusBadRequest, "invalid supplier status")
		return
	}
	ctx := r.Context()
	before, err := s.procurement.GetSupplier(ctx, actor.TenantID, id)
	if errors.Is(err, procurement.ErrNotFound) {
		writeError(w, http.StatusNotFound, "supplier not found")
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
	productIDs := []uuid.UUID(nil)
	productIDsSet := false
	if req.ProductIDs != nil {
		productIDs = *req.ProductIDs
		productIDsSet = true
	}
	after, err := s.procurement.UpdateSupplier(ctx, tx, actor.TenantID, id, procurement.SupplierUpdateInput{
		Code: req.Code, Name: req.Name, ContactName: req.ContactName,
		ContactEmail: req.ContactEmail, ContactPhone: req.ContactPhone,
		PaymentTermsDays: req.PaymentTermsDays, Status: req.Status,
		ProductIDs: productIDs, ProductIDsSet: productIDsSet,
	})
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "a supplier with that code already exists")
		return
	}
	if isForeignKeyViolation(err) {
		writeError(w, http.StatusNotFound, "one or more products were not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "supplier.updated", EventType: "SupplierUpdated",
		EntityType: "supplier", EntityID: id.String(),
		PreviousValue: toSupplierDTO(before), NewValue: toSupplierDTO(after),
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toSupplierDTO(after))
}

func (s *Server) handleDeactivateSupplier(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid supplier id")
		return
	}
	ctx := r.Context()
	before, err := s.procurement.GetSupplier(ctx, actor.TenantID, id)
	if errors.Is(err, procurement.ErrNotFound) {
		writeError(w, http.StatusNotFound, "supplier not found")
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
	after, err := s.procurement.DeactivateSupplier(ctx, tx, actor.TenantID, id)
	if errors.Is(err, procurement.ErrSupplierInUse) {
		writeError(w, http.StatusConflict, "supplier is referenced by an open purchase order")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "supplier.deactivated", EventType: "SupplierDeactivated",
		EntityType: "supplier", EntityID: id.String(),
		PreviousValue: toSupplierDTO(before), NewValue: toSupplierDTO(after),
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toSupplierDTO(after))
}

type poLineRequest struct {
	ProductID     uuid.UUID `json:"product_id"`
	OrderedLitres float64   `json:"ordered_litres"`
	UnitPrice     string    `json:"unit_price"`
}

func poLineInputs(lines []poLineRequest) ([]procurement.PurchaseOrderLineInput, bool) {
	out := make([]procurement.PurchaseOrderLineInput, 0, len(lines))
	for _, ln := range lines {
		if ln.ProductID == uuid.Nil || ln.OrderedLitres <= 0 || !validDecimal(ln.UnitPrice) {
			return nil, false
		}
		out = append(out, procurement.PurchaseOrderLineInput{
			ProductID: ln.ProductID, OrderedLitres: ln.OrderedLitres, UnitPrice: strings.TrimSpace(ln.UnitPrice),
		})
	}
	return out, true
}

type createPORequest struct {
	StationID            uuid.UUID       `json:"station_id"`
	SupplierID           uuid.UUID       `json:"supplier_id"`
	ExpectedDeliveryDate *string         `json:"expected_delivery_date,omitempty"`
	Notes                *string         `json:"notes,omitempty"`
	Lines                []poLineRequest `json:"lines"`
}

func (s *Server) handleCreatePurchaseOrder(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req createPORequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.StationID == uuid.Nil || req.SupplierID == uuid.Nil || len(req.Lines) == 0 {
		writeError(w, http.StatusBadRequest, "station_id, supplier_id, and at least one line are required")
		return
	}
	lines, ok := poLineInputs(req.Lines)
	if !ok {
		writeError(w, http.StatusBadRequest, "lines must have product_id, positive ordered_litres, and decimal unit_price")
		return
	}
	expected, err := parseDate(req.ExpectedDeliveryDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid expected_delivery_date (want YYYY-MM-DD)")
		return
	}
	ctx := r.Context()
	if _, err := s.stations.Get(ctx, actor.TenantID, req.StationID); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "station not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.authorizeStation(w, r, actor, "purchase_order.manage", req.StationID) {
		return
	}
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	po, err := s.procurement.CreatePurchaseOrder(ctx, tx, actor.TenantID, procurement.PurchaseOrderInput{
		StationID: req.StationID, SupplierID: req.SupplierID,
		ExpectedDeliveryDate: expected, Notes: req.Notes, Lines: lines, RaisedBy: actor.UserID,
	})
	if errors.Is(err, procurement.ErrSupplierUnavailable) {
		writeError(w, http.StatusConflict, "supplier is not active")
		return
	}
	if isForeignKeyViolation(err) {
		writeError(w, http.StatusNotFound, "supplier, station, or product not found")
		return
	}
	if err != nil {
		s.logger.Error("create purchase order", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "purchase_order.created", EventType: "PurchaseOrderCreated",
		EntityType: "purchase_order", EntityID: po.ID.String(),
		NewValue: toPurchaseOrderDTO(po),
		IP:       clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toPurchaseOrderDTO(po))
}

func (s *Server) handleListPurchaseOrders(w http.ResponseWriter, r *http.Request) {
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
		id, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid supplier_id")
			return
		}
		supplierID = &id
	}
	var status *string
	if raw := r.URL.Query().Get("status"); raw != "" {
		status = &raw
	}
	rows, err := s.procurement.ListPurchaseOrders(r.Context(), actor.TenantID, procurement.PurchaseOrderFilter{
		StationIDs: filter, SupplierID: supplierID, Status: status,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]purchaseOrderDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toPurchaseOrderDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleGetPurchaseOrder(w http.ResponseWriter, r *http.Request) {
	actor, po, ok := s.purchaseOrderForStationPermission(w, r, "purchase_order.read")
	if !ok {
		return
	}
	_ = actor
	writeJSON(w, http.StatusOK, toPurchaseOrderDTO(po))
}

type updatePORequest struct {
	ExpectedDeliveryDate *string          `json:"expected_delivery_date,omitempty"`
	Notes                *string          `json:"notes,omitempty"`
	Lines                *[]poLineRequest `json:"lines,omitempty"`
}

func (s *Server) handleUpdatePurchaseOrder(w http.ResponseWriter, r *http.Request) {
	actor, before, ok := s.purchaseOrderForStationPermission(w, r, "purchase_order.manage")
	if !ok {
		return
	}
	var req updatePORequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	expected, err := parseDate(req.ExpectedDeliveryDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid expected_delivery_date (want YYYY-MM-DD)")
		return
	}
	var lines []procurement.PurchaseOrderLineInput
	linesSet := false
	if req.Lines != nil {
		if len(*req.Lines) == 0 {
			writeError(w, http.StatusBadRequest, "at least one line is required")
			return
		}
		var ok bool
		lines, ok = poLineInputs(*req.Lines)
		if !ok {
			writeError(w, http.StatusBadRequest, "lines must have product_id, positive ordered_litres, and decimal unit_price")
			return
		}
		linesSet = true
	}
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	after, err := s.procurement.UpdatePurchaseOrderDraft(ctx, tx, actor.TenantID, before.ID, procurement.PurchaseOrderUpdateInput{
		ExpectedDeliveryDate: expected, ExpectedDateSet: req.ExpectedDeliveryDate != nil,
		Notes: req.Notes, NotesSet: req.Notes != nil, Lines: lines, LinesSet: linesSet,
	})
	if errors.Is(err, procurement.ErrPurchaseOrderNotDraft) {
		writeError(w, http.StatusConflict, "purchase order lines can only be edited while draft")
		return
	}
	if isForeignKeyViolation(err) {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "purchase_order.updated", EventType: "PurchaseOrderUpdated",
		EntityType: "purchase_order", EntityID: after.ID.String(),
		PreviousValue: toPurchaseOrderDTO(before), NewValue: toPurchaseOrderDTO(after),
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toPurchaseOrderDTO(after))
}

type transitionPORequest struct {
	Status string `json:"status"`
}

func (s *Server) handleTransitionPurchaseOrder(w http.ResponseWriter, r *http.Request) {
	actor, before, ok := s.purchaseOrderForStationPermission(w, r, "purchase_order.approve")
	if !ok {
		return
	}
	var req transitionPORequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !procurement.ValidPurchaseOrderTransition(before.Status, req.Status) {
		writeError(w, http.StatusConflict, "invalid purchase order status transition")
		return
	}
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	after, err := s.procurement.TransitionPurchaseOrder(ctx, tx, actor.TenantID, before.ID, actor.UserID, before.Status, req.Status)
	if errors.Is(err, procurement.ErrInvalidTransition) {
		writeError(w, http.StatusConflict, "invalid purchase order status transition")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	action, eventType := poTransitionEvent(req.Status)
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: action, EventType: eventType,
		EntityType: "purchase_order", EntityID: after.ID.String(),
		PreviousValue: toPurchaseOrderDTO(before), NewValue: toPurchaseOrderDTO(after),
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toPurchaseOrderDTO(after))
}

type receivePOReceiptRequest struct {
	TankID          uuid.UUID `json:"tank_id"`
	POLineID        uuid.UUID `json:"po_line_id"`
	VolumeLitres    float64   `json:"volume_litres"`
	DipBeforeLitres *float64  `json:"dip_before_litres,omitempty"`
	DipAfterLitres  *float64  `json:"dip_after_litres,omitempty"`
	LineUnitPrice   *string   `json:"line_unit_price,omitempty"`
	FreightAmount   string    `json:"freight_amount,omitempty"`
	DutyAmount      string    `json:"duty_amount,omitempty"`
	LeviesAmount    string    `json:"levies_amount,omitempty"`
	Notes           *string   `json:"notes,omitempty"`
}

func (s *Server) handleReceivePurchaseOrderReceipt(w http.ResponseWriter, r *http.Request) {
	actor, po, ok := s.purchaseOrderForStationPermission(w, r, "delivery.receive")
	if !ok {
		return
	}
	var req receivePOReceiptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.TankID == uuid.Nil || req.POLineID == uuid.Nil || req.VolumeLitres <= 0 {
		writeError(w, http.StatusBadRequest, "tank_id, po_line_id, and positive volume_litres are required")
		return
	}
	if (req.DipBeforeLitres != nil && *req.DipBeforeLitres < 0) || (req.DipAfterLitres != nil && *req.DipAfterLitres < 0) {
		writeError(w, http.StatusBadRequest, "dip volumes must be non-negative")
		return
	}
	if !validOptionalDecimal(req.LineUnitPrice) || !validDecimal(defaultDecimal(req.FreightAmount)) ||
		!validDecimal(defaultDecimal(req.DutyAmount)) || !validDecimal(defaultDecimal(req.LeviesAmount)) {
		writeError(w, http.StatusBadRequest, "cost fields must be non-negative decimal strings")
		return
	}
	ctx := r.Context()
	tank, err := s.tanks.Get(ctx, actor.TenantID, req.TankID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "tank not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var dipVariance *float64
	var dipMismatch bool
	if req.DipBeforeLitres != nil && req.DipAfterLitres != nil {
		variance := req.VolumeLitres - (*req.DipAfterLitres - *req.DipBeforeLitres)
		dipVariance = &variance
		prod, err := s.products.Get(ctx, actor.TenantID, tank.ProductID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		dipMismatch = abs(variance) > req.VolumeLitres*prod.LossTolerancePercent/100
	}
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	res, err := s.inventory.ReceiveGoodsReceipt(ctx, tx, actor.TenantID, inventory.GoodsReceiptInput{
		TankID: req.TankID, PurchaseOrderID: po.ID, POLineID: req.POLineID,
		VolumeLitres: req.VolumeLitres, DipBeforeLitres: req.DipBeforeLitres,
		DipAfterLitres: req.DipAfterLitres, DipVarianceLitres: dipVariance,
		LineUnitPrice: req.LineUnitPrice, FreightAmount: defaultDecimal(req.FreightAmount),
		DutyAmount: defaultDecimal(req.DutyAmount), LeviesAmount: defaultDecimal(req.LeviesAmount),
		ReceivedBy: actor.UserID, Notes: req.Notes,
	})
	switch {
	case errors.Is(err, inventory.ErrNoOpeningBalance):
		writeError(w, http.StatusConflict, "tank has no opening balance; set one before receiving deliveries")
		return
	case errors.Is(err, inventory.ErrPurchaseOrderNotReceivable):
		writeError(w, http.StatusConflict, "purchase order is not confirmed or partially received")
		return
	case errors.Is(err, inventory.ErrPOLineNotFound):
		writeError(w, http.StatusNotFound, "purchase order line not found")
		return
	case errors.Is(err, inventory.ErrReceiptTankMismatch):
		writeError(w, http.StatusBadRequest, "tank does not match the purchase order station/product")
		return
	case err != nil:
		s.logger.Error("receive PO receipt", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "goods_receipt.recorded", EventType: "GoodsReceiptRecorded",
		EntityType: "delivery", EntityID: res.Delivery.ID.String(),
		NewValue: toDeliveryDTO(res.Delivery),
		IP:       clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "goods_receipt.priced", EventType: "GoodsReceiptPriced",
		EntityType: "delivery", EntityID: res.Delivery.ID.String(),
		NewValue: toDeliveryDTO(res.Delivery),
		IP:       clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if res.PurchaseOrderStatus == procurement.POStatusPartiallyReceived || res.PurchaseOrderStatus == procurement.POStatusReceived {
		if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
			TenantID: actor.TenantID, ActorID: actor.UserID,
			Action: "purchase_order.received", EventType: "PurchaseOrderReceived",
			EntityType: "purchase_order", EntityID: po.ID.String(),
			NewValue: map[string]any{"id": po.ID, "status": res.PurchaseOrderStatus},
			IP:       clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"delivery":                 toDeliveryDTO(res.Delivery),
		"movement":                 toStockMovementDTO(res.Movement),
		"dip_mismatch":             dipMismatch,
		"quantity_discrepancy":     res.QuantityDiscrepancy,
		"quantity_variance_litres": res.QuantityVarianceLitres,
		"purchase_order_status":    res.PurchaseOrderStatus,
	})
}

type recordInvoiceRequest struct {
	PurchaseOrderID uuid.UUID                  `json:"purchase_order_id"`
	InvoiceNumber   string                     `json:"invoice_number"`
	ReceivedAt      *string                    `json:"received_at,omitempty"`
	DueDate         *string                    `json:"due_date,omitempty"`
	Notes           *string                    `json:"notes,omitempty"`
	Lines           []recordInvoiceLineRequest `json:"lines"`
}

type recordInvoiceLineRequest struct {
	POLineID       uuid.UUID  `json:"po_line_id"`
	DeliveryID     *uuid.UUID `json:"delivery_id,omitempty"`
	InvoicedLitres float64    `json:"invoiced_litres"`
	UnitPrice      string     `json:"unit_price"`
	Amount         *string    `json:"amount,omitempty"`
}

func (s *Server) handleRecordSupplierInvoice(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req recordInvoiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.InvoiceNumber = strings.TrimSpace(req.InvoiceNumber)
	if req.PurchaseOrderID == uuid.Nil || req.InvoiceNumber == "" || len(req.Lines) == 0 {
		writeError(w, http.StatusBadRequest, "purchase_order_id, invoice_number, and lines are required")
		return
	}
	var receivedAt *time.Time
	if req.ReceivedAt != nil && *req.ReceivedAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ReceivedAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "received_at must be RFC3339")
			return
		}
		receivedAt = &t
	}
	dueDate, err := parseDate(req.DueDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid due_date (want YYYY-MM-DD)")
		return
	}
	ctx := r.Context()
	po, err := s.procurement.GetPurchaseOrder(ctx, actor.TenantID, req.PurchaseOrderID)
	if errors.Is(err, procurement.ErrNotFound) {
		writeError(w, http.StatusNotFound, "purchase order not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.authorizeStation(w, r, actor, "invoice.manage", po.StationID) {
		return
	}
	lines := make([]procurement.SupplierInvoiceLineInput, 0, len(req.Lines))
	for _, ln := range req.Lines {
		if ln.POLineID == uuid.Nil || ln.InvoicedLitres <= 0 || !validDecimal(ln.UnitPrice) || !validOptionalDecimal(ln.Amount) {
			writeError(w, http.StatusBadRequest, "invoice lines require po_line_id, positive invoiced_litres, and decimal unit_price/amount")
			return
		}
		lines = append(lines, procurement.SupplierInvoiceLineInput{
			POLineID: ln.POLineID, DeliveryID: ln.DeliveryID, InvoicedLitres: ln.InvoicedLitres,
			UnitPrice: strings.TrimSpace(ln.UnitPrice), Amount: trimOptionalDecimal(ln.Amount),
		})
	}
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	inv, err := s.procurement.RecordSupplierInvoice(ctx, tx, actor.TenantID, procurement.SupplierInvoiceInput{
		PurchaseOrderID: req.PurchaseOrderID, InvoiceNumber: req.InvoiceNumber,
		ReceivedAt: receivedAt, DueDate: dueDate, Notes: req.Notes,
		RecordedBy: actor.UserID, Lines: lines,
	})
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "supplier invoice number already exists")
		return
	}
	if errors.Is(err, procurement.ErrInvalidTransition) {
		writeError(w, http.StatusConflict, "invoice can only be recorded against a submitted or later purchase order")
		return
	}
	if errors.Is(err, procurement.ErrNotFound) || isForeignKeyViolation(err) {
		writeError(w, http.StatusNotFound, "purchase order line or delivery not found")
		return
	}
	if err != nil {
		s.logger.Error("record supplier invoice", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "supplier_invoice.recorded", EventType: "SupplierInvoiceRecorded",
		EntityType: "supplier_invoice", EntityID: inv.ID.String(),
		NewValue: toSupplierInvoiceDTO(inv),
		IP:       clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	for i := range inv.Discrepancies {
		if inv.Discrepancies[i].Status != "open" {
			continue
		}
		dto := toProcurementDiscrepancyDTO(&inv.Discrepancies[i])
		if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
			TenantID: actor.TenantID, ActorID: actor.UserID,
			Action: "procurement_discrepancy.raised", EventType: "ProcurementDiscrepancyRaised",
			EntityType: "procurement_discrepancy", EntityID: inv.Discrepancies[i].ID.String(),
			NewValue: dto,
			IP:       clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toSupplierInvoiceDTO(inv))
}

func (s *Server) handleGetSupplierInvoice(w http.ResponseWriter, r *http.Request) {
	_, inv, ok := s.invoiceForStationPermission(w, r, "invoice.manage")
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toSupplierInvoiceDTO(inv))
}

func (s *Server) handleApproveSupplierInvoice(w http.ResponseWriter, r *http.Request) {
	actor, before, ok := s.invoiceForStationPermission(w, r, "invoice.approve")
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	after, err := s.procurement.ApproveSupplierInvoice(ctx, tx, actor.TenantID, before.ID, actor.UserID)
	if errors.Is(err, procurement.ErrInvoiceHasDiscrepancy) {
		writeError(w, http.StatusConflict, "resolve procurement discrepancies before approving")
		return
	}
	if errors.Is(err, procurement.ErrInvoiceNotMatched) {
		writeError(w, http.StatusConflict, "invoice is not matched")
		return
	}
	if errors.Is(err, procurement.ErrSelfApproval) {
		writeError(w, http.StatusForbidden, "separation of duties: you cannot approve an invoice you recorded")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "supplier_invoice.approved", EventType: "SupplierInvoiceApproved",
		EntityType: "supplier_invoice", EntityID: after.ID.String(),
		PreviousValue: toSupplierInvoiceDTO(before), NewValue: toSupplierInvoiceDTO(after),
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	payablePayload, _ := json.Marshal(map[string]any{
		"supplier_id":         after.SupplierID,
		"station_id":          after.StationID,
		"supplier_invoice_id": after.ID,
		"amount":              after.TotalAmount,
		"due_date":            fmtDate(after.DueDate),
	})
	if err := events.WriteOutbox(ctx, tx, events.Event{
		TenantID: &actor.TenantID, Type: "PayableCreated",
		AggregateType: "supplier_invoice", AggregateID: after.ID.String(),
		ActorID: &actor.UserID, Payload: payablePayload,
		CorrelationID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toSupplierInvoiceDTO(after))
}

type resolveProcurementDiscrepancyRequest struct {
	Status string `json:"status"`
}

func (s *Server) handleResolveProcurementDiscrepancy(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid discrepancy id")
		return
	}
	var req resolveProcurementDiscrepancyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Status != "resolved" {
		writeError(w, http.StatusBadRequest, "status must be resolved")
		return
	}
	ctx := r.Context()
	before, err := s.procurement.GetDiscrepancy(ctx, actor.TenantID, id)
	if errors.Is(err, procurement.ErrNotFound) {
		writeError(w, http.StatusNotFound, "discrepancy not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	inv, err := s.procurement.GetSupplierInvoice(ctx, actor.TenantID, before.SupplierInvoiceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.authorizeStation(w, r, actor, "invoice.approve", inv.StationID) {
		return
	}
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	after, err := s.procurement.ResolveDiscrepancy(ctx, tx, actor.TenantID, id, actor.UserID)
	if errors.Is(err, procurement.ErrAlreadyResolved) {
		writeError(w, http.StatusConflict, "discrepancy is already resolved")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "procurement_discrepancy.resolved", EventType: "ProcurementDiscrepancyResolved",
		EntityType: "procurement_discrepancy", EntityID: after.ID.String(),
		PreviousValue: toProcurementDiscrepancyDTO(before), NewValue: toProcurementDiscrepancyDTO(after),
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toProcurementDiscrepancyDTO(after))
}

type supplierBalanceDTO struct {
	SupplierID        uuid.UUID `json:"supplier_id"`
	SupplierName      string    `json:"supplier_name"`
	OutstandingAmount string    `json:"outstanding_amount"`
	InvoiceCount      int       `json:"invoice_count"`
}

type priceTrendDTO struct {
	SupplierID         uuid.UUID `json:"supplier_id"`
	SupplierName       string    `json:"supplier_name"`
	ProductID          uuid.UUID `json:"product_id"`
	ProductName        string    `json:"product_name"`
	ReceivedAt         string    `json:"received_at"`
	LandedCostPerLitre string    `json:"landed_cost_per_litre"`
}

type procurementOverviewDTO struct {
	Station          stationDTO           `json:"station"`
	OpenOrders       []purchaseOrderDTO   `json:"open_purchase_orders"`
	RecentReceipts   []deliveryDTO        `json:"recent_receipts"`
	SupplierBalances []supplierBalanceDTO `json:"supplier_balances"`
	PriceTrend       []priceTrendDTO      `json:"price_trend"`
}

func (s *Server) handleProcurementOverview(w http.ResponseWriter, r *http.Request) {
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
	ctx := r.Context()
	station, err := s.stations.Get(ctx, actor.TenantID, stationID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	orders, err := s.procurement.OpenPurchaseOrdersForStation(ctx, actor.TenantID, stationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	receipts, err := s.inventory.ListDeliveriesForStation(ctx, actor.TenantID, stationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	balances, err := s.procurement.SupplierBalancesForStation(ctx, actor.TenantID, stationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	trend, err := s.procurement.PriceTrendForStation(ctx, actor.TenantID, stationID, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := procurementOverviewDTO{
		Station:          toStationDTO(station),
		OpenOrders:       []purchaseOrderDTO{},
		RecentReceipts:   []deliveryDTO{},
		SupplierBalances: []supplierBalanceDTO{},
		PriceTrend:       []priceTrendDTO{},
	}
	for i := range orders {
		out.OpenOrders = append(out.OpenOrders, toPurchaseOrderDTO(&orders[i]))
	}
	for i := range receipts {
		if i >= 10 {
			break
		}
		out.RecentReceipts = append(out.RecentReceipts, toDeliveryDTO(&receipts[i]))
	}
	for i := range balances {
		out.SupplierBalances = append(out.SupplierBalances, supplierBalanceDTO{
			SupplierID: balances[i].SupplierID, SupplierName: balances[i].SupplierName,
			OutstandingAmount: balances[i].OutstandingAmount, InvoiceCount: balances[i].InvoiceCount,
		})
	}
	for i := range trend {
		out.PriceTrend = append(out.PriceTrend, priceTrendDTO{
			SupplierID: trend[i].SupplierID, SupplierName: trend[i].SupplierName,
			ProductID: trend[i].ProductID, ProductName: trend[i].ProductName,
			ReceivedAt:         trend[i].ReceivedAt.Format(time.RFC3339),
			LandedCostPerLitre: trend[i].LandedCostPerLitre,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) purchaseOrderForStationPermission(w http.ResponseWriter, r *http.Request, perm string) (identity.Actor, *procurement.PurchaseOrder, bool) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return identity.Actor{}, nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid purchase order id")
		return actor, nil, false
	}
	po, err := s.procurement.GetPurchaseOrder(r.Context(), actor.TenantID, id)
	if errors.Is(err, procurement.ErrNotFound) {
		writeError(w, http.StatusNotFound, "purchase order not found")
		return actor, nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return actor, nil, false
	}
	if !s.authorizeStation(w, r, actor, perm, po.StationID) {
		return actor, nil, false
	}
	return actor, po, true
}

func (s *Server) invoiceForStationPermission(w http.ResponseWriter, r *http.Request, perm string) (identity.Actor, *procurement.SupplierInvoice, bool) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return identity.Actor{}, nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid supplier invoice id")
		return actor, nil, false
	}
	inv, err := s.procurement.GetSupplierInvoice(r.Context(), actor.TenantID, id)
	if errors.Is(err, procurement.ErrNotFound) {
		writeError(w, http.StatusNotFound, "supplier invoice not found")
		return actor, nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return actor, nil, false
	}
	if !s.authorizeStation(w, r, actor, perm, inv.StationID) {
		return actor, nil, false
	}
	return actor, inv, true
}

func poTransitionEvent(status string) (string, string) {
	switch status {
	case procurement.POStatusSubmitted:
		return "purchase_order.submitted", "PurchaseOrderSubmitted"
	case procurement.POStatusConfirmed:
		return "purchase_order.confirmed", "PurchaseOrderConfirmed"
	case procurement.POStatusCancelled:
		return "purchase_order.cancelled", "PurchaseOrderCancelled"
	case procurement.POStatusClosed:
		return "purchase_order.closed", "PurchaseOrderClosed"
	default:
		return "purchase_order.updated", "PurchaseOrderUpdated"
	}
}

func validDecimal(v string) bool {
	v = strings.TrimSpace(v)
	return v != "" && decimalPattern.MatchString(v)
}

func validOptionalDecimal(v *string) bool {
	if v == nil {
		return true
	}
	return validDecimal(*v)
}

func trimOptionalDecimal(v *string) *string {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(*v)
	return &s
}

func defaultDecimal(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "0"
	}
	return v
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
