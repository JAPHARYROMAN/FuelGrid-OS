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
	_, slug, admin := h.adminContext(t, ctx)
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
	if mv["movement_type"] != "delivery" || mv["litres"].(string) != "9800.000" {
		t.Fatalf("receipt movement = %v", mv)
	}
	del := receipt["delivery"].(map[string]any)
	if del["landed_cost_per_litre"] != "2515.3061" {
		t.Fatalf("landed cost per litre = %v, want 2515.3061", del["landed_cost_per_litre"])
	}
	firstDeliveryID := del["id"].(string)

	// Invoice billing 10,000L while only 9,800L arrived on the attributed receipt
	// raises a blocking quantity discrepancy. The match is scoped to THIS
	// invoice's receipt (firstDeliveryID), not a cumulative sum of all PO
	// receipts. Approval is refused until the discrepancy is resolved.
	code, inv := h.invPostJSON(t, "/api/v1/supplier-invoices", admin, map[string]any{
		"purchase_order_id": poID,
		"invoice_number":    fmt.Sprintf("INV-%d", suffix),
		"lines": []map[string]any{{
			"po_line_id":      lineID,
			"delivery_id":     firstDeliveryID,
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
	// Separation of duties: the recorder (admin) cannot approve their own invoice.
	if code, _ := h.invPostJSON(t, "/api/v1/supplier-invoices/"+invoiceID+"/approve", admin, map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("self-approve invoice should be 403, got %d", code)
	}
	approver := h.secondApprover(t, ctx, slug)
	if code, approved := h.invPostJSON(t, "/api/v1/supplier-invoices/"+invoiceID+"/approve", approver, map[string]any{}); code != http.StatusOK || approved["status"] != "approved" {
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
	secondDeliveryID := receipt["delivery"].(map[string]any)["id"].(string)

	// PROC-19 regression: the quantity match is per-invoice, not a running sum
	// of every receipt on the PO. By now 10,000L total has been received
	// (9,800 + 200). A SECOND partial invoice attributed to the 200L receipt
	// must be compared against THAT receipt only, never the 10,000L cumulative.
	//
	// (a) A second partial invoice billing 200L against the 200L receipt matches
	//     cleanly — the earlier 9,800L delivery is not double-counted into it.
	code, inv2 := h.invPostJSON(t, "/api/v1/supplier-invoices", admin, map[string]any{
		"purchase_order_id": poID,
		"invoice_number":    fmt.Sprintf("INV2-%d", suffix),
		"lines": []map[string]any{{
			"po_line_id":      lineID,
			"delivery_id":     secondDeliveryID,
			"invoiced_litres": 200,
			"unit_price":      "2500.00",
		}},
	})
	if code != http.StatusCreated {
		t.Fatalf("record second invoice: %d %v", code, inv2)
	}
	if inv2["status"] != "matched" || len(inv2["discrepancies"].([]any)) != 0 {
		t.Fatalf("second partial invoice should match its own 200L receipt, got %v", inv2)
	}

	// (b) A third partial invoice billing 1,000L against the SAME 200L receipt
	//     flags a quantity discrepancy — proving the comparison is against that
	//     receipt's 200L, not the 10,000L cumulative (against which 1,000L would
	//     have spuriously matched under the old running-sum logic).
	code, inv3 := h.invPostJSON(t, "/api/v1/supplier-invoices", admin, map[string]any{
		"purchase_order_id": poID,
		"invoice_number":    fmt.Sprintf("INV3-%d", suffix),
		"lines": []map[string]any{{
			"po_line_id":      lineID,
			"delivery_id":     secondDeliveryID,
			"invoiced_litres": 1000,
			"unit_price":      "2500.00",
		}},
	})
	if code != http.StatusCreated {
		t.Fatalf("record third invoice: %d %v", code, inv3)
	}
	if inv3["status"] != "discrepancy" || len(inv3["discrepancies"].([]any)) != 1 {
		t.Fatalf("third invoice (1,000L vs 200L receipt) should flag, got %v", inv3)
	}
	d3 := inv3["discrepancies"].([]any)[0].(map[string]any)
	if d3["type"] != "quantity" || d3["variance_litres"].(float64) != 800 {
		t.Fatalf("third invoice variance should be +800L against its receipt, got %v", d3)
	}
}

// TestPhase5_ReceivingGuards proves PROC-07/13: a receipt cannot post stock past
// the ordered quantity, and a product's (large) loss tolerance is no longer
// reused as a receiving tolerance — so a real supplier shortfall leaves the PO
// partially_received instead of auto-completing as fully received.
func TestPhase5_ReceivingGuards(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)
	suffix := time.Now().UnixNano()

	// A deliberately large 10% product loss tolerance, which must NOT leak into
	// receiving acceptance.
	if _, err := h.pool.Exec(ctx, `UPDATE products SET loss_tolerance_percent = 10.0 WHERE id = $1`, h.ids.agoProduct); err != nil {
		t.Fatalf("set loss tolerance: %v", err)
	}

	code, supplier := h.invPostJSON(t, "/api/v1/suppliers", admin, map[string]any{
		"code": fmt.Sprintf("SUPG-%d", suffix), "name": "Guards Supplier",
		"payment_terms_days": 14, "product_ids": []string{h.ids.agoProduct.String()},
	})
	if code != http.StatusCreated {
		t.Fatalf("create supplier: %d %v", code, supplier)
	}
	code, po := h.invPostJSON(t, "/api/v1/purchase-orders", admin, map[string]any{
		"station_id": h.ids.station1.String(), "supplier_id": supplier["id"].(string),
		"lines": []map[string]any{{"product_id": h.ids.agoProduct.String(), "ordered_litres": 10000, "unit_price": "2500.00"}},
	})
	if code != http.StatusCreated {
		t.Fatalf("create PO: %d %v", code, po)
	}
	poID := po["id"].(string)
	lineID := po["lines"].([]any)[0].(map[string]any)["id"].(string)
	for _, st := range []string{"submitted", "confirmed"} {
		if code, _ := h.invPostJSON(t, "/api/v1/purchase-orders/"+poID+"/status", admin, map[string]any{"status": st}); code != http.StatusOK {
			t.Fatalf("PO %s: %d", st, code)
		}
	}
	if code, _ := h.invPostJSON(t, "/api/v1/tanks/"+h.ids.tankAGO.String()+"/opening-balance", admin, map[string]any{"litres": 5000}); code != http.StatusCreated {
		t.Fatalf("opening balance: %d", code)
	}

	// PROC-07: receiving 11,000 against a 10,000 line is refused — stock cannot
	// be posted past the order (beyond the receiving tolerance).
	if code, _ := h.invPostJSON(t, "/api/v1/purchase-orders/"+poID+"/receipts", admin, map[string]any{
		"tank_id": h.ids.tankAGO.String(), "po_line_id": lineID, "volume_litres": 11000,
	}); code != http.StatusUnprocessableEntity {
		t.Fatalf("over-receipt should be 422, got %d", code)
	}

	// PROC-13: a 1,000 L (10%) shortfall — within the product's 10% loss
	// tolerance but far beyond the 0.5% receiving tolerance — leaves the PO
	// partially_received, not auto-completed as received.
	code, receipt := h.invPostJSON(t, "/api/v1/purchase-orders/"+poID+"/receipts", admin, map[string]any{
		"tank_id": h.ids.tankAGO.String(), "po_line_id": lineID, "volume_litres": 9000,
	})
	if code != http.StatusCreated || receipt["purchase_order_status"] != "partially_received" {
		t.Fatalf("9,000 of 10,000 receipt: %d status=%v, want partially_received", code, receipt["purchase_order_status"])
	}

	// Receiving the remaining 1,000 completes the PO.
	if code, receipt := h.invPostJSON(t, "/api/v1/purchase-orders/"+poID+"/receipts", admin, map[string]any{
		"tank_id": h.ids.tankAGO.String(), "po_line_id": lineID, "volume_litres": 1000,
	}); code != http.StatusCreated || receipt["purchase_order_status"] != "received" {
		t.Fatalf("balance receipt: %d status=%v, want received", code, receipt["purchase_order_status"])
	}
}
