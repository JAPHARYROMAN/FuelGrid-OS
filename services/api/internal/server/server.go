// Package server owns the HTTP composition root: router, middleware
// stack, route table, and graceful lifecycle.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/japharyroman/fuelgrid-os/internal/cache"
	"github.com/japharyroman/fuelgrid-os/internal/calibration"
	"github.com/japharyroman/fuelgrid-os/internal/companies"
	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
	"github.com/japharyroman/fuelgrid-os/internal/identity/repo"
	"github.com/japharyroman/fuelgrid-os/internal/incidents"
	"github.com/japharyroman/fuelgrid-os/internal/inventory"
	"github.com/japharyroman/fuelgrid-os/internal/nozzles"
	"github.com/japharyroman/fuelgrid-os/internal/observability"
	"github.com/japharyroman/fuelgrid-os/internal/operations"
	"github.com/japharyroman/fuelgrid-os/internal/payments"
	"github.com/japharyroman/fuelgrid-os/internal/pricing"
	"github.com/japharyroman/fuelgrid-os/internal/procurement"
	"github.com/japharyroman/fuelgrid-os/internal/products"
	"github.com/japharyroman/fuelgrid-os/internal/pumps"
	"github.com/japharyroman/fuelgrid-os/internal/readings"
	"github.com/japharyroman/fuelgrid-os/internal/receivables"
	"github.com/japharyroman/fuelgrid-os/internal/reconciliation"
	"github.com/japharyroman/fuelgrid-os/internal/regions"
	"github.com/japharyroman/fuelgrid-os/internal/revenue"
	"github.com/japharyroman/fuelgrid-os/internal/stations"
	"github.com/japharyroman/fuelgrid-os/internal/tanks"
	"github.com/japharyroman/fuelgrid-os/services/api/internal/config"
)

// Deps groups the backing services the API depends on. DB and Redis may be
// nil for thin smoke tests — the readiness probe skips probes for nil deps.
// Identity and Policy must be non-nil whenever auth/admin routes are
// reachable.
type Deps struct {
	DB       *database.Pool
	Redis    *cache.Client
	Identity *identity.Service
	Policy   *policy.Service
	Metrics  *observability.Metrics
}

// Server owns the chi router and the embedded *http.Server. It is the
// composition root for every middleware and route the API exposes.
type Server struct {
	cfg      config.Config
	logger   *slog.Logger
	deps     Deps
	identity *identity.Service
	policy   *policy.Service
	metrics  *observability.Metrics

	companies      *companies.Repo
	regions        *regions.Repo
	stations       *stations.Repo
	products       *products.Repo
	tanks          *tanks.Repo
	pumps          *pumps.Repo
	nozzles        *nozzles.Repo
	calibration    *calibration.Repo
	incidents      *incidents.Repo
	operations     *operations.Repo
	readings       *readings.Repo
	inventory      *inventory.Repo
	payments       *payments.Repo
	pricing        *pricing.Repo
	procurement    *procurement.Repo
	receivables    *receivables.Repo
	reconciliation *reconciliation.Repo
	revenue        *revenue.Repo
	userRepo       *repo.UserRepo
	sessionRepo    *repo.SessionRepo

	http *http.Server
}

// New wires the router, middleware stack, and route table for the API.
// It does not start the listener — call Start for that.
func New(cfg config.Config, logger *slog.Logger, deps Deps) *Server {
	s := &Server{
		cfg:      cfg,
		logger:   logger,
		deps:     deps,
		identity: deps.Identity,
		policy:   deps.Policy,
		metrics:  deps.Metrics,
	}

	// Admin / domain repos only get built when the pool is up. Handlers
	// gate themselves on s.deps.DB == nil checks at registration time.
	if deps.DB != nil {
		s.companies = companies.New(deps.DB)
		s.regions = regions.New(deps.DB)
		s.stations = stations.New(deps.DB)
		s.products = products.New(deps.DB)
		s.tanks = tanks.New(deps.DB)
		s.pumps = pumps.New(deps.DB)
		s.nozzles = nozzles.New(deps.DB)
		s.calibration = calibration.New(deps.DB)
		s.incidents = incidents.New(deps.DB)
		s.operations = operations.New(deps.DB)
		s.readings = readings.New(deps.DB)
		s.inventory = inventory.New(deps.DB)
		s.payments = payments.New(deps.DB)
		s.pricing = pricing.New(deps.DB)
		s.procurement = procurement.New(deps.DB)
		s.receivables = receivables.New(deps.DB)
		s.reconciliation = reconciliation.New(deps.DB)
		s.revenue = revenue.New(deps.DB)
		s.userRepo = repo.NewUserRepo(deps.DB)
		s.sessionRepo = repo.NewSessionRepo(deps.DB)
	}

	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(echoRequestID)
	r.Use(s.logRequests)
	r.Use(s.recordMetrics)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(30 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSOrigins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Request-Id"},
		ExposedHeaders:   []string{"X-Request-Id"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)
	r.Get("/metrics", s.handleMetrics)

	r.Route("/api/v1", func(r chi.Router) {
		// Platform provisioning — its own static-token auth, not user
		// sessions. Mounted regardless of identity wiring; the middleware
		// 404s when PLATFORM_ADMIN_TOKEN is unset.
		r.With(s.requirePlatformAdmin).Post("/platform/tenants", s.handleCreateTenant)

		if s.identity != nil {
			r.Route("/auth", func(r chi.Router) {
				r.Post("/login", s.handleLogin)
				r.Post("/logout", s.handleLogout)
				r.Post("/refresh", s.handleRefresh)
				r.Post("/password-reset/request", s.handlePasswordResetRequest)
				r.Post("/password-reset/confirm", s.handlePasswordResetConfirm)

				r.Group(func(r chi.Router) {
					r.Use(s.requireAuth)
					r.Post("/mfa/enroll", s.handleMfaEnroll)
					r.Post("/mfa/verify", s.handleMfaVerify)
				})
			})

			// Authenticated routes (no specific permission gate beyond
			// having a session).
			r.Group(func(r chi.Router) {
				r.Use(s.requireAuth)
				r.Get("/me", s.handleMe)
				if s.operations != nil {
					// Self-scoped: returns only the actor's own shift + assignments.
					r.Get("/me/active-shift", s.handleMyActiveShift)
				}
				if s.policy != nil {
					r.Get("/me/permissions", s.handleMePermissions)
				}
				if s.sessionRepo != nil {
					r.Get("/me/sessions", s.handleListMySessions)
					r.Delete("/me/sessions/{sessionID}", s.handleRevokeMySession)
					r.Post("/me/password", s.handleChangeMyPassword)
				}
			})

			if s.policy != nil {
				// Station read (existing Stage-5 endpoint, now backed by
				// the proper stations repo).
				r.Group(func(r chi.Router) {
					r.Use(s.requireAuth)
					r.With(s.requirePermission("station.read", stationFromURLParam("stationID"))).
						Get("/stations/{stationID}", s.handleGetStation)

					r.With(s.requirePermission("station.read", stationFromURLParam("stationID"))).
						Get("/stations/{stationID}/overview", s.handleStationOverview)

					r.With(s.requirePermission("station.read", stationFromURLParam("stationID"))).
						Get("/stations/{stationID}/operations-overview", s.handleOperationsOverview)

					r.With(s.requirePermission("audit.read", nil)).
						Get("/audit-logs", s.handleListAuditLogs)

					r.With(s.requirePermission("users.assign_roles", nil)).
						Post("/admin/users/{userID}/roles", s.handleGrantRole)
				})

				// Admin console surface. Everything beyond this point
				// is tenant-wide and writes audit + outbox via the
				// audit.WriteWithOutbox helper.
				if s.companies != nil {
					r.Group(func(r chi.Router) {
						r.Use(s.requireAuth)

						r.With(s.requirePermissionHeld("station.read")).
							Get("/companies", s.handleListCompanies)
						r.With(s.requirePermission("companies.manage", nil)).Group(func(r chi.Router) {
							r.Post("/companies", s.handleCreateCompany)
							r.Patch("/companies/{id}", s.handleUpdateCompany)
							r.Delete("/companies/{id}", s.handleDeleteCompany)
						})

						r.With(s.requirePermissionHeld("station.read")).
							Get("/regions", s.handleListRegions)
						r.With(s.requirePermission("regions.manage", nil)).Group(func(r chi.Router) {
							r.Post("/regions", s.handleCreateRegion)
							r.Patch("/regions/{id}", s.handleUpdateRegion)
							r.Delete("/regions/{id}", s.handleDeleteRegion)
						})

						r.With(s.requirePermissionHeld("station.read")).
							Get("/stations", s.handleListStations)
						r.With(s.requirePermission("station.manage", nil)).Group(func(r chi.Router) {
							r.Post("/stations", s.handleCreateStation)
							r.Patch("/stations/{stationID}", s.handleUpdateStation)
							r.Delete("/stations/{stationID}", s.handleDeleteStation)
						})

						r.With(s.requirePermissionHeld("station.read")).Group(func(r chi.Router) {
							r.Get("/products", s.handleListProducts)
							r.Get("/products/{id}", s.handleGetProduct)
						})
						r.With(s.requirePermission("products.manage", nil)).Group(func(r chi.Router) {
							r.Post("/products", s.handleCreateProduct)
							r.Patch("/products/{id}", s.handleUpdateProduct)
							r.Delete("/products/{id}", s.handleDeleteProduct)
						})

						// Tanks: reads ride tenant-wide station.read; writes are
						// station-scoped (tanks.manage) and authorized in-handler
						// against the station from the body or the target row.
						r.With(s.requirePermissionHeld("station.read")).Group(func(r chi.Router) {
							r.Get("/tanks", s.handleListTanks)
							r.Get("/tanks/{id}", s.handleGetTank)
						})
						r.Post("/tanks", s.handleCreateTank)
						r.Patch("/tanks/{id}", s.handleUpdateTank)
						r.Delete("/tanks/{id}", s.handleDeleteTank)

						// Pumps & nozzles: reads ride tenant-wide station.read;
						// writes are station-scoped (pumps.manage) and authorized
						// in-handler. Nozzle mutations fold into pumps.manage.
						r.With(s.requirePermissionHeld("station.read")).Group(func(r chi.Router) {
							r.Get("/pumps", s.handleListPumps)
							r.Get("/pumps/{id}", s.handleGetPump)
							r.Get("/nozzles", s.handleListNozzles)
						})
						r.Post("/pumps", s.handleCreatePump)
						r.Patch("/pumps/{id}", s.handleUpdatePump)
						r.Delete("/pumps/{id}", s.handleDeletePump)
						r.Post("/nozzles", s.handleCreateNozzle)
						r.Patch("/nozzles/{id}", s.handleUpdateNozzle)
						r.Delete("/nozzles/{id}", s.handleDeleteNozzle)

						// Tank calibration: reads ride station.read; CSV upload
						// is station-scoped (tanks.calibrate), authorized
						// in-handler against the tank's station.
						r.With(s.requirePermissionHeld("station.read")).Group(func(r chi.Router) {
							r.Get("/tanks/{id}/calibration-charts", s.handleListCalibrationCharts)
							r.Get("/tanks/{id}/calibration-charts/active", s.handleGetActiveCalibrationChart)
							r.Get("/tanks/{id}/calibrated-volume", s.handleCalibratedVolume)
						})
						r.Post("/tanks/{id}/calibration-charts", s.handleUploadCalibrationChart)

						// Stock ledger (Phase 4, Stage 1). Per-tank append-only
						// movement history and derived book balance; both gated by
						// the station-scoped inventory.read, authorized in-handler
						// against the tank's station.
						r.Get("/tanks/{id}/ledger", s.handleListTankLedger)
						r.Get("/tanks/{id}/book-balance", s.handleGetTankBookBalance)
						// Opening balance (Phase 4, Stage 2): seed a tank's ledger
						// from its first dip or a manual figure. Manual stock writes
						// reuse the station-scoped stock.adjust, authorized in-handler.
						r.Post("/tanks/{id}/opening-balance", s.handleSetTankOpeningBalance)

						// Deliveries (Phase 4, Stage 3): receive posts a +volume
						// 'delivery' movement; reads ride inventory.read. Receive is
						// station-scoped (delivery.receive), authorized in-handler.
						r.Get("/tanks/{id}/deliveries", s.handleListTankDeliveries)
						r.Post("/tanks/{id}/deliveries", s.handleReceiveDelivery)
						r.With(s.requirePermission("inventory.read", stationFromURLParam("stationID"))).
							Get("/stations/{stationID}/deliveries", s.handleListStationDeliveries)
						r.Get("/deliveries/{id}", s.handleGetDeliveryReceipt)

						// Procurement (Phase 5): supplier master, station-scoped
						// purchase orders, PO-backed goods receipts, supplier
						// invoice matching, and overview surfaces.
						r.With(s.requirePermissionHeld("purchase_order.read")).Group(func(r chi.Router) {
							r.Get("/suppliers", s.handleListSuppliers)
							r.Get("/suppliers/{id}", s.handleGetSupplier)
							r.Get("/purchase-orders", s.handleListPurchaseOrders)
						})
						r.With(s.requirePermission("supplier.manage", nil)).Group(func(r chi.Router) {
							r.Post("/suppliers", s.handleCreateSupplier)
							r.Patch("/suppliers/{id}", s.handleUpdateSupplier)
							r.Delete("/suppliers/{id}", s.handleDeactivateSupplier)
						})
						r.Post("/purchase-orders", s.handleCreatePurchaseOrder)
						r.Get("/purchase-orders/{id}", s.handleGetPurchaseOrder)
						r.Patch("/purchase-orders/{id}", s.handleUpdatePurchaseOrder)
						r.Post("/purchase-orders/{id}/status", s.handleTransitionPurchaseOrder)
						r.Post("/purchase-orders/{id}/receipts", s.handleReceivePurchaseOrderReceipt)
						r.Post("/supplier-invoices", s.handleRecordSupplierInvoice)
						r.Get("/supplier-invoices/{id}", s.handleGetSupplierInvoice)
						r.Post("/supplier-invoices/{id}/approve", s.handleApproveSupplierInvoice)
						r.Patch("/procurement-discrepancies/{id}/status", s.handleResolveProcurementDiscrepancy)
						r.With(s.requirePermission("purchase_order.read", stationFromURLParam("stationID"))).
							Get("/stations/{stationID}/procurement-overview", s.handleProcurementOverview)

						// Reconciliation (Phase 4, Stages 5-6). Preview/get/list ride
						// reconciliation.read; run/adjust/seal are reconciliation.manage,
						// all authorized in-handler against the tank's station.
						r.Get("/tanks/{id}/reconciliation-preview", s.handleReconciliationPreview)
						r.Get("/tanks/{id}/reconciliation", s.handleGetReconciliation)
						r.Post("/tanks/{id}/reconciliations", s.handlePersistReconciliation)
						r.Post("/reconciliations/{id}/adjustments", s.handleAdjustReconciliation)
						r.Post("/reconciliations/{id}/seal", s.handleSealReconciliation)
						r.With(s.requirePermission("reconciliation.read", stationFromURLParam("stationID"))).
							Get("/stations/{stationID}/reconciliations", s.handleListStationReconciliations)

						// Category D overviews (Phase 4, Stages 7-8): one-call
						// dashboards for the /inventory and /reconciliation screens.
						r.With(s.requirePermission("inventory.read", stationFromURLParam("stationID"))).
							Get("/stations/{stationID}/inventory-overview", s.handleInventoryOverview)
						r.With(s.requirePermission("reconciliation.read", stationFromURLParam("stationID"))).
							Get("/stations/{stationID}/reconciliation-overview", s.handleReconciliationOverview)

						// Pricing (Phase 6, Stages 1-2): selling price book. Writes are
						// station-scoped (price.change); reads ride pricing.read.
						r.With(s.requirePermission("price.change", stationFromURLParam("stationID"))).
							Post("/stations/{stationID}/prices", s.handleSetPrice)
						r.With(s.requirePermission("pricing.read", stationFromURLParam("stationID"))).Group(func(r chi.Router) {
							r.Get("/stations/{stationID}/price-board", s.handlePriceBoard)
							r.Get("/stations/{stationID}/price-history", s.handlePriceHistory)
						})

						// Recognized sales & valuation (Phase 6, Stages 3-4). Shift
						// sales authorize revenue.read in-handler against the shift's
						// station; station reads ride the URL station.
						r.Get("/shifts/{id}/sales", s.handleListShiftSales)
						r.With(s.requirePermission("revenue.read", stationFromURLParam("stationID"))).
							Get("/stations/{stationID}/sales", s.handleListStationSales)
						r.With(s.requirePermission("margin.view", stationFromURLParam("stationID"))).
							Get("/stations/{stationID}/inventory-valuation", s.handleInventoryValuation)

						// Tender (Phase 6, Stage 5): shift payments + reconciliation
						// against recognized revenue (in-handler station authz).
						r.Post("/shifts/{id}/payments", s.handleRecordPayment)
						r.Get("/shifts/{id}/payments", s.handleListShiftPayments)
						r.Get("/shifts/{id}/payment-reconciliation", s.handleShiftPaymentReconciliation)

						// Credit customers & receivables (Phase 6, Stage 6). Customers
						// are tenant-wide: reads ride customer.read, writes credit.manage.
						r.With(s.requirePermissionHeld("customer.read")).Group(func(r chi.Router) {
							r.Get("/customers", s.handleListCustomers)
							r.Get("/customers/{id}/statement", s.handleCustomerStatement)
						})
						r.With(s.requirePermission("credit.manage", nil)).Group(func(r chi.Router) {
							r.Post("/customers", s.handleCreateCustomer)
							r.Patch("/customers/{id}", s.handleUpdateCustomer)
							r.Post("/customers/{id}/payments", s.handleRecordCustomerPayment)
						})

						// Revenue close & dashboard (Phase 6, Stages 7-8).
						r.With(s.requirePermission("revenue.read", stationFromURLParam("stationID"))).Group(func(r chi.Router) {
							r.Post("/stations/{stationID}/revenue-days", s.handleComputeRevenueDay)
							r.Get("/stations/{stationID}/revenue-overview", s.handleRevenueOverview)
						})
						r.With(s.requirePermission("period.lock", nil)).
							Post("/revenue-days/{id}/lock", s.handleLockRevenueDay)
						r.With(s.requirePermissionHeld("customer.read")).
							Get("/ar-aging", s.handleARaging)

						// Pump calibration events + status lifecycle. Reads ride
						// station.read; calibration is station-scoped
						// (pumps.calibrate), status changes fold into pumps.manage
						// / tanks.manage — all authorized in-handler.
						r.With(s.requirePermissionHeld("station.read")).
							Get("/pumps/{id}/calibrations", s.handleListPumpCalibrations)
						r.Post("/pumps/{id}/calibrations", s.handleCreatePumpCalibration)
						r.Patch("/pumps/{id}/status", s.handleUpdatePumpStatus)
						r.Patch("/tanks/{id}/status", s.handleUpdateTankStatus)

						// Incidents queue. Reads ride station.read; writes are
						// station-scoped (incidents.manage), authorized in-handler.
						r.With(s.requirePermissionHeld("station.read")).
							Get("/incidents", s.handleListIncidents)
						r.Post("/incidents", s.handleCreateIncident)
						r.Patch("/incidents/{id}/status", s.handleUpdateIncidentStatus)

						// Operating days (Phase 3, Stage 1). Open/list are
						// station-nested and gated by the URL station; close/lock
						// are id-based and authorized in-handler against the day's
						// station (operations.manage_day).
						r.With(s.requirePermission("station.read", stationFromURLParam("stationID"))).
							Get("/stations/{stationID}/operating-days", s.handleListOperatingDays)
						r.With(s.requirePermission("operations.manage_day", stationFromURLParam("stationID"))).
							Post("/stations/{stationID}/operating-days", s.handleOpenOperatingDay)
						r.Get("/operating-days/{id}", s.handleGetOperatingDay)
						r.Patch("/operating-days/{id}/status", s.handleUpdateOperatingDayStatus)
						r.Patch("/operating-days/{id}/lock", s.handleLockOperatingDay)

						// Shifts (Phase 3, Stage 2). Open/list are station-nested
						// (shift.open / station.read via the URL); get/close and the
						// assignment routes are id-based and authorized in-handler.
						r.With(s.requirePermission("station.read", stationFromURLParam("stationID"))).
							Get("/stations/{stationID}/shifts", s.handleListShifts)
						r.With(s.requirePermission("shift.open", stationFromURLParam("stationID"))).
							Post("/stations/{stationID}/shifts", s.handleOpenShift)
						r.Get("/shifts/{id}", s.handleGetShift)
						r.Post("/shifts/{id}/attendants", s.handleAssignAttendant)
						r.Delete("/shifts/{id}/attendants/{userID}", s.handleUnassignAttendant)
						r.Post("/shifts/{id}/nozzle-assignments", s.handleAssignNozzle)
						r.Delete("/shifts/{id}/nozzle-assignments/{assignmentID}", s.handleUnassignNozzle)

						// Pump meter readings (Phase 3, Stage 3). All id-based on
						// the shift; reads authorize station.read in-handler, writes
						// reuse reading.edit via shiftForWrite.
						r.Get("/shifts/{id}/meter-readings", s.handleListMeterReadings)
						r.Post("/shifts/{id}/meter-readings", s.handleCaptureMeterReading)
						r.Post("/shifts/{id}/meter-readings/{readingID}/correct", s.handleCorrectMeterReading)

						// Tank dip readings (Phase 3, Stage 4). Capture resolves
						// litres via the tank's active calibration chart.
						r.Get("/shifts/{id}/dip-readings", s.handleListDipReadings)
						r.Post("/shifts/{id}/dip-readings", s.handleCaptureDipReading)
						r.Post("/shifts/{id}/dip-readings/{readingID}/correct", s.handleCorrectDipReading)

						// Shift close & cash reconciliation (Phase 3, Stage 5).
						r.Post("/shifts/{id}/close", s.handleCloseShift)
						r.Get("/shifts/{id}/close-summary", s.handleCloseSummary)
						r.Post("/shifts/{id}/cash-submission", s.handleSubmitCash)

						// Approval & exceptions (Phase 3, Stage 6). Day lock
						// (all-shifts-approved guard) already lives on the
						// operating-day routes above.
						r.Patch("/shifts/{id}/status", s.handleApproveShift)
						r.Get("/shifts/{id}/exceptions", s.handleListShiftExceptions)
						r.Patch("/shift-exceptions/{id}/status", s.handleResolveShiftException)

						r.With(s.requirePermission("users.manage", nil)).
							Get("/users", s.handleListUsers)
						r.With(s.requirePermission("users.invite", nil)).
							Post("/admin/users", s.handleInviteUser)
						r.With(s.requirePermission("users.manage", nil)).
							Patch("/admin/users/{userID}/status", s.handleUpdateUserStatus)
						r.With(s.requirePermission("users.assign_roles", nil)).
							Delete("/admin/users/{userID}/roles/{roleCode}", s.handleRevokeUserRole)
						r.With(s.requirePermission("users.assign_roles", nil)).Group(func(r chi.Router) {
							r.Post("/admin/users/{userID}/station-access", s.handleGrantStationAccess)
							r.Delete("/admin/users/{userID}/station-access/{stationID}", s.handleRevokeStationAccess)
						})

						r.With(s.requirePermission("users.manage", nil)).
							Get("/roles", s.handleListRoles)
					})
				}
			}
		}
	})

	s.http = &http.Server{
		Addr:              cfg.Addr(),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return s
}

// Start blocks on http.ListenAndServe. It returns nil on graceful
// shutdown via Shutdown; any other error is propagated.
func (s *Server) Start() error {
	s.logger.Info("api listening", "addr", s.cfg.Addr())
	if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown drains in-flight requests within the deadline carried by ctx.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("api shutting down")
	return s.http.Shutdown(ctx)
}
