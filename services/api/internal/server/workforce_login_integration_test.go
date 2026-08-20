package server_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestProvisionEmployeeLogin_CreatesScopedAttendantAccount(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	admin := h.login(t, slug(h), h.ids.adminEmail)

	var employeeID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO employees (tenant_id, station_id, full_name, role)
		VALUES ($1, $2, 'Roster Employee', 'pump_attendant')
		RETURNING id`, h.ids.tenantID, h.ids.station1,
	).Scan(&employeeID); err != nil {
		t.Fatalf("seed employee: %v", err)
	}
	email := fmt.Sprintf("roster-%d@example.com", time.Now().UnixNano())
	code, body := h.postJSON(t, "/api/v1/employees/"+employeeID.String()+"/login-account", admin,
		fmt.Sprintf(`{"email":%q}`, email))
	if code != http.StatusCreated {
		t.Fatalf("provision login: code=%d body=%v", code, body)
	}
	if body["email"] != email || body["status"] != "invited" || body["created"] != true {
		t.Fatalf("unexpected response: %v", body)
	}

	var userID uuid.UUID
	var userStatus, employeeEmail string
	if err := h.pool.QueryRow(ctx, `
		SELECT e.user_id, u.status, e.email
		FROM employees e
		JOIN users u ON u.tenant_id = e.tenant_id AND u.id = e.user_id
		WHERE e.tenant_id = $1 AND e.id = $2`, h.ids.tenantID, employeeID,
	).Scan(&userID, &userStatus, &employeeEmail); err != nil {
		t.Fatalf("load provisioned account: %v", err)
	}
	if userStatus != "invited" || employeeEmail != email {
		t.Fatalf("status/email = %s/%s", userStatus, employeeEmail)
	}

	var attendantRole, stationAccess bool
	if err := h.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id
			WHERE ur.tenant_id = $1 AND ur.user_id = $2 AND r.code = 'attendant'
		), EXISTS (
			SELECT 1 FROM user_station_access
			WHERE tenant_id = $1 AND user_id = $2 AND station_id = $3
		)`, h.ids.tenantID, userID, h.ids.station1,
	).Scan(&attendantRole, &stationAccess); err != nil {
		t.Fatalf("load account grants: %v", err)
	}
	if !attendantRole || !stationAccess {
		t.Fatalf("role/access = %v/%v, want true/true", attendantRole, stationAccess)
	}

	code, body = h.postJSON(t, "/api/v1/employees/"+employeeID.String()+"/login-account", admin,
		fmt.Sprintf(`{"email":%q}`, email))
	if code != http.StatusConflict {
		t.Fatalf("duplicate provision: code=%d body=%v, want 409", code, body)
	}
}

func TestProvisionEmployeeLogin_RejectsPrivilegedAccount(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	admin := h.login(t, slug(h), h.ids.adminEmail)

	var employeeID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO employees (tenant_id, station_id, full_name, role)
		VALUES ($1, $2, 'Dedicated Login Required', 'pump_attendant')
		RETURNING id`, h.ids.tenantID, h.ids.station1,
	).Scan(&employeeID); err != nil {
		t.Fatalf("seed employee: %v", err)
	}
	code, body := h.postJSON(t, "/api/v1/employees/"+employeeID.String()+"/login-account", admin,
		fmt.Sprintf(`{"email":%q}`, h.ids.adminEmail))
	if code != http.StatusConflict {
		t.Fatalf("privileged account link: code=%d body=%v, want 409", code, body)
	}

	var linked bool
	if err := h.pool.QueryRow(ctx,
		`SELECT user_id IS NOT NULL FROM employees WHERE tenant_id = $1 AND id = $2`,
		h.ids.tenantID, employeeID,
	).Scan(&linked); err != nil {
		t.Fatalf("load employee link: %v", err)
	}
	if linked {
		t.Fatal("privileged account was linked despite conflict")
	}
}
