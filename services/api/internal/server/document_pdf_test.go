package server

import (
	"bytes"
	"fmt"
	"testing"
)

// TestListDocumentRenders exercises the reusable table-document framework
// directly (no DB): it builds a letterhead doc, draws a meta band, and renders a
// multi-page table via listTable, asserting a well-formed PDF with the header
// repeated across pages. This guards the rendering/pagination path every
// list-document handler — and every later-wave entity — reuses.
func TestListDocumentRenders(t *testing.T) {
	doc := newLetterheadDoc(LetterheadBranding{DisplayName: "Acme Fuels"}, LetterheadOptions{
		Title:    "Customers",
		SubLines: []string{"120 records"},
	})
	doc.metaBand([]DocumentMeta{{Label: "Status", Value: "active"}})

	cols := []DocumentColumn{
		{Header: "Code", Width: 24},
		{Header: "Name", Width: 56},
		{Header: "Contact", Width: 50},
		{Header: "Credit limit", Width: 30, Align: "R", Numeric: true}, // em dash safe
		{Header: "Status", Width: 20, Align: "C"},
	}
	// Enough rows to force a page break so the repeating-header path runs.
	rows := make([][]string, 0, 120)
	for i := 0; i < 120; i++ {
		rows = append(rows, []string{
			fmt.Sprintf("C-%03d", i), fmt.Sprintf("Customer %d — Ltd", i),
			"Jane Doe • +254700000000", "1234567.89", "active",
		})
	}
	doc.listTable(cols, rows, []string{"", "Total", "", "9999999.99", ""})

	out, err := doc.bytes()
	if err != nil {
		t.Fatalf("bytes(): %v", err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatalf("output is not a PDF (bad magic): %q", out[:min(8, len(out))])
	}
	if !bytes.Contains(out, []byte("%%EOF")) {
		t.Fatal("PDF missing EOF trailer")
	}
	// 120 rows + header at ~6mm each spill well past one A4 page.
	if pages := doc.pdf.PageNo(); pages < 2 {
		t.Fatalf("expected the long table to paginate (>=2 pages), got %d", pages)
	}
}
