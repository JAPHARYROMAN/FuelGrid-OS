package server_test

// DB-backed integration test for the supplier-invoice listing endpoint
// (GET /api/v1/supplier-invoices) added for feature 7.3. It drives the real
// supplier -> PO -> receipt -> invoice flow, then exercises the list with its
// purchase_order_id / supplier_id / status filters and confirms each item is
// hydrated with its lines.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestSupplierInvoices_List(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()

	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)
	suffix := time.Now().UnixNano()

	code, supplier := h.invPostJSON(t, "/api/v1/suppliers", admin, map[string]any{
		"code":               fmt.Sprintf("SUP-%d", suffix),
		"name":               "List Supplier",
		"payment_terms_days": 14,
		"product_ids":        []string{h.ids.agoProduct.String()},
	})
	if code != http.StatusCreated {
		t.Fatalf("create supplier: %d %v", code, supplier)
	}
	supplierID := supplier["id"].(string)

	code, po := h.invPostJSON(t, "/api/v1/purchase-orders", admin, map[string]any{
		"station_id":  h.ids.station1.String(),
		"supplier_id": supplierID,
		"lines": []map[string]any{{
			"product_id":     h.ids.agoProduct.String(),
			"ordered_litres": 5000,
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

	if code, _ := h.invPostJSON(t, "/api/v1/tanks/"+h.ids.tankAGO.String()+"/opening-balance", admin, map[string]any{"litres": 8000}); code != http.StatusCreated {
		t.Fatalf("opening balance: %d", code)
	}
	code, receipt := h.invPostJSON(t, "/api/v1/purchase-orders/"+poID+"/receipts", admin, map[string]any{
		"tank_id":       h.ids.tankAGO.String(),
		"po_line_id":    lineID,
		"volume_litres": 5000,
	})
	if code != http.StatusCreated {
		t.Fatalf("receive: %d %v", code, receipt)
	}
	deliveryID := receipt["delivery"].(map[string]any)["id"].(string)

	// A cleanly matching invoice (invoiced == received == ordered, price matches).
	invoiceNumber := fmt.Sprintf("INV-%d", suffix)
	code, inv := h.invPostJSON(t, "/api/v1/supplier-invoices", admin, map[string]any{
		"purchase_order_id": poID,
		"invoice_number":    invoiceNumber,
		"lines": []map[string]any{{
			"po_line_id":      lineID,
			"delivery_id":     deliveryID,
			"invoiced_litres": 5000,
			"unit_price":      "2500.00",
		}},
	})
	if code != http.StatusCreated {
		t.Fatalf("record invoice: %d %v", code, inv)
	}
	if inv["status"] != "matched" {
		t.Fatalf("invoice should match, got %v", inv["status"])
	}
	invoiceID := inv["id"].(string)

	// Unfiltered list includes the invoice, hydrated with its line.
	code, list := h.getJSON(t, "/api/v1/supplier-invoices", admin)
	if code != http.StatusOK {
		t.Fatalf("list invoices: %d %v", code, list)
	}
	found := findInvoice(list, invoiceID)
	if found == nil {
		t.Fatalf("invoice %s not in unfiltered list: %v", invoiceID, list)
	}
	if found["invoice_number"] != invoiceNumber {
		t.Fatalf("listed invoice_number = %v, want %s", found["invoice_number"], invoiceNumber)
	}
	if found["total_amount"] != "12500000.00" {
		t.Fatalf("listed total_amount = %v, want 12500000.00", found["total_amount"])
	}
	lines, _ := found["lines"].([]any)
	if len(lines) != 1 {
		t.Fatalf("listed invoice should carry its 1 line, got %v", found["lines"])
	}

	// purchase_order_id filter narrows to this PO's invoices.
	code, byPO := h.getJSON(t, "/api/v1/supplier-invoices?purchase_order_id="+poID, admin)
	if code != http.StatusOK || findInvoice(byPO, invoiceID) == nil {
		t.Fatalf("filter by PO: %d %v", code, byPO)
	}

	// supplier_id filter narrows to this supplier's invoices.
	code, bySupplier := h.getJSON(t, "/api/v1/supplier-invoices?supplier_id="+supplierID, admin)
	if code != http.StatusOK || findInvoice(bySupplier, invoiceID) == nil {
		t.Fatalf("filter by supplier: %d %v", code, bySupplier)
	}

	// status=matched includes it; status=approved (it isn't yet) excludes it.
	code, byStatus := h.getJSON(t, "/api/v1/supplier-invoices?status=matched", admin)
	if code != http.StatusOK || findInvoice(byStatus, invoiceID) == nil {
		t.Fatalf("filter by status=matched: %d %v", code, byStatus)
	}
	code, byApproved := h.getJSON(t, "/api/v1/supplier-invoices?status=approved", admin)
	if code != http.StatusOK || findInvoice(byApproved, invoiceID) != nil {
		t.Fatalf("status=approved should exclude a matched invoice: %d %v", code, byApproved)
	}

	// A bogus purchase_order_id (not a UUID) is a 400, not a 500.
	if code, _ := h.getJSON(t, "/api/v1/supplier-invoices?purchase_order_id=not-a-uuid", admin); code != http.StatusBadRequest {
		t.Fatalf("invalid purchase_order_id: %d, want 400", code)
	}

	// A station outside the actor's scope filter is honoured (no rows leak).
	other := url.Values{"station_id": {h.ids.station2.String()}}.Encode()
	code, byOtherStation := h.getJSON(t, "/api/v1/supplier-invoices?"+other, admin)
	if code != http.StatusOK || findInvoice(byOtherStation, invoiceID) != nil {
		t.Fatalf("invoice should not appear under a different station filter: %d %v", code, byOtherStation)
	}
}

// findInvoice returns the listed invoice object with the given id, or nil.
func findInvoice(list map[string]any, id string) map[string]any {
	items, _ := list["items"].([]any)
	for _, raw := range items {
		m, ok := raw.(map[string]any)
		if ok && m["id"] == id {
			return m
		}
	}
	return nil
}
