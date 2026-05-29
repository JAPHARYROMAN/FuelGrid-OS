package server_test

// DB-backed integration tests for Phase 5 procurement. They reuse the shared
// Phase 2 harness and drive the real HTTP API through supplier -> PO ->
// receipt -> invoice match -> approval.

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestPhase5_ProcurementFlow(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()

	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)
	suffix := time.Now().UnixNano()

	// Supplier master.
	code, supplier := h.invPostJSON(t, "/api/v1/suppliers", admin, map[string]any{
		"code":               fmt.Sprintf("SUP-%d", suffix),
		"name":               "Phase 5 Supplier",
		"payment_terms_days": 14,
		"product_ids":        []string{h.ids.agoProduct.String()},
	})
	if code != http.StatusCreated {
		t.Fatalf("create supplier: %d %v", code, supplier)
	}
	supplierID := supplier["id"].(string)

	// Draft PO, submitted, confirmed. Lines are immutable after submission.
	code, po := h.invPostJSON(t, "/api/v1/purchase-orders", admin, map[string]any{
		"station_id":  h.ids.station1.String(),
		"supplier_id": supplierID,
		"lines": []map[string]any{{
			"product_id":     h.ids.agoProduct.String(),
			"ordered_litres": 10000,
			"unit_price":     "2500.00",
		}},
	})
	if code != http.StatusCreated {
		t.Fatalf("create PO: %d %v", code, po)
	}
	poID := po["id"].(string)
	lineID := po["lines"].([]any)[0].(map[string]any)["id"].(string)

	if code, _ := h.invPostJSON(t, "/api/v1/purchase-orders/"+poID+"/status", admin, map[string]any{"status": "submitted"}); code != http.StatusOK {
		t.Fatalf("submit PO: %d", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/purchase-orders/"+poID+"/status", admin, map[string]any{"status": "confirmed"}); code != http.StatusOK {
		t.Fatalf("confirm PO: %d", code)
	}
	if code, _ := h.patchJSON(t, "/api/v1/purchase-orders/"+poID, admin, `{"lines":[]}`); code != http.StatusBadRequest {
		t.Fatalf("empty line update after submit: %d, want 400", code)
	}
	if code, _ := h.do(t, http.MethodDelete, "/api/v1/suppliers/"+supplierID, admin, nil, ""); code != http.StatusConflict {
		t.Fatalf("deactivate supplier with open PO: %d, want 409", code)
	}

	// Receive 9,800L against a 10,000L line: stock moves by 9,800, PO becomes
	// partially_received, and the landed cost is snapshotted.
	tankPath := "/api/v1/tanks/" + h.ids.tankAGO.String()
	if code, _ := h.invPostJSON(t, tankPath+"/opening-balance", admin, map[string]any{"litres": 8000}); code != http.StatusCreated {
		t.Fatalf("opening balance: %d", code)
	}
	code, receipt := h.invPostJSON(t, "/api/v1/purchase-orders/"+poID+"/receipts", admin, map[string]any{
		"tank_id":        h.ids.tankAGO.String(),
		"po_line_id":     lineID,
		"volume_litres":  9800,
		"freight_amount": "150000.00",
	})
	if code != http.StatusCreated {
		t.Fatalf("receive 9800: %d %v", code, receipt)
	}
	if receipt["purchase_order_status"] != "partially_received" || !receipt["quantity_discrepancy"].(bool) {
		t.Fatalf("partial receipt flags = %v", receipt)
	}
	mv := receipt["movement"].(map[string]any)
	if mv["movement_type"] != "delivery" || mv["litres"].(float64) != 9800 {
		t.Fatalf("receipt movement = %v", mv)
	}
	del := receipt["delivery"].(map[string]any)
	if del["landed_cost_per_litre"] != "2515.3061" {
		t.Fatalf("landed cost per litre = %v, want 2515.3061", del["landed_cost_per_litre"])
	}

	// Invoice billing 10,000L while only 9,800L has arrived raises a blocking
	// discrepancy. Approval is refused until the discrepancy is resolved.
	code, inv := h.invPostJSON(t, "/api/v1/supplier-invoices", admin, map[string]any{
		"purchase_order_id": poID,
		"invoice_number":    fmt.Sprintf("INV-%d", suffix),
		"lines": []map[string]any{{
			"po_line_id":      lineID,
			"invoiced_litres": 10000,
			"unit_price":      "2500.00",
		}},
	})
	if code != http.StatusCreated {
		t.Fatalf("record invoice: %d %v", code, inv)
	}
	if inv["status"] != "discrepancy" || len(inv["discrepancies"].([]any)) != 1 {
		t.Fatalf("invoice match = %v", inv)
	}
	invoiceID := inv["id"].(string)
	discrepancyID := inv["discrepancies"].([]any)[0].(map[string]any)["id"].(string)
	if code, _ := h.invPostJSON(t, "/api/v1/supplier-invoices/"+invoiceID+"/approve", admin, map[string]any{}); code != http.StatusConflict {
		t.Fatalf("approve discrepant invoice: %d, want 409", code)
	}
	if code, _ := h.patchJSON(t, "/api/v1/procurement-discrepancies/"+discrepancyID+"/status", admin, `{"status":"resolved"}`); code != http.StatusOK {
		t.Fatalf("resolve discrepancy: %d", code)
	}
	if code, approved := h.invPostJSON(t, "/api/v1/supplier-invoices/"+invoiceID+"/approve", admin, map[string]any{}); code != http.StatusOK || approved["status"] != "approved" {
		t.Fatalf("approve resolved invoice: %d %v", code, approved)
	}
	var payableEvents int
	if err := h.pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		WHERE tenant_id = $1 AND event_type = 'PayableCreated' AND aggregate_id = $2
	`, h.ids.tenantID, invoiceID).Scan(&payableEvents); err != nil || payableEvents != 1 {
		t.Fatalf("payable event count=%d err=%v, want 1", payableEvents, err)
	}

	// Receiving the remaining 200L moves the PO to received.
	code, receipt = h.invPostJSON(t, "/api/v1/purchase-orders/"+poID+"/receipts", admin, map[string]any{
		"tank_id":       h.ids.tankAGO.String(),
		"po_line_id":    lineID,
		"volume_litres": 200,
	})
	if code != http.StatusCreated || receipt["purchase_order_status"] != "received" {
		t.Fatalf("receive balance: %d %v", code, receipt)
	}
}
