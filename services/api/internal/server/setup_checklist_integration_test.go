package server_test

import (
	"context"
	"net/http"
	"testing"
)

func TestSetupChecklist_UpdateStepCompleted(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	code, body := h.patchJSON(t, "/api/v1/setup/checklist", admin, `{
		"step_code": "opening_stock",
		"status": "completed"
	}`)
	if code != http.StatusOK {
		t.Fatalf("review setup step: status %d: %v", code, body)
	}

	steps, _ := body["steps"].([]any)
	for _, raw := range steps {
		step, _ := raw.(map[string]any)
		if step["code"] == "opening_stock" {
			if step["status"] != "completed" {
				t.Fatalf("opening_stock status = %v, want completed", step["status"])
			}
			if step["completed_by"] == nil {
				t.Fatal("opening_stock completed_by is nil")
			}
			return
		}
	}
	t.Fatal("opening_stock step not found")
}
