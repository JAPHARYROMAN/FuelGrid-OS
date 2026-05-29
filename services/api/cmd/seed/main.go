// Command seed inserts a small demo dataset into a fresh FuelGrid OS
// database: one tenant, one company, one region, two stations, and one
// demo user with the station_manager role scoped to the first station.
//
// The second station deliberately has NO user_station_access row for the
// demo user — it's the "this should 403" probe used by CI smoke tests.
//
// Idempotent: re-running it does nothing if the demo slug already exists.
//
// Usage:
//
//	DATABASE_URL=postgres://... go run ./services/api/cmd/seed
//
// Environment overrides (all optional):
//
//	DEMO_TENANT_SLUG    default "demo"
//	DEMO_USER_EMAIL     default "demo@fuelgrid.local"
//	DEMO_USER_PASSWORD  default "fuelgrid-demo-password-1234"
//	DEMO_ROLE_CODE      default "station_manager"
//	AUTH_PASSWORD_PEPPER must match the API to allow logins
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
)

const (
	defaultTenantSlug = "demo"
	defaultUserEmail  = "demo@fuelgrid.local"
	// defaultUserPassword is a dev-only convenience for `make seed`. Override
	// in any non-development environment via DEMO_USER_PASSWORD.
	defaultUserPassword = "fuelgrid-demo-password-1234" //nolint:gosec // G101: development-only default, override in prod
	defaultRoleCode     = "station_manager"

	defaultAdminEmail = "admin@fuelgrid.local"
	// Same dev-only convenience for the admin seed.
	defaultAdminPassword = "fuelgrid-admin-password-1234" //nolint:gosec // G101: development-only default, override in prod
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		logger.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}

	tenantSlug := envOr("DEMO_TENANT_SLUG", defaultTenantSlug)
	userEmail := envOr("DEMO_USER_EMAIL", defaultUserEmail)
	userPassword := envOr("DEMO_USER_PASSWORD", defaultUserPassword)
	roleCode := envOr("DEMO_ROLE_CODE", defaultRoleCode)
	adminEmail := envOr("DEMO_ADMIN_EMAIL", defaultAdminEmail)
	adminPassword := envOr("DEMO_ADMIN_PASSWORD", defaultAdminPassword)
	pepper := os.Getenv("AUTH_PASSWORD_PEPPER")

	// Backdoor guard: outside development, refuse to provision accounts with
	// the well-known default passwords. Seeding demo data into production is
	// discouraged; if you must, supply explicit secrets via DEMO_USER_PASSWORD
	// and DEMO_ADMIN_PASSWORD so no known-password account is ever created.
	if envOr("NODE_ENV", "development") != "development" {
		if userPassword == defaultUserPassword || adminPassword == defaultAdminPassword {
			return errors.New("refusing to seed default demo passwords outside development: set DEMO_USER_PASSWORD and DEMO_ADMIN_PASSWORD to non-default values (and prefer not seeding demo data in production at all)")
		}
	}

	hasher := password.New(password.DefaultParams, pepper)
	passwordHash, err := hasher.Hash(userPassword)
	if err != nil {
		return err
	}
	adminHash, err := hasher.Hash(adminPassword)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := database.Connect(ctx, database.Config{URL: url})
	if err != nil {
		return err
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var existingTenantID string
	err = tx.QueryRow(ctx,
		`SELECT id FROM tenants WHERE slug = $1`,
		tenantSlug,
	).Scan(&existingTenantID)
	if err == nil {
		slog.Info("demo tenant already present, skipping", "tenant_id", existingTenantID)
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	var tenantID, companyID, regionID, station1ID, station2ID, userID, roleID string
	var adminUserID, adminRoleID, supplierID, purchaseOrderID string

	if err := tx.QueryRow(ctx, `
		INSERT INTO tenants (name, slug)
		VALUES ('FuelGrid Demo Co.', $1)
		RETURNING id
	`, tenantSlug).Scan(&tenantID); err != nil {
		return err
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO companies (tenant_id, name, legal_name, currency, timezone)
		VALUES ($1, 'Demo Petroleum Ltd', 'Demo Petroleum Limited', 'USD', 'Africa/Dar_es_Salaam')
		RETURNING id
	`, tenantID).Scan(&companyID); err != nil {
		return err
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO regions (tenant_id, company_id, name, code)
		VALUES ($1, $2, 'Dar es Salaam', 'DAR')
		RETURNING id
	`, tenantID, companyID).Scan(&regionID); err != nil {
		return err
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO stations (tenant_id, company_id, region_id, name, code,
		                     city, country, latitude, longitude, timezone)
		VALUES ($1, $2, $3, 'Mikocheni Service Station', 'MIK-01',
		        'Dar es Salaam', 'Tanzania', -6.7700000, 39.2400000, 'Africa/Dar_es_Salaam')
		RETURNING id
	`, tenantID, companyID, regionID).Scan(&station1ID); err != nil {
		return err
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO stations (tenant_id, company_id, region_id, name, code,
		                     city, country, latitude, longitude, timezone)
		VALUES ($1, $2, $3, 'Mlimani Service Station', 'MSA-01',
		        'Dar es Salaam', 'Tanzania', -6.7740000, 39.2330000, 'Africa/Dar_es_Salaam')
		RETURNING id
	`, tenantID, companyID, regionID).Scan(&station2ID); err != nil {
		return err
	}

	// Product catalogue. Colours reuse the --color-fuel-* tokens from
	// packages/config/tailwind.preset.css.
	if _, err := tx.Exec(ctx, `
		INSERT INTO products
		    (tenant_id, code, name, category, unit, default_price, tax_rate,
		     density_kg_m3, loss_tolerance_percent, color)
		VALUES
		    ($1, 'PMS', 'Premium Motor Spirit', 'fuel', 'litre', 2950.00, 18.00, 740.000, 0.50, '#f97316'),
		    ($1, 'AGO', 'Automotive Gas Oil (Diesel)', 'fuel', 'litre', 2820.00, 18.00, 832.000, 0.50, '#2563eb'),
		    ($1, 'KERO', 'Kerosene', 'fuel', 'litre', 2480.00, 18.00, 800.000, 0.50, '#a855f7')
	`, tenantID); err != nil {
		return err
	}

	// Tanks. Two at MIK-01 (PMS + AGO, 30,000L each), one at MSA-01
	// (PMS, 25,000L). Products are resolved by code within the tenant.
	if _, err := tx.Exec(ctx, `
		INSERT INTO tanks
		    (tenant_id, station_id, product_id, name, code,
		     capacity_litres, safe_min_litres, safe_max_litres, dead_stock_litres)
		VALUES
		    ($1, $2, (SELECT id FROM products WHERE tenant_id = $1 AND code = 'PMS'),
		        'PMS Tank 1', 'T1', 30000.000, 3000.000, 28500.000, 500.000),
		    ($1, $2, (SELECT id FROM products WHERE tenant_id = $1 AND code = 'AGO'),
		        'AGO Tank 1', 'T2', 30000.000, 3000.000, 28500.000, 500.000),
		    ($1, $3, (SELECT id FROM products WHERE tenant_id = $1 AND code = 'PMS'),
		        'PMS Tank 1', 'T1', 25000.000, 2500.000, 23750.000, 500.000)
	`, tenantID, station1ID, station2ID); err != nil {
		return err
	}

	// Two pumps at MIK-01. Pump 1 draws PMS (tank T1), pump 2 draws AGO
	// (tank T2). Each gets two nozzles; default price comes from the
	// product. station_id + product_id are derived from the tank so the
	// composite-FK invariants in 0011 hold.
	var pump1ID, pump2ID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO pumps (tenant_id, station_id, number, name)
		VALUES ($1, $2, 1, 'Pump 1') RETURNING id
	`, tenantID, station1ID).Scan(&pump1ID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO pumps (tenant_id, station_id, number, name)
		VALUES ($1, $2, 2, 'Pump 2') RETURNING id
	`, tenantID, station1ID).Scan(&pump2ID); err != nil {
		return err
	}
	for _, np := range []struct {
		pumpID   string
		tankCode string
	}{{pump1ID, "T1"}, {pump2ID, "T2"}} {
		if _, err := tx.Exec(ctx, `
			INSERT INTO nozzles
			    (tenant_id, station_id, pump_id, tank_id, product_id, number, default_price)
			SELECT $1, $2, $3, t.id, t.product_id, g.num, p.default_price
			FROM tanks t
			JOIN products p ON p.id = t.product_id
			CROSS JOIN (VALUES (1), (2)) AS g(num)
			WHERE t.tenant_id = $1 AND t.station_id = $2 AND t.code = $4
		`, tenantID, station1ID, np.pumpID, np.tankCode); err != nil {
			return err
		}
	}

	// Calibration chart for MIK-01's PMS tank (T1): dip 0..3000mm in 60mm
	// steps (51 points), a simple linear strap (volume = dip * 10) so the
	// 30,000L tank reads full at 3000mm. The Phase-3 dip handler will call
	// the calibrated-volume endpoint backed by this chart.
	if _, err := tx.Exec(ctx, `
		WITH ch AS (
			INSERT INTO tank_calibration_charts (tenant_id, tank_id, name, source)
			SELECT $1, t.id, 'Initial strapping', 'seed'
			FROM tanks t
			WHERE t.tenant_id = $1 AND t.station_id = $2 AND t.code = 'T1'
			RETURNING id
		)
		INSERT INTO tank_calibration_entries (chart_id, dip_mm, volume_litres)
		SELECT ch.id, g.dip, g.dip * 10
		FROM ch CROSS JOIN generate_series(0, 3000, 60) AS g(dip)
	`, tenantID, station1ID); err != nil {
		return err
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO users (tenant_id, email, full_name, status,
		                  password_hash, password_changed_at)
		VALUES ($1, $2, 'Demo Operator', 'active', $3, now())
		RETURNING id
	`, tenantID, userEmail, passwordHash).Scan(&userID); err != nil {
		return err
	}

	// Grant the system role by code. System roles have tenant_id IS NULL.
	if err := tx.QueryRow(ctx, `
		SELECT id FROM roles WHERE code = $1 AND is_system = true
	`, roleCode).Scan(&roleID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id, tenant_id)
		VALUES ($1, $2, $3)
	`, userID, roleID, tenantID); err != nil {
		return err
	}

	// Scope: explicit access to station 1 only. Station 2 is the
	// "forbidden" probe target.
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_station_access (user_id, station_id, tenant_id)
		VALUES ($1, $2, $3)
	`, userID, station1ID, tenantID); err != nil {
		return err
	}

	// Admin user — has the system_admin role (all permissions) and no
	// user_station_access rows (tenant-wide reach). Stage 7's CI smoke
	// uses this account to exercise the admin grant-role endpoint.
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (tenant_id, email, full_name, status,
		                  password_hash, password_changed_at)
		VALUES ($1, $2, 'Demo Admin', 'active', $3, now())
		RETURNING id
	`, tenantID, adminEmail, adminHash).Scan(&adminUserID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `
		SELECT id FROM roles WHERE code = 'system_admin' AND is_system = true
	`).Scan(&adminRoleID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id, tenant_id)
		VALUES ($1, $2, $3)
	`, adminUserID, adminRoleID, tenantID); err != nil {
		return err
	}

	// Phase 5 procurement seed: one active supplier with product coverage and
	// one confirmed PMS purchase order for MIK-01, ready for receiving.
	if err := tx.QueryRow(ctx, `
		INSERT INTO suppliers
		    (tenant_id, code, name, contact_name, contact_email, contact_phone, payment_terms_days)
		VALUES ($1, 'ORYX', 'Oryx Energies', 'Depot Desk', 'supply@oryx.local', '+255700000000', 14)
		RETURNING id
	`, tenantID).Scan(&supplierID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO supplier_products (tenant_id, supplier_id, product_id)
		SELECT $1, $2, p.id
		FROM products p
		WHERE p.tenant_id = $1 AND p.code IN ('PMS', 'AGO')
	`, tenantID, supplierID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO purchase_orders
		    (tenant_id, station_id, supplier_id, expected_delivery_date,
		     status, raised_by, submitted_by, submitted_at, confirmed_by, confirmed_at, notes)
		VALUES ($1, $2, $3, CURRENT_DATE + INTERVAL '1 day',
		        'confirmed', $4, $4, now(), $4, now(), 'Seeded PMS replenishment')
		RETURNING id
	`, tenantID, station1ID, supplierID, adminUserID).Scan(&purchaseOrderID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO purchase_order_lines
		    (tenant_id, purchase_order_id, product_id, ordered_litres, unit_price)
		SELECT $1, $2, p.id, 10000.000, 2500.00
		FROM products p
		WHERE p.tenant_id = $1 AND p.code = 'PMS'
	`, tenantID, purchaseOrderID); err != nil {
		return err
	}

	// An open operating day for MIK-01 on today's date, so Phase-3 shift
	// flows have a day to hang off out of the box.
	var operatingDayID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO operating_days (tenant_id, station_id, business_date, opened_by)
		VALUES ($1, $2, CURRENT_DATE, $3) RETURNING id
	`, tenantID, station1ID, adminUserID).Scan(&operatingDayID); err != nil {
		return err
	}

	// An open shift in that day with the demo operator assigned to MIK-01's
	// two PMS nozzles (pump 1).
	var shiftID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO shifts (tenant_id, station_id, operating_day_id, name, opened_by)
		VALUES ($1, $2, $3, 'Morning', $4) RETURNING id
	`, tenantID, station1ID, operatingDayID, adminUserID).Scan(&shiftID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO shift_attendants (shift_id, user_id, tenant_id, assigned_by)
		VALUES ($1, $2, $3, $4)
	`, shiftID, userID, tenantID, adminUserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO shift_nozzle_assignments (tenant_id, station_id, shift_id, nozzle_id, attendant_id, assigned_by)
		SELECT $1, $6, $2, n.id, $3, $4
		FROM nozzles n
		WHERE n.tenant_id = $1 AND n.pump_id = $5
	`, tenantID, shiftID, userID, adminUserID, pump1ID, station1ID); err != nil {
		return err
	}

	// Opening meter readings for those assigned nozzles, so Stage-3 flows
	// have an opening to close against.
	if _, err := tx.Exec(ctx, `
		INSERT INTO meter_readings (tenant_id, shift_id, nozzle_id, reading_type, reading, recorded_by)
		SELECT $1, $2, n.id, 'opening', 10000.000, $3
		FROM nozzles n
		WHERE n.tenant_id = $1 AND n.pump_id = $4
	`, tenantID, shiftID, userID, pump1ID); err != nil {
		return err
	}

	// Opening tank dip for MIK-01's PMS tank: 1240mm resolves to 12400L on
	// the seeded linear chart (volume = dip*10). Snapshots the active chart.
	if _, err := tx.Exec(ctx, `
		INSERT INTO tank_dip_readings
		    (tenant_id, shift_id, tank_id, reading_type, dip_mm, volume_litres, chart_id, recorded_by)
		SELECT $1, $2, t.id, 'opening', 1240.000, 12400.000, c.id, $3
		FROM tanks t
		JOIN tank_calibration_charts c ON c.tank_id = t.id AND c.status = 'active'
		WHERE t.tenant_id = $1 AND t.station_id = $4 AND t.code = 'T1'
	`, tenantID, shiftID, userID, station1ID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	slog.Info("seeded demo data",
		"tenant_id", tenantID,
		"tenant_slug", tenantSlug,
		"company_id", companyID,
		"region_id", regionID,
		"station1_id", station1ID,
		"station1_code", "MIK-01",
		"station2_id", station2ID,
		"station2_code", "MSA-01",
		"products", "PMS, AGO, KERO",
		"supplier_id", supplierID,
		"purchase_order_id", purchaseOrderID,
		"tanks", "MIK-01: T1(PMS), T2(AGO); MSA-01: T1(PMS)",
		"pumps", "MIK-01: Pump 1 (2x PMS), Pump 2 (2x AGO)",
		"calibration", "MIK-01 PMS tank: 51-point chart (0..3000mm)",
		"operating_day", "MIK-01: open for today",
		"shift", "MIK-01: Morning (operator on 2 PMS nozzles)",
		"meter_readings", "MIK-01 Morning: opening 10000 on 2 nozzles",
		"dip_reading", "MIK-01 Morning: PMS tank opening 1240mm -> 12400L",
		"user_id", userID,
		"user_email", userEmail,
		"role_code", roleCode,
		"access_scope", "station MIK-01 only",
		"admin_user_id", adminUserID,
		"admin_email", adminEmail,
		"admin_role", "system_admin",
	)
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
