package server

import (
	"bytes"
	"testing"
)

// TestReportPDFRenders builds a document through every layout helper and asserts
// the output is a non-trivial, well-formed PDF (correct magic + EOF marker) with
// no error accumulated by fpdf. This guards the rendering path the two report
// handlers share without needing a database.
func TestReportPDFRenders(t *testing.T) {
	doc := newReportPDF(
		"Daily Shift & Close Report",
		"Tenant 00000000-0000-0000-0000-000000000000",
		"Station ST-1 — Main • Business date 2026-05-31", // em dash + bullet
	)
	doc.sectionHeading("Close summary")
	doc.keyValue("Gross revenue", "12345.67")
	doc.totalRow("Margin", "1234.56")
	doc.table(
		[]string{"Date", "Status", "Gross"},
		[]float64{40, 40, 40},
		[]string{"L", "L", "R"},
		[][]string{{"2026-05-31", "locked", "12345.67"}},
	)
	doc.note("All money figures are exact decimals — identical to the CSV.")

	out, err := doc.bytes()
	if err != nil {
		t.Fatalf("bytes(): %v", err)
	}
	if len(out) < 800 {
		t.Fatalf("PDF unexpectedly small: %d bytes", len(out))
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatalf("output is not a PDF (bad magic): %q", out[:min(8, len(out))])
	}
	if !bytes.Contains(out, []byte("%%EOF")) {
		t.Fatal("PDF missing EOF trailer")
	}
}
