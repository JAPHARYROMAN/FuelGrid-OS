package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRegions_DuplicateNameReturnsConflict(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()

	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	var companyID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		SELECT id FROM companies WHERE tenant_id = $1 ORDER BY created_at LIMIT 1
	`, h.ids.tenantID).Scan(&companyID); err != nil {
		t.Fatalf("lookup company: %v", err)
	}

	name := fmt.Sprintf("Conflict Region %d", time.Now().UnixNano())
	create := map[string]any{"company_id": companyID, "name": name}
	code, first := h.invPostJSON(t, "/api/v1/regions", admin, create)
	if code != http.StatusCreated {
		t.Fatalf("first create = %d, body=%v", code, first)
	}

	create["name"] = name
	code, duplicate := h.invPostJSON(t, "/api/v1/regions", admin, create)
	if code != http.StatusConflict {
		t.Fatalf("duplicate create = %d, body=%v", code, duplicate)
	}
	if duplicate["error"] != "a region with that name already exists for this company" {
		t.Fatalf("duplicate error = %v", duplicate["error"])
	}

	secondName := name + " Secondary"
	code, second := h.invPostJSON(t, "/api/v1/regions", admin, map[string]any{
		"company_id": companyID,
		"name":       secondName,
	})
	if code != http.StatusCreated {
		t.Fatalf("second create = %d, body=%v", code, second)
	}
	secondID, _ := second["id"].(string)
	if secondID == "" {
		t.Fatalf("second region id missing: %v", second)
	}

	raw, _ := json.Marshal(map[string]any{"name": name})
	code, response := h.do(t, http.MethodPatch, "/api/v1/regions/"+secondID, admin, bytes.NewReader(raw), "application/json")
	var updateConflict map[string]any
	_ = json.Unmarshal(response, &updateConflict)
	if code != http.StatusConflict {
		t.Fatalf("duplicate update = %d, body=%v", code, updateConflict)
	}
	if updateConflict["error"] != "a region with that name already exists for this company" {
		t.Fatalf("update error = %v", updateConflict["error"])
	}
}
