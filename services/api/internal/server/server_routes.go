package server

// This file decomposes the single large route table that used to live inline
// in New() into per-domain register* functions.
//
// PURE REFACTOR INVARIANT: the exact route paths, HTTP methods, registration
// ORDER (chi is order-sensitive for overlapping patterns), and the middleware
// wrapping at every requireAuth / rateLimitPerTenant / requirePermission(Held)
// site are preserved byte-for-byte. registerRoutes is called by New() and runs
// each group in the same order the inline table did. The CONTRACT_STRICT route
// contract test and `go run ./services/api/cmd/routes` prove the registered set
// is identical before and after this split.

import "github.com/go-chi/chi/v5"

// registerRoutes wires the operational probes and the entire /api/v1 surface in
// the same order as the original inline table.
func (s *Server) registerRoutes(r chi.Router) {
	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)
	r.Get("/metrics", s.handleMetrics)

	r.Route("/api/v1", func(r chi.Router) {
		// Platform provisioning — its own static-token auth, not user
		// sessions. Mounted regardless of identity wiring; the middleware
		// 404s when PLATFORM_ADMIN_TOKEN is unset.
		s.registerPlatformRoutes(r)

		// M-Pesa (Daraja) result webhook — unauthenticated by design (Safaricom
		// posts it with no session) and keyed by the globally-unique checkout id
		// on the owner pool. Mounted outside every auth group.
		s.registerPaymentsWebhook(r)

		if s.identity != nil {
			s.registerAuthRoutes(r)

			// Authenticated routes (no specific permission gate beyond
			// having a session).
			s.registerSelfServiceRoutes(r)
			s.registerMfaRoutes(r)

			if s.policy != nil {
				// Station read (existing Stage-5 endpoint, now backed by
				// the proper stations repo).
				s.registerStationReadRoutes(r)

				// Admin console surface. Everything beyond this point
				// is tenant-wide and writes audit + outbox via the
				// audit.WriteWithOutbox helper.
				if s.companies != nil {
					r.Group(func(r chi.Router) {
						r.Use(s.requireAuth)
						r.Use(s.rateLimitPerTenant)

						s.registerCommercialMasterRoutes(r)
						s.registerInventoryRoutes(r)
						s.registerProcurementRoutes(r)
						s.registerReconciliationRoutes(r)
						s.registerPricingRoutes(r)
						s.registerRevenueRoutes(r)
						s.registerTenderRoutes(r)
						s.registerPaymentsRoutes(r)
						s.registerReceivablesRoutes(r)
						s.registerFleetCreditRoutes(r)
						s.registerEnterpriseRoutes(r)
						s.registerRiskRoutes(r)
						s.registerRevenueCloseRoutes(r)
						s.registerFinanceRoutes(r)
						s.registerReportsRoutes(r)
						s.registerReportInsightsRoutes(r)
						s.registerReportExcelRoutes(r)
						s.registerReportsStructuredRoutes(r)
						s.registerAccountingExportRoutes(r)
						s.registerOperationsRoutes(r)
						s.registerWorkforceRoutes(r)
						s.registerUserAdminRoutes(r)
						s.registerAdminJobRoutes(r)
					})
				}
			}
		}
	})
}

// registerPlatformRoutes mounts the static-token platform provisioning route.
func (s *Server) registerPlatformRoutes(r chi.Router) {
	r.With(s.requirePlatformAdmin).Post("/platform/tenants", s.handleCreateTenant)
}

// registerAuthRoutes mounts the /auth sub-router: the public session endpoints
// plus the authenticated MFA group (requireAuth site #1 + rateLimitPerTenant).
func (s *Server) registerAuthRoutes(r chi.Router) {
	r.Route("/auth", func(r chi.Router) {
		r.Post("/login", s.handleLogin)
		r.Post("/logout", s.handleLogout)
		r.Post("/refresh", s.handleRefresh)
		r.Post("/password-reset/request", s.handlePasswordResetRequest)
		r.Post("/password-reset/confirm", s.handlePasswordResetConfirm)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Use(s.rateLimitPerTenant)
			r.Post("/mfa/enroll", s.handleMfaEnroll)
			r.Post("/mfa/verify", s.handleMfaVerify)
		})
	})
}

// registerSelfServiceRoutes mounts the session-scoped /me routes
// (requireAuth site #2 + rateLimitPerTenant).
func (s *Server) registerSelfServiceRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Use(s.rateLimitPerTenant)
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
		if s.notifications != nil {
			// In-app notification feed — scoped to the caller's user/tenant,
			// so any authenticated user may read and mark their own feed.
			s.registerNotificationRoutes(r)
		}
	})
}

// registerStationReadRoutes mounts the station-read / audit / role-grant group
// (requireAuth site #3 + rateLimitPerTenant).
func (s *Server) registerStationReadRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Use(s.rateLimitPerTenant)
		// Held (not station-scoped) at the route so a cross-tenant
		// station id reaches the handler, where the tenant-scoped
		// load returns 404 — never 403 — keeping another tenant's
		// stations indistinguishable from non-existent. Per-station
		// scope is then enforced in-handler via authorizeStation,
		// so an in-tenant out-of-scope station still returns 403.
		r.With(s.requirePermissionHeld("station.read")).
			Get("/stations/{stationID}", s.handleGetStation)

		r.With(s.requirePermissionHeld("station.read")).
			Get("/stations/{stationID}/overview", s.handleStationOverview)

		r.With(s.requirePermissionHeld("station.read")).
			Get("/stations/{stationID}/operations-overview", s.handleOperationsOverview)

		r.With(s.requirePermission("audit.read", nil)).
			Get("/audit-logs", s.handleListAuditLogs)
		r.With(s.requirePermission("audit.read", nil)).
			Post("/audit-logs/export", s.handleExportAuditLogs)

		r.With(s.requirePermission("users.assign_roles", nil)).
			Post("/admin/users/{userID}/roles", s.handleGrantRole)
	})
}

// The following register* functions all run inside the admin-console group
// (requireAuth site #4 + rateLimitPerTenant) established in registerRoutes.
// They take the already-grouped router and register their domain's routes in
// the original order.

// registerCommercialMasterRoutes: companies, regions, stations, products.
func (s *Server) registerCommercialMasterRoutes(r chi.Router) {
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
}

// registerInventoryRoutes: tanks, pumps/nozzles, calibration, stock ledger,
// opening balance, and deliveries.
func (s *Server) registerInventoryRoutes(r chi.Router) {
	// Tanks: reads ride tenant-wide station.read; writes are
	// station-scoped (tanks.manage) and authorized in-handler
	// against the station from the body or the target row.
	r.With(s.requirePermissionHeld("station.read")).Group(func(r chi.Router) {
		r.Get("/tanks", s.handleListTanks)
		r.Get("/tanks/{id}", s.handleGetTank)
	})
	r.With(s.requirePermissionHeld("tanks.manage")).Post("/tanks", s.handleCreateTank)
	r.With(s.requirePermissionHeld("tanks.manage")).Patch("/tanks/{id}", s.handleUpdateTank)
	r.With(s.requirePermissionHeld("tanks.manage")).Delete("/tanks/{id}", s.handleDeleteTank)

	// Pumps & nozzles: reads ride tenant-wide station.read;
	// writes are station-scoped (pumps.manage) and authorized
	// in-handler. Nozzle mutations fold into pumps.manage.
	r.With(s.requirePermissionHeld("station.read")).Group(func(r chi.Router) {
		r.Get("/pumps", s.handleListPumps)
		r.Get("/pumps/{id}", s.handleGetPump)
		r.Get("/nozzles", s.handleListNozzles)
	})
	r.With(s.requirePermissionHeld("pumps.manage")).Post("/pumps", s.handleCreatePump)
	r.With(s.requirePermissionHeld("pumps.manage")).Patch("/pumps/{id}", s.handleUpdatePump)
	r.With(s.requirePermissionHeld("pumps.manage")).Delete("/pumps/{id}", s.handleDeletePump)
	r.With(s.requirePermissionHeld("pumps.manage")).Post("/nozzles", s.handleCreateNozzle)
	r.With(s.requirePermissionHeld("pumps.manage")).Patch("/nozzles/{id}", s.handleUpdateNozzle)
	r.With(s.requirePermissionHeld("pumps.manage")).Delete("/nozzles/{id}", s.handleDeleteNozzle)

	// Tank calibration: reads ride station.read; CSV upload
	// is station-scoped (tanks.calibrate), authorized
	// in-handler against the tank's station.
	r.With(s.requirePermissionHeld("station.read")).Group(func(r chi.Router) {
		r.Get("/tanks/{id}/calibration-charts", s.handleListCalibrationCharts)
		r.Get("/tanks/{id}/calibration-charts/active", s.handleGetActiveCalibrationChart)
		r.Get("/tanks/{id}/calibrated-volume", s.handleCalibratedVolume)
	})
	r.With(s.requirePermissionHeld("tanks.calibrate")).Post("/tanks/{id}/calibration-charts", s.handleUploadCalibrationChart)

	// Stock ledger (Phase 4, Stage 1). Per-tank append-only
	// movement history and derived book balance; both gated by
	// the station-scoped inventory.read, authorized in-handler
	// against the tank's station.
	r.Get("/tanks/{id}/ledger", s.handleListTankLedger)
	r.Get("/tanks/{id}/book-balance", s.handleGetTankBookBalance)
	// Opening balance (Phase 4, Stage 2): seed a tank's ledger
	// from its first dip or a manual figure. Manual stock writes
	// reuse the station-scoped stock.adjust, authorized in-handler.
	r.With(s.requirePermissionHeld("stock.adjust")).Post("/tanks/{id}/opening-balance", s.handleSetTankOpeningBalance)

	// Deliveries (Phase 4, Stage 3): receive posts a +volume
	// 'delivery' movement; reads ride inventory.read. Receive is
	// station-scoped (delivery.receive), authorized in-handler.
	r.Get("/tanks/{id}/deliveries", s.handleListTankDeliveries)
	r.With(s.requirePermissionHeld("delivery.receive")).Post("/tanks/{id}/deliveries", s.handleReceiveDelivery)
	r.With(s.requirePermission("inventory.read", stationFromURLParam("stationID"))).
		Get("/stations/{stationID}/deliveries", s.handleListStationDeliveries)
	r.Get("/deliveries/{id}", s.handleGetDeliveryReceipt)
}

// registerProcurementRoutes (Phase 5): supplier master, station-scoped purchase
// orders, PO-backed goods receipts, supplier invoice matching, and overviews.
func (s *Server) registerProcurementRoutes(r chi.Router) {
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
	r.With(s.requirePermissionHeld("purchase_order.manage")).Post("/purchase-orders", s.handleCreatePurchaseOrder)
	r.Get("/purchase-orders/{id}", s.handleGetPurchaseOrder)
	r.With(s.requirePermissionHeld("purchase_order.manage")).Patch("/purchase-orders/{id}", s.handleUpdatePurchaseOrder)
	r.With(s.requirePermissionHeld("purchase_order.approve")).Post("/purchase-orders/{id}/status", s.handleTransitionPurchaseOrder)
	r.With(s.requirePermissionHeld("delivery.receive")).Post("/purchase-orders/{id}/receipts", s.handleReceivePurchaseOrderReceipt)
	r.With(s.requirePermissionHeld("invoice.manage")).Post("/supplier-invoices", s.handleRecordSupplierInvoice)
	r.Get("/supplier-invoices/{id}", s.handleGetSupplierInvoice)
	r.With(s.requirePermissionHeld("invoice.approve")).Post("/supplier-invoices/{id}/approve", s.handleApproveSupplierInvoice)
	r.With(s.requirePermissionHeld("invoice.approve")).Patch("/procurement-discrepancies/{id}/status", s.handleResolveProcurementDiscrepancy)
	r.With(s.requirePermission("purchase_order.read", stationFromURLParam("stationID"))).
		Get("/stations/{stationID}/procurement-overview", s.handleProcurementOverview)
}

// registerReconciliationRoutes (Phase 4, Stages 5-8): reconciliation lifecycle
// plus the inventory/reconciliation overview dashboards.
func (s *Server) registerReconciliationRoutes(r chi.Router) {
	// Reconciliation (Phase 4, Stages 5-6). Preview/get/list ride
	// reconciliation.read; run/adjust/seal are reconciliation.manage,
	// all authorized in-handler against the tank's station.
	r.Get("/tanks/{id}/reconciliation-preview", s.handleReconciliationPreview)
	r.Get("/tanks/{id}/reconciliation", s.handleGetReconciliation)
	r.With(s.requirePermissionHeld("reconciliation.manage")).Post("/tanks/{id}/reconciliations", s.handlePersistReconciliation)
	r.With(s.requirePermissionHeld("reconciliation.manage")).Post("/reconciliations/{id}/adjustments", s.handleAdjustReconciliation)
	r.With(s.requirePermissionHeld("reconciliation.manage")).Post("/reconciliations/{id}/seal", s.handleSealReconciliation)
	r.With(s.requirePermission("reconciliation.read", stationFromURLParam("stationID"))).
		Get("/stations/{stationID}/reconciliations", s.handleListStationReconciliations)

	// Category D overviews (Phase 4, Stages 7-8): one-call
	// dashboards for the /inventory and /reconciliation screens.
	r.With(s.requirePermission("inventory.read", stationFromURLParam("stationID"))).
		Get("/stations/{stationID}/inventory-overview", s.handleInventoryOverview)
	r.With(s.requirePermission("reconciliation.read", stationFromURLParam("stationID"))).
		Get("/stations/{stationID}/reconciliation-overview", s.handleReconciliationOverview)
}

// registerPricingRoutes (Phase 6, Stages 1-2): selling price book.
func (s *Server) registerPricingRoutes(r chi.Router) {
	// Pricing (Phase 6, Stages 1-2): selling price book. Writes are
	// station-scoped (price.change); reads ride pricing.read.
	r.With(s.requirePermission("price.change", stationFromURLParam("stationID"))).
		Post("/stations/{stationID}/prices", s.handleSetPrice)
	r.With(s.requirePermission("pricing.read", stationFromURLParam("stationID"))).Group(func(r chi.Router) {
		r.Get("/stations/{stationID}/price-board", s.handlePriceBoard)
		r.Get("/stations/{stationID}/price-history", s.handlePriceHistory)
	})
}

// registerRevenueRoutes (Phase 6, Stages 3-4): recognized sales & valuation.
func (s *Server) registerRevenueRoutes(r chi.Router) {
	// Recognized sales & valuation (Phase 6, Stages 3-4). Shift
	// sales authorize revenue.read in-handler against the shift's
	// station; station reads ride the URL station.
	r.Get("/shifts/{id}/sales", s.handleListShiftSales)
	r.With(s.requirePermission("revenue.read", stationFromURLParam("stationID"))).
		Get("/stations/{stationID}/sales", s.handleListStationSales)
	r.With(s.requirePermission("margin.view", stationFromURLParam("stationID"))).
		Get("/stations/{stationID}/inventory-valuation", s.handleInventoryValuation)
}

// registerTenderRoutes (Phase 6, Stage 5): shift payments + reconciliation
// against recognized revenue (in-handler station authz).
func (s *Server) registerTenderRoutes(r chi.Router) {
	r.With(s.requirePermissionHeld("payment.record")).Post("/shifts/{id}/payments", s.handleRecordPayment)
	r.Get("/shifts/{id}/payments", s.handleListShiftPayments)
	r.Get("/shifts/{id}/payment-reconciliation", s.handleShiftPaymentReconciliation)
}

// registerReceivablesRoutes (Phase 6, Stage 6): credit customers & receivables.
func (s *Server) registerReceivablesRoutes(r chi.Router) {
	// Credit customers & receivables (Phase 6, Stage 6). Customers
	// are tenant-wide: reads ride customer.read, writes credit.manage.
	r.With(s.requirePermissionHeld("customer.read")).Group(func(r chi.Router) {
		r.Get("/customers", s.handleListCustomers)
		r.Get("/customers/{id}/statement", s.handleCustomerStatement)
		r.Get("/customers/{id}/contacts", s.handleListCustomerContacts)
	})
	r.With(s.requirePermission("credit.manage", nil)).Group(func(r chi.Router) {
		r.Post("/customers", s.handleCreateCustomer)
		r.Patch("/customers/{id}", s.handleUpdateCustomer)
		r.Post("/customers/{id}/payments", s.handleRecordCustomerPayment)
	})
}

// registerFleetCreditRoutes (Phase 8): customer credit & fleet fuel OS.
func (s *Server) registerFleetCreditRoutes(r chi.Router) {
	// ===== Phase 8: Customer Credit & Fleet Fuel OS =====
	// Customer master lifecycle + contacts (Stage 1).
	r.With(s.requirePermission("customer.manage", nil)).Group(func(r chi.Router) {
		r.Post("/customers/{id}/status", s.handleSetCustomerStatus)
		r.Post("/customers/{id}/contacts", s.handleCreateCustomerContact)
		r.Delete("/customers/{id}/contacts/{contactID}", s.handleDeleteCustomerContact)
	})
	// Credit profile, position & holds (Stage 2).
	r.With(s.requirePermissionHeld("customer_credit.read")).Group(func(r chi.Router) {
		r.Get("/customers/{id}/credit-profile", s.handleGetCreditProfile)
		r.Get("/customers/{id}/credit-position", s.handleCreditPosition)
	})
	r.With(s.requirePermission("customer_credit.manage", nil)).Group(func(r chi.Router) {
		r.Put("/customers/{id}/credit-profile", s.handleUpsertCreditProfile)
		r.Post("/customers/{id}/credit-hold", s.handleSetCreditHold)
	})
	// Customer price agreements (Stage 3).
	r.With(s.requirePermissionHeld("customer_credit.read")).
		Get("/customer-price-agreements", s.handleListPriceAgreements)
	r.With(s.requirePermission("customer_pricing.manage", nil)).
		Post("/customer-price-agreements", s.handleCreatePriceAgreement)
	r.With(s.requirePermission("customer_pricing.approve", nil)).Group(func(r chi.Router) {
		r.Post("/customer-price-agreements/{id}/approve", s.handleTransitionPriceAgreement("approve"))
		r.Post("/customer-price-agreements/{id}/activate", s.handleTransitionPriceAgreement("activate"))
		r.Post("/customer-price-agreements/{id}/cancel", s.handleTransitionPriceAgreement("cancel"))
	})

	// Fleet identity: vehicles, drivers, credentials (Stages 4-6).
	r.With(s.requirePermissionHeld("customer.read")).Group(func(r chi.Router) {
		r.Get("/fleet/vehicles", s.handleListVehicles)
		r.Get("/fleet/drivers", s.handleListDrivers)
		r.Get("/fleet/credentials", s.handleListCredentials)
	})
	r.With(s.requirePermission("customer.manage", nil)).Group(func(r chi.Router) {
		r.Post("/fleet/vehicles", s.handleCreateVehicle)
		r.Post("/fleet/vehicles/{id}/status", s.handleSetVehicleStatus)
		r.Post("/fleet/drivers", s.handleCreateDriver)
		r.Post("/fleet/drivers/{id}/status", s.handleSetDriverStatus)
		r.Post("/fleet/drivers/{id}/reset-pin", s.handleResetDriverPIN)
	})
	r.With(s.requirePermission("fuel_credential.issue", nil)).
		Post("/fleet/credentials", s.handleIssueCredential)
	r.With(s.requirePermission("fuel_credential.manage", nil)).Group(func(r chi.Router) {
		r.Post("/fleet/credentials/{id}/status", s.handleSetCredentialStatus)
		r.Post("/fleet/credentials/validate", s.handleValidateCredential)
	})

	// Authorization & forecourt (Stages 7-9).
	r.With(s.requirePermissionHeld("customer.read")).Group(func(r chi.Router) {
		r.Get("/fuel-authorizations", s.handleListAuthorizations)
		r.Get("/fuel-authorizations/{id}", s.handleGetAuthorization)
		r.Get("/fuel-limits", s.handleListFuelLimits)
	})
	r.With(s.requirePermission("fuel_authorization.create", nil)).Group(func(r chi.Router) {
		r.Post("/fuel-authorizations", s.handleRequestAuthorization)
		r.Post("/fuel-authorizations/{id}/fulfill", s.handleFulfillAuthorization)
	})
	r.With(s.requirePermission("fuel_authorization.cancel", nil)).Group(func(r chi.Router) {
		r.Post("/fuel-authorizations/{id}/cancel", s.handleAuthorizationStatus("cancelled"))
		r.Post("/fuel-authorizations/{id}/void", s.handleAuthorizationStatus("voided"))
	})
	r.With(s.requirePermission("fuel_limit.manage", nil)).
		Post("/fuel-limits", s.handleCreateFuelLimit)

	// Odometer & fleet consumption (Stages 10-11).
	r.With(s.requirePermissionHeld("customer.read")).
		Get("/fleet/vehicles/{id}/odometer", s.handleListOdometer)
	r.With(s.requirePermission("fuel_authorization.create", nil)).
		Post("/fleet/vehicles/{id}/odometer", s.handleRecordOdometer)
	r.With(s.requirePermissionHeld("fleet_report.read")).
		Get("/fleet/consumption", s.handleFleetConsumption)

	// Statements & credit alerts (Stages 12-13).
	r.With(s.requirePermissionHeld("customer.read")).Group(func(r chi.Router) {
		r.Get("/customers/{id}/statements", s.handleListStatements)
		r.Get("/credit-alerts", s.handleListCreditAlerts)
	})
	r.With(s.requirePermission("customer_statement.issue", nil)).Group(func(r chi.Router) {
		r.Post("/customers/{id}/statements", s.handleGenerateStatement)
		r.Post("/customer-statements/{id}/issue", s.handleIssueStatement)
	})
	r.With(s.requirePermission("customer_credit_alert.manage", nil)).Group(func(r chi.Router) {
		r.Post("/credit-alerts/scan", s.handleScanCreditAlerts)
		r.Post("/credit-alerts/{id}/acknowledge", s.handleTransitionCreditAlert("acknowledged"))
		r.Post("/credit-alerts/{id}/resolve", s.handleTransitionCreditAlert("resolved"))
		r.Post("/credit-alerts/{id}/dismiss", s.handleTransitionCreditAlert("dismissed"))
	})
}

// registerEnterpriseRoutes (Phase 9): chain & enterprise command.
func (s *Server) registerEnterpriseRoutes(r chi.Router) {
	// ===== Phase 9: Chain & Enterprise Command =====
	// Hierarchy, scopes, approvals (Stages 1-3).
	r.With(s.requirePermissionHeld("enterprise.read")).Group(func(r chi.Router) {
		r.Get("/enterprise/station-groups", s.handleListStationGroups)
		r.Get("/approval-policies", s.handleListApprovalPolicies)
		r.Get("/approval-requests", s.handleListApprovalRequests)
		r.Post("/approval-requests", s.handleRaiseApprovalRequest)
	})
	r.With(s.requirePermission("enterprise_structure.manage", nil)).Group(func(r chi.Router) {
		r.Post("/enterprise/station-groups", s.handleCreateStationGroup)
		r.Post("/enterprise/station-groups/{id}/members", s.handleAddGroupMember)
	})
	r.With(s.requirePermissionHeld("enterprise_access.read")).
		Get("/enterprise/users/{id}/effective-stations", s.handleEffectiveStations)
	r.With(s.requirePermission("enterprise_access.manage", nil)).
		Post("/enterprise/scope-grants", s.handleGrantScope)
	r.With(s.requirePermission("approval_policy.manage", nil)).
		Post("/approval-policies", s.handleCreateApprovalPolicy)
	r.With(s.requirePermission("approval_request.decide", nil)).
		Post("/approval-requests/{id}/decide", s.handleDecideApproval)

	// Read models & command dashboards (Stages 4-6).
	r.With(s.requirePermissionHeld("enterprise.read")).Group(func(r chi.Router) {
		r.Get("/enterprise/overview", s.handleEnterpriseOverview)
		r.Get("/enterprise/station-ranking", s.handleStationRanking)
		r.Get("/enterprise/regions/{id}", s.handleStationRanking)
	})
	r.With(s.requirePermission("enterprise_projection.admin", nil)).
		Post("/enterprise/projections/rebuild", s.handleRebuildProjections)

	// Central commercial control (Stages 7-9).
	r.With(s.requirePermissionHeld("enterprise.read")).Group(func(r chi.Router) {
		r.Get("/central-price-rollouts", s.handleListPriceRollouts)
		r.Get("/central-procurement-plans", s.handleListProcurementPlans)
		r.Get("/stock-transfers", s.handleListTransfers)
	})
	r.With(s.requirePermission("central_pricing.manage", nil)).
		Post("/central-price-rollouts", s.handleCreatePriceRollout)
	r.With(s.requirePermission("central_pricing.approve", nil)).
		Post("/central-price-rollouts/{id}/approve", s.handleApprovePriceRollout)
	r.With(s.requirePermission("central_pricing.publish", nil)).
		Post("/central-price-rollouts/{id}/activate", s.handleActivatePriceRollout)
	r.With(s.requirePermission("central_procurement.manage", nil)).
		Post("/central-procurement-plans", s.handleCreateProcurementPlan)
	r.With(s.requirePermission("central_procurement.release", nil)).
		Post("/central-procurement-plans/{id}/release", s.handleReleaseProcurementPlan)
	r.With(s.requirePermission("stock_transfer.manage", nil)).
		Post("/stock-transfers", s.handleCreateTransfer)
	r.With(s.requirePermission("stock_transfer.approve", nil)).
		Post("/stock-transfers/{id}/approve", s.handleApproveTransfer)
	r.With(s.requirePermission("stock_transfer.receive", nil)).
		Post("/stock-transfers/{id}/receive", s.handleReceiveTransfer)

	// Consolidated finance & reports (Stages 10-11).
	r.With(s.requirePermissionHeld("finance.read")).
		Get("/enterprise/finance/consolidated", s.handleConsolidatedFinance)
	r.With(s.requirePermission("finance.export", nil)).
		Get("/enterprise/reports/station-kpis", s.handleStationKPIExport)

	// Enterprise operations UX — exception command queue (Stage 12).
	r.With(s.requirePermissionHeld("enterprise.read")).
		Get("/enterprise/exceptions", s.handleEnterpriseExceptions)
}

// registerRiskRoutes (Phase 10): risk, fraud & intelligence.
func (s *Server) registerRiskRoutes(r chi.Router) {
	// ===== Phase 10: Risk, Fraud & Intelligence =====
	// Signals, rules, detection, alerts (Stages 1-8).
	r.With(s.requirePermissionHeld("risk.read")).Group(func(r chi.Router) {
		r.Get("/risk/signals", s.handleListSignals)
		r.Get("/risk/rules", s.handleListRiskRules)
	})
	r.With(s.requirePermissionHeld("risk_alert.read")).Group(func(r chi.Router) {
		r.Get("/risk/alerts", s.handleListRiskAlerts)
		r.Get("/risk/alerts/{id}", s.handleGetRiskAlert)
	})
	r.With(s.requirePermission("risk_signal.admin", nil)).
		Post("/risk/signals/backfill", s.handleBackfillSignals)
	r.With(s.requirePermission("risk_rule.manage", nil)).Group(func(r chi.Router) {
		r.Post("/risk/rules", s.handleCreateRiskRule)
		r.Post("/risk/rules/{id}/status", s.handleSetRiskRuleStatus)
	})
	r.With(s.requirePermission("risk_alert.manage", nil)).Group(func(r chi.Router) {
		r.Post("/risk/detect", s.handleRunDetection)
		r.Post("/risk/alerts/{id}/acknowledge", s.handleTransitionRiskAlert("acknowledged"))
		r.Post("/risk/alerts/{id}/investigate", s.handleTransitionRiskAlert("investigating"))
		r.Post("/risk/alerts/{id}/resolve", s.handleTransitionRiskAlert("resolved"))
		r.Post("/risk/alerts/{id}/dismiss", s.handleTransitionRiskAlert("dismissed"))
		r.Post("/risk/alerts/{id}/escalate", s.handleTransitionRiskAlert("escalated"))
	})

	// Risk scoring & dashboard (Stages 9-10).
	r.With(s.requirePermissionHeld("risk.read")).Group(func(r chi.Router) {
		r.Get("/risk/overview", s.handleRiskOverview)
		r.Get("/risk/scores", s.handleListRiskScores)
	})
	r.With(s.requirePermission("risk_score.admin", nil)).
		Post("/risk/scores/recompute", s.handleRecomputeRiskScores)

	// Investigation workflow (Stages 11-13).
	r.With(s.requirePermissionHeld("investigation.read")).Group(func(r chi.Router) {
		r.Get("/investigations", s.handleListCases)
		r.Get("/investigations/{id}", s.handleGetCaseTimeline)
	})
	r.With(s.requirePermission("investigation.manage", nil)).Group(func(r chi.Router) {
		r.Post("/investigations", s.handleCreateCase)
		r.Post("/investigations/{id}/alerts", s.handleAttachAlertToCase)
		r.Post("/investigations/{id}/comments", s.handleAddCaseComment)
		r.Post("/investigations/{id}/actions", s.handleAddCaseAction)
		r.Post("/investigations/{id}/actions/{actionID}/status", s.handleSetCaseActionStatus)
		r.Post("/investigations/{id}/status", s.handleSetCaseStatus)
	})

	// Tuning, governance & trust (Stages 14-15).
	r.With(s.requirePermissionHeld("risk_governance.admin")).Group(func(r chi.Router) {
		r.Get("/risk/governance", s.handleRiskGovernance)
		r.Get("/risk/suppressions", s.handleListSuppressions)
		r.Post("/risk/engine/pause", s.handlePauseAllRiskRules)
	})
	r.With(s.requirePermission("risk_rule.tune", nil)).
		Post("/risk/rules/{id}/tune", s.handleTuneRiskRule)
	r.With(s.requirePermission("risk_alert.suppress", nil)).
		Post("/risk/suppressions", s.handleCreateSuppression)
}

// registerRevenueCloseRoutes (Phase 6, Stages 7-8): revenue close & dashboard.
func (s *Server) registerRevenueCloseRoutes(r chi.Router) {
	// Revenue close & dashboard (Phase 6, Stages 7-8).
	r.With(s.requirePermission("revenue.read", stationFromURLParam("stationID"))).Group(func(r chi.Router) {
		r.Post("/stations/{stationID}/revenue-days", s.handleComputeRevenueDay)
		r.Get("/stations/{stationID}/revenue-overview", s.handleRevenueOverview)
	})
	r.With(s.requirePermission("period.lock", nil)).
		Post("/revenue-days/{id}/lock", s.handleLockRevenueDay)
	r.With(s.requirePermissionHeld("customer.read")).
		Get("/ar-aging", s.handleARaging)
}

// registerFinanceRoutes (Phase 7): finance & accounting control — accounting
// foundation, payables, banking, customer invoices, expenses, reports, exports.
func (s *Server) registerFinanceRoutes(r chi.Router) {
	// ===== Phase 7: Finance & Accounting Control =====
	// Accounting foundation (Stages 1-3): chart of accounts,
	// periods, journal engine — all tenant-wide.
	r.With(s.requirePermissionHeld("finance.read")).Group(func(r chi.Router) {
		r.Get("/accounts", s.handleListAccounts)
		r.Get("/accounting-periods", s.handleListPeriods)
	})
	r.With(s.requirePermission("account.manage", nil)).Group(func(r chi.Router) {
		r.Post("/accounts", s.handleCreateAccount)
		r.Post("/accounts/seed-defaults", s.handleSeedDefaultChart)
		r.Patch("/accounts/{id}", s.handleUpdateAccount)
	})
	r.With(s.requirePermission("period.manage", nil)).
		Post("/accounting-periods", s.handleCreatePeriod)
	r.With(s.requirePermission("period.close", nil)).Group(func(r chi.Router) {
		r.Post("/accounting-periods/{id}/start-close", s.handlePeriodTransition("start_close"))
		r.Post("/accounting-periods/{id}/close", s.handlePeriodTransition("closed"))
	})
	r.With(s.requirePermission("period.reopen", nil)).
		Post("/accounting-periods/{id}/reopen", s.handlePeriodTransition("reopened"))
	r.With(s.requirePermission("period.lock", nil)).
		Post("/accounting-periods/{id}/lock", s.handlePeriodTransition("locked"))
	r.With(s.requirePermissionHeld("journal.read")).Group(func(r chi.Router) {
		r.Get("/journal-entries", s.handleListJournalEntries)
		r.Get("/journal-entries/{id}", s.handleGetJournalEntry)
	})
	r.With(s.requirePermission("journal.adjust", nil)).Group(func(r chi.Router) {
		r.Post("/journal-entries", s.handlePostAdjustment)
		r.Post("/journal-entries/{id}/reverse", s.handleReverseJournalEntry)
	})

	// Payables & supplier payments (Phase 7, Stages 7-8).
	r.With(s.requirePermissionHeld("payable.read")).Group(func(r chi.Router) {
		r.Get("/payables", s.handleListPayables)
		r.Get("/ap-aging", s.handleAPaging)
		r.Get("/supplier-payments", s.handleListSupplierPayments)
	})
	r.With(s.requirePermission("payable.manage", nil)).
		Post("/payables/import", s.handleImportPayables)
	r.With(s.requirePermission("supplier_payment.manage", nil)).
		Post("/supplier-payments", s.handleRecordSupplierPayment)

	// Cash & banking (Phase 7, Stages 4-6). Reads ride
	// finance.read; writes gate on the cash/bank permissions.
	r.With(s.requirePermissionHeld("finance.read")).Group(func(r chi.Router) {
		r.Get("/stations/{stationID}/cash-reconciliations", s.handleListCashReconciliations)
		r.Get("/cash-reconciliations/{id}", s.handleGetCashReconciliation)
		r.Get("/bank-accounts", s.handleListBankAccounts)
		r.Get("/bank-deposits", s.handleListBankDeposits)
		r.Get("/bank-statement-lines", s.handleListBankStatementLines)
	})
	r.With(s.requirePermission("cash_reconciliation.manage", nil)).Group(func(r chi.Router) {
		r.Post("/stations/{stationID}/cash-reconciliations", s.handleCreateCashReconciliation)
		r.Post("/cash-reconciliations/{id}/submit", s.handleSubmitCashReconciliation)
	})
	r.With(s.requirePermission("cash_reconciliation.approve", nil)).
		Post("/cash-reconciliations/{id}/approve", s.handleApproveCashReconciliation)
	r.With(s.requirePermission("bank_account.manage", nil)).
		Post("/bank-accounts", s.handleCreateBankAccount)
	r.With(s.requirePermission("bank_deposit.manage", nil)).Group(func(r chi.Router) {
		r.Post("/bank-deposits", s.handleCreateBankDeposit)
		r.Post("/bank-deposits/{id}/prepare", s.handlePrepareBankDeposit)
	})
	r.With(s.requirePermission("bank_deposit.confirm", nil)).
		Post("/bank-deposits/{id}/confirm", s.handleConfirmBankDeposit)
	r.With(s.requirePermission("bank_statement.manage", nil)).Group(func(r chi.Router) {
		r.Post("/bank-statements/import", s.handleImportBankStatement)
		r.Post("/bank-statement-lines/{id}/match", s.handleMatchBankStatementLine)
		r.Post("/bank-statement-lines/{id}/unmatch", s.handleUnmatchBankStatementLine)
		r.Post("/bank-statement-lines/{id}/bank-fee", s.handleBankFeeStatementLine)
	})

	// Customer invoices & payments (Phase 7, Stages 9-10).
	r.With(s.requirePermissionHeld("finance.read")).Group(func(r chi.Router) {
		r.Get("/customer-invoices", s.handleListCustomerInvoices)
		r.Get("/customer-invoices/{id}", s.handleGetCustomerInvoice)
		r.Get("/customer-invoices-aging", s.handleCustomerInvoiceAging)
		r.Get("/customer-payments", s.handleListCustomerPayments)
	})
	r.With(s.requirePermission("customer_invoice.manage", nil)).
		Post("/customer-invoices", s.handleCreateCustomerInvoice)
	r.With(s.requirePermission("customer_invoice.issue", nil)).
		Post("/customer-invoices/{id}/issue", s.handleIssueCustomerInvoice)
	r.With(s.requirePermission("customer_payment.manage", nil)).
		Post("/customer-payments", s.handlePostCustomerPayment)

	// Expenses & petty cash (Phase 7, Stages 11-12).
	r.With(s.requirePermissionHeld("finance.read")).Group(func(r chi.Router) {
		r.Get("/expense-categories", s.handleListExpenseCategories)
		r.Get("/expenses", s.handleListExpenses)
		r.Get("/expenses/{id}", s.handleGetExpense)
		r.Get("/petty-cash-floats", s.handleListPettyCashFloats)
		r.Get("/petty-cash-floats/{id}", s.handleGetPettyCashFloat)
		r.Get("/petty-cash-floats/{id}/transactions", s.handleListPettyCashTransactions)
	})
	r.With(s.requirePermission("expense.manage", nil)).Group(func(r chi.Router) {
		r.Post("/expense-categories", s.handleCreateExpenseCategory)
		r.Post("/expenses", s.handleCreateExpense)
		r.Post("/expenses/{id}/submit", s.handleSubmitExpense)
	})
	r.With(s.requirePermission("expense.approve", nil)).
		Post("/expenses/{id}/approve", s.handleApproveExpense)
	r.With(s.requirePermission("expense.post", nil)).
		Post("/expenses/{id}/post", s.handlePostExpense)
	r.With(s.requirePermission("petty_cash.manage", nil)).Group(func(r chi.Router) {
		r.Post("/petty-cash-floats", s.handleCreatePettyCashFloat)
		r.Post("/petty-cash-floats/{id}/transactions", s.handlePettyCashTransaction)
	})
	r.With(s.requirePermission("petty_cash.reconcile", nil)).
		Post("/petty-cash-floats/{id}/reconcile", s.handleReconcilePettyCash)

	// Finance reports + dashboard (Phase 7, Stages 13, 15) —
	// read-only over posted journal lines, gated by finance.read.
	r.With(s.requirePermissionHeld("finance.read")).Group(func(r chi.Router) {
		r.Get("/finance/overview", s.handleFinanceOverview)
		r.Get("/finance/reports/trial-balance", s.handleTrialBalance)
		r.Get("/finance/reports/profit-loss", s.handleIncomeStatement)
		r.Get("/finance/reports/balance-sheet", s.handleBalanceSheet)
		r.Get("/finance/reports/general-ledger", s.handleGeneralLedger)
		r.Get("/finance/close-checklist", s.handleCloseChecklist)
		r.Get("/finance/exports", s.handleListExports)
	})

	// Accounting exports (Phase 7, Stage 14) — sensitive, audited.
	r.With(s.requirePermission("finance.export", nil)).
		Post("/finance/exports/{type}", s.handleGenerateExport)
}

// registerOperationsRoutes (Phase 3): pump calibration & status lifecycle,
// incidents, operating days, shifts, meter/dip readings, close & approval.
func (s *Server) registerOperationsRoutes(r chi.Router) {
	// Pump calibration events + status lifecycle. Reads ride
	// station.read; calibration is station-scoped
	// (pumps.calibrate), status changes fold into pumps.manage
	// / tanks.manage — all authorized in-handler.
	r.With(s.requirePermissionHeld("station.read")).
		Get("/pumps/{id}/calibrations", s.handleListPumpCalibrations)
	r.With(s.requirePermissionHeld("pumps.calibrate")).Post("/pumps/{id}/calibrations", s.handleCreatePumpCalibration)
	r.With(s.requirePermissionHeld("pumps.manage")).Patch("/pumps/{id}/status", s.handleUpdatePumpStatus)
	r.With(s.requirePermissionHeld("tanks.manage")).Patch("/tanks/{id}/status", s.handleUpdateTankStatus)

	// Incidents queue. Reads ride station.read; writes are
	// station-scoped (incidents.manage), authorized in-handler.
	r.With(s.requirePermissionHeld("station.read")).
		Get("/incidents", s.handleListIncidents)
	r.With(s.requirePermissionHeld("incidents.manage")).Post("/incidents", s.handleCreateIncident)
	r.With(s.requirePermissionHeld("incidents.manage")).Patch("/incidents/{id}/status", s.handleUpdateIncidentStatus)

	// Operating days (Phase 3, Stage 1). Open/list are
	// station-nested and gated by the URL station; close/lock
	// are id-based and authorized in-handler against the day's
	// station (operations.manage_day).
	r.With(s.requirePermission("station.read", stationFromURLParam("stationID"))).
		Get("/stations/{stationID}/operating-days", s.handleListOperatingDays)
	r.With(s.requirePermission("operations.manage_day", stationFromURLParam("stationID"))).
		Post("/stations/{stationID}/operating-days", s.handleOpenOperatingDay)
	r.Get("/operating-days/{id}", s.handleGetOperatingDay)
	r.With(s.requirePermissionHeld("operations.manage_day")).Patch("/operating-days/{id}/status", s.handleUpdateOperatingDayStatus)
	r.With(s.requirePermissionHeld("operations.manage_day")).Patch("/operating-days/{id}/lock", s.handleLockOperatingDay)

	// Shifts (Phase 3, Stage 2). Open/list are station-nested
	// (shift.open / station.read via the URL); get/close and the
	// assignment routes are id-based and authorized in-handler.
	r.With(s.requirePermission("station.read", stationFromURLParam("stationID"))).
		Get("/stations/{stationID}/shifts", s.handleListShifts)
	r.With(s.requirePermission("shift.open", stationFromURLParam("stationID"))).
		Post("/stations/{stationID}/shifts", s.handleOpenShift)
	r.Get("/shifts/{id}", s.handleGetShift)
	r.With(s.requirePermissionHeld("shift.assign")).Post("/shifts/{id}/attendants", s.handleAssignAttendant)
	r.With(s.requirePermissionHeld("shift.assign")).Delete("/shifts/{id}/attendants/{userID}", s.handleUnassignAttendant)
	r.With(s.requirePermissionHeld("shift.assign")).Post("/shifts/{id}/nozzle-assignments", s.handleAssignNozzle)
	r.With(s.requirePermissionHeld("shift.assign")).Delete("/shifts/{id}/nozzle-assignments/{assignmentID}", s.handleUnassignNozzle)

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
	r.With(s.requirePermissionHeld("shift.close")).Post("/shifts/{id}/close", s.handleCloseShift)
	r.Get("/shifts/{id}/close-summary", s.handleCloseSummary)
	r.Post("/shifts/{id}/cash-submission", s.handleSubmitCash)

	// Approval & exceptions (Phase 3, Stage 6). Day lock
	// (all-shifts-approved guard) already lives on the
	// operating-day routes above.
	r.With(s.requirePermissionHeld("shift.approve")).Patch("/shifts/{id}/status", s.handleApproveShift)
	r.Get("/shifts/{id}/exceptions", s.handleListShiftExceptions)
	r.With(s.requirePermissionHeld("shift.approve")).Patch("/shift-exceptions/{id}/status", s.handleResolveShiftException)
}

// registerUserAdminRoutes: user & role administration.
func (s *Server) registerUserAdminRoutes(r chi.Router) {
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
}
