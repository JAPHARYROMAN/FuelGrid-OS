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
	"github.com/japharyroman/fuelgrid-os/internal/nozzles"
	"github.com/japharyroman/fuelgrid-os/internal/observability"
	"github.com/japharyroman/fuelgrid-os/internal/operations"
	"github.com/japharyroman/fuelgrid-os/internal/products"
	"github.com/japharyroman/fuelgrid-os/internal/pumps"
	"github.com/japharyroman/fuelgrid-os/internal/readings"
	"github.com/japharyroman/fuelgrid-os/internal/regions"
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

	companies   *companies.Repo
	regions     *regions.Repo
	stations    *stations.Repo
	products    *products.Repo
	tanks       *tanks.Repo
	pumps       *pumps.Repo
	nozzles     *nozzles.Repo
	calibration *calibration.Repo
	incidents   *incidents.Repo
	operations  *operations.Repo
	readings    *readings.Repo
	userRepo    *repo.UserRepo
	sessionRepo *repo.SessionRepo

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
						r.Patch("/shifts/{id}/status", s.handleUpdateShiftStatus)
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
