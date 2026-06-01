package server

import (
	"strings"
	"testing"
	"time"

	"github.com/japharyroman/fuelgrid-os/internal/accounting"
)

func glRow(num int64, code, name, debit, credit, src string, memo *string) accounting.JournalExportRow {
	return accounting.JournalExportRow{
		EntryNumber: num,
		EntryDate:   time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		AccountCode: code, AccountName: name,
		Debit: debit, Credit: credit, SourceType: src, Status: "posted", Memo: memo,
	}
}

func sampleGLLines() []accounting.JournalExportRow {
	memo := "Daily revenue"
	return []accounting.JournalExportRow{
		glRow(101, "1000", "Cash", "1234.56", "0", "revenue", &memo),
		glRow(101, "4000", "Fuel sales", "0", "1234.56", "revenue", &memo),
	}
}

func TestBuildGLGenericCSV(t *testing.T) {
	body, err := buildGLGenericCSV(sampleGLLines())
	if err != nil {
		t.Fatalf("buildGLGenericCSV: %v", err)
	}
	got := string(body)
	if !strings.HasPrefix(got, "entry_number,entry_date,account_code,account_name,debit,credit,source_type,status,memo") {
		t.Fatalf("missing/incorrect header: %q", got)
	}
	// Decimal amounts are preserved verbatim (never reformatted as floats).
	if !strings.Contains(got, "1234.56") {
		t.Fatalf("expected exact decimal amount, got %q", got)
	}
	if !strings.Contains(got, "101,2026-03-15,1000,Cash,1234.56,0,revenue,posted,Daily revenue") {
		t.Fatalf("unexpected data row: %q", got)
	}
}

func TestBuildGLXeroCSV(t *testing.T) {
	body, err := buildGLXeroCSV(sampleGLLines())
	if err != nil {
		t.Fatalf("buildGLXeroCSV: %v", err)
	}
	got := string(body)
	if !strings.HasPrefix(got, "*Narration,*Date,*AccountCode,Description,Debit,Credit") {
		t.Fatalf("missing/incorrect Xero header: %q", got)
	}
	// Xero wants the amount in exactly one of debit/credit; the zero side blanks.
	if !strings.Contains(got, "Journal 101 (revenue),15/03/2026,1000,Daily revenue,1234.56,") {
		t.Fatalf("debit line wrong: %q", got)
	}
	if !strings.Contains(got, "Journal 101 (revenue),15/03/2026,4000,Daily revenue,,1234.56") {
		t.Fatalf("credit line wrong: %q", got)
	}
}

func TestBuildGLIIF(t *testing.T) {
	body := buildGLIIF(sampleGLLines())
	got := string(body)
	for _, want := range []string{
		"!TRNS\tTRNSTYPE\tDATE\tACCNT\tNAME\tAMOUNT\tMEMO",
		"!SPL\tTRNSTYPE\tDATE\tACCNT\tNAME\tAMOUNT\tMEMO",
		"!ENDTRNS",
		"ENDTRNS",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("IIF missing %q in:\n%s", want, got)
		}
	}
	// The entry's first line is a TRNS (debit positive); the second a SPL with
	// the credit negated — both exact decimal strings.
	if !strings.Contains(got, "TRNS\tGENERAL JOURNAL\t03/15/2026\tCash\t\t1234.56\tDaily revenue") {
		t.Fatalf("TRNS line wrong:\n%s", got)
	}
	if !strings.Contains(got, "SPL\tGENERAL JOURNAL\t03/15/2026\tFuel sales\t\t-1234.56\tDaily revenue") {
		t.Fatalf("SPL line wrong:\n%s", got)
	}
}

func TestIIFSignedAmount(t *testing.T) {
	cases := []struct{ debit, credit, want string }{
		{"100.00", "0", "100.00"},
		{"0", "250.50", "-250.50"},
		{"", "", "0"},
		{"0.00", "0.00", "0"},
	}
	for _, c := range cases {
		if got := iifSignedAmount(c.debit, c.credit); got != c.want {
			t.Errorf("iifSignedAmount(%q,%q) = %q, want %q", c.debit, c.credit, got, c.want)
		}
	}
}

func TestIIFCleanStripsFraming(t *testing.T) {
	if got := iifClean("a\tb\nc\rd"); got != "a b c d" {
		t.Errorf("iifClean = %q", got)
	}
}
