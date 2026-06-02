package server

import (
	"bytes"
	"fmt"
	"time"

	"github.com/go-pdf/fpdf"
)

// Shared company-letterhead PDF helper (LETTERHEAD).
//
// THE reusable building block every downloadable document PDF starts from. It
// renders the tenant's configured letterhead — optional logo top-left, the
// company identity block, a tax/registration line, and a horizontal divider —
// then sets a footer (page "X of Y", a generation timestamp, and the optional
// footer note) so each subsequent page is stamped consistently. After calling
// it, build the body with the existing pdfDoc layout helpers (sectionHeading,
// keyValue, table, note) and finish with doc.bytes().
//
// It is deliberately self-contained: it takes a plain LetterheadBranding value
// (and the logo bytes) rather than importing the branding repo, so callers in
// any wave can map their tenant branding into it without a dependency cycle.

const (
	// letterheadLogoMaxWidth is the max logo box width (mm). The logo is scaled
	// to fit this width, preserving aspect, and never taller than its box.
	letterheadLogoMaxWidth = 30.0
	// letterheadLogoMaxHeight caps the logo height so a tall/square logo cannot
	// push the company block down the page.
	letterheadLogoMaxHeight = 22.0
)

// LetterheadBranding is the flattened branding a letterhead renders. It mirrors
// the text fields of internal/branding.Branding plus the resolved logo bytes,
// kept free of any repo/db type so the helper has no awkward dependencies.
type LetterheadBranding struct {
	DisplayName     string
	LegalName       string
	TaxID           string
	RegistrationNo  string
	AddressLine1    string
	AddressLine2    string
	City            string
	Country         string
	Phone           string
	Email           string
	Website         string
	FooterNote      string
	Logo            []byte // optional PNG/JPEG bytes; empty = no logo
	LogoContentType string // "image/png" | "image/jpeg" (used to pick the fpdf image type)
}

// LetterheadOptions configures the document around the branding.
type LetterheadOptions struct {
	// Title is the large document title under the brand block (e.g. "Daily
	// Shift & Close Report"). Optional.
	Title string
	// SubLines are muted lines under the title (period/scope/etc.). Optional.
	SubLines []string
	// GeneratedAt stamps the footer. Injected (not time.Now()) so deterministic
	// tests can pin it; the zero value falls back to time.Now().UTC() so normal
	// callers need not pass anything.
	GeneratedAt time.Time
}

// fpdfImageType maps a stored content type to the fpdf image-type token. fpdf
// accepts "PNG"/"JPG"; an unknown/empty type defaults to PNG.
func fpdfImageType(contentType string) string {
	switch contentType {
	case "image/jpeg", "image/jpg":
		return "JPG"
	default:
		return "PNG"
	}
}

// newLetterheadDoc starts a portrait A4 document carrying the tenant letterhead
// header and a stamped footer, returning a *pdfDoc ready for the body helpers.
// This is the shared entry point document PDFs reuse.
func newLetterheadDoc(b LetterheadBranding, opts LetterheadOptions) *pdfDoc {
	generatedAt := opts.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pdfMargin, pdfMargin, pdfMargin)
	// Leave room at the bottom for the footer band.
	pdf.SetAutoPageBreak(true, pdfMargin+12)

	d := &pdfDoc{pdf: pdf, tr: pdf.UnicodeTranslatorFromDescriptor("cp1252")}

	// Footer: page "X of Y", generation timestamp, and the optional footer note.
	// AliasNbPages lets "{nb}" resolve to the final page count.
	pdf.AliasNbPages("{nb}")
	footerNote := b.FooterNote
	pdf.SetFooterFunc(func() {
		pdf.SetY(-15)
		pdf.SetDrawColor(225, 225, 225)
		fy := pdf.GetY()
		pdf.Line(pdfMargin, fy, 210-pdfMargin, fy)
		pdf.Ln(1)
		pdf.SetFont(pdfFontFamily, "I", 7)
		pdf.SetTextColor(140, 140, 140)
		left := "Generated " + generatedAt.Format("2006-01-02 15:04 MST")
		if footerNote != "" {
			left += "  •  " + footerNote
		}
		// Left: timestamp (+ note). Right: page X of Y.
		pdf.CellFormat(0, 5, d.tr(left), "", 0, "L", false, 0, "")
		pdf.CellFormat(0, 5, d.tr(fmt.Sprintf("Page %d of {nb}", pdf.PageNo())), "", 0, "R", false, 0, "")
	})

	pdf.AddPage()
	d.renderLetterhead(b, opts)
	return d
}

// renderLetterhead lays out the header: optional logo top-left, the company
// identity block to its right (or full-width when no logo), the tax/reg line,
// the document title + sublines, and a divider. Splitting it out keeps
// newLetterheadDoc readable and lets tests exercise the layout directly.
func (d *pdfDoc) renderLetterhead(b LetterheadBranding, opts LetterheadOptions) {
	pdf := d.pdf
	startY := pdf.GetY()
	textX := pdfMargin

	// Logo top-left, scaled to fit the logo box while preserving aspect.
	if len(b.Logo) > 0 {
		imgType := fpdfImageType(b.LogoContentType)
		info := pdf.RegisterImageOptionsReader(
			"letterhead_logo",
			fpdf.ImageOptions{ImageType: imgType},
			bytes.NewReader(b.Logo),
		)
		if pdf.Ok() && info != nil && info.Width() > 0 && info.Height() > 0 {
			w := letterheadLogoMaxWidth
			h := w * info.Height() / info.Width()
			if h > letterheadLogoMaxHeight {
				h = letterheadLogoMaxHeight
				w = h * info.Width() / info.Height()
			}
			pdf.ImageOptions(
				"letterhead_logo", pdfMargin, startY, w, h, false,
				fpdf.ImageOptions{ImageType: imgType}, 0, "",
			)
			textX = pdfMargin + letterheadLogoMaxWidth + 6
		}
	}

	// Company identity block, positioned to the right of the logo box.
	blockW := 210 - pdfMargin - textX
	pdf.SetXY(textX, startY)

	displayName := b.DisplayName
	if displayName == "" {
		displayName = b.LegalName
	}
	if displayName == "" {
		displayName = pdfBrand
	}
	pdf.SetFont(pdfFontFamily, "B", 14)
	pdf.SetTextColor(20, 20, 20)
	pdf.CellFormat(blockW, 6, d.tr(displayName), "", 2, "L", false, 0, "")

	pdf.SetFont(pdfFontFamily, "", 9)
	pdf.SetTextColor(90, 90, 90)
	line := func(s string) {
		if s == "" {
			return
		}
		pdf.SetX(textX)
		pdf.CellFormat(blockW, 4.4, d.tr(s), "", 2, "L", false, 0, "")
	}
	if b.LegalName != "" && b.LegalName != displayName {
		line(b.LegalName)
	}
	line(b.AddressLine1)
	line(b.AddressLine2)
	line(joinNonEmpty(", ", b.City, b.Country))
	line(joinNonEmpty("  •  ", b.Phone, b.Email, b.Website))
	if b.TaxID != "" || b.RegistrationNo != "" {
		var taxLine string
		if b.TaxID != "" {
			taxLine = "Tax PIN: " + b.TaxID
		}
		if b.RegistrationNo != "" {
			if taxLine != "" {
				taxLine += "   "
			}
			taxLine += "Reg No: " + b.RegistrationNo
		}
		line(taxLine)
	}

	// Drop below whichever column is taller (logo box vs text block).
	headerBottom := pdf.GetY()
	if len(b.Logo) > 0 {
		logoBottom := startY + letterheadLogoMaxHeight
		if logoBottom > headerBottom {
			headerBottom = logoBottom
		}
	}
	pdf.SetXY(pdfMargin, headerBottom+2)

	// Document title + sublines.
	if opts.Title != "" {
		pdf.SetFont(pdfFontFamily, "B", 16)
		pdf.SetTextColor(20, 20, 20)
		pdf.CellFormat(0, 8, d.tr(opts.Title), "", 1, "L", false, 0, "")
	}
	if len(opts.SubLines) > 0 {
		pdf.SetFont(pdfFontFamily, "", 10)
		pdf.SetTextColor(80, 80, 80)
		for _, s := range opts.SubLines {
			if s == "" {
				continue
			}
			pdf.CellFormat(0, 5, d.tr(s), "", 1, "L", false, 0, "")
		}
	}

	// Divider under the header.
	pdf.Ln(2)
	pdf.SetDrawColor(210, 210, 210)
	y := pdf.GetY()
	pdf.Line(pdfMargin, y, 210-pdfMargin, y)
	pdf.Ln(4)
	pdf.SetTextColor(20, 20, 20)
}

// joinNonEmpty joins the non-empty parts with sep.
func joinNonEmpty(sep string, parts ...string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += sep
		}
		out += p
	}
	return out
}
