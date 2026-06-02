package server

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"
)

// tinyPNG returns a small valid PNG so the letterhead's logo-embed path is
// exercised without a fixture file.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			img.Set(x, y, color.RGBA{R: 10, G: 80, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func assertValidPDF(t *testing.T, out []byte) {
	t.Helper()
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

// TestLetterheadDocRendersWithBranding builds a document through the shared
// letterhead helper (the deliverable other document PDFs reuse) plus the body
// helpers, with a full branding block, and asserts a valid non-empty PDF. The
// generation timestamp is injected so the render is deterministic.
func TestLetterheadDocRendersWithBranding(t *testing.T) {
	b := LetterheadBranding{
		DisplayName:    "Acme Fuels",
		LegalName:      "Acme Petroleum Holdings Ltd",
		TaxID:          "P051234567X",
		RegistrationNo: "C.12345",
		AddressLine1:   "1 Refinery Road",
		City:           "Nairobi",
		Country:        "Kenya",
		Phone:          "+254 700 000000",
		Email:          "ops@acme.example",
		Website:        "acme.example",
		FooterNote:     "Confidential",
	}
	doc := newLetterheadDoc(b, LetterheadOptions{
		Title:       "Daily Shift & Close Report",
		SubLines:    []string{"Station ST-1 — Main • Business date 2026-05-31"},
		GeneratedAt: time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC),
	})
	doc.sectionHeading("Close summary")
	doc.keyValue("Gross revenue", "12345.67")
	doc.totalRow("Margin", "1234.56")
	doc.note("All money figures are exact decimals — identical to the CSV.")

	out, err := doc.bytes()
	if err != nil {
		t.Fatalf("bytes(): %v", err)
	}
	assertValidPDF(t, out)
}

// TestLetterheadDocEmbedsLogo exercises the optional-logo branch: a PNG is
// registered and placed top-left. The document must still be a valid PDF and
// noticeably larger than the no-logo render (the embedded image adds bytes).
func TestLetterheadDocEmbedsLogo(t *testing.T) {
	logo := tinyPNG(t)
	withLogo := newLetterheadDoc(LetterheadBranding{
		DisplayName:     "Acme Fuels",
		Logo:            logo,
		LogoContentType: "image/png",
	}, LetterheadOptions{Title: "Doc", GeneratedAt: time.Unix(0, 0).UTC()})
	withLogoOut, err := withLogo.bytes()
	if err != nil {
		t.Fatalf("with-logo bytes(): %v", err)
	}
	assertValidPDF(t, withLogoOut)

	noLogo := newLetterheadDoc(LetterheadBranding{DisplayName: "Acme Fuels"},
		LetterheadOptions{Title: "Doc", GeneratedAt: time.Unix(0, 0).UTC()})
	noLogoOut, err := noLogo.bytes()
	if err != nil {
		t.Fatalf("no-logo bytes(): %v", err)
	}
	if len(withLogoOut) <= len(noLogoOut) {
		t.Fatalf("logo render (%d) should exceed no-logo render (%d)", len(withLogoOut), len(noLogoOut))
	}
}

// TestLetterheadDocEmptyBrandingFallsBack ensures a brand-new tenant with no
// branding configured still renders a valid PDF (the product name backstops the
// brand line), so the report PDFs keep working after the refactor.
func TestLetterheadDocEmptyBrandingFallsBack(t *testing.T) {
	doc := newLetterheadDoc(LetterheadBranding{}, LetterheadOptions{
		Title:       "Financial Statement",
		GeneratedAt: time.Unix(0, 0).UTC(),
	})
	doc.keyValue("Revenue", "0.00")
	out, err := doc.bytes()
	if err != nil {
		t.Fatalf("bytes(): %v", err)
	}
	assertValidPDF(t, out)
}

// TestFpdfImageType maps content types to fpdf's image-type token.
func TestFpdfImageType(t *testing.T) {
	cases := map[string]string{
		"image/png":  "PNG",
		"image/jpeg": "JPG",
		"image/jpg":  "JPG",
		"":           "PNG",
		"image/gif":  "PNG",
	}
	for in, want := range cases {
		if got := fpdfImageType(in); got != want {
			t.Errorf("fpdfImageType(%q) = %q, want %q", in, got, want)
		}
	}
}
