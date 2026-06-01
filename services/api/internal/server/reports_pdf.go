package server

import (
	"fmt"
	"time"

	"github.com/go-pdf/fpdf"
)

// PDF report rendering (REPORTS-PDF).
//
// A dependency-light wrapper over github.com/go-pdf/fpdf that lays out the two
// formal documents the operators print and file: the daily shift/close report
// and the financial statement (balance sheet + P&L). The look is intentionally
// plain and brandable — a title block (product + tenant + period), then tabular
// figures in a fixed-width-ish monospaced font so money/litre columns line up.
//
// Money and litre values are passed in verbatim as the exact decimal STRINGS the
// repos already return (numeric ::text). The PDF never parses them into floats —
// it only renders the strings, so the figures on the page are byte-identical to
// the CSV and JSON contracts (MD-5).

const (
	pdfMargin     = 15.0 // page margin (mm)
	pdfBrand      = "FuelGrid OS"
	pdfFontFamily = "Helvetica"
)

// pdfDoc wraps an fpdf.Fpdf with the report's layout helpers and a deferred
// error. fpdf accumulates errors internally; callers check Err() at the end.
//
// tr transcodes UTF-8 into the cp1252 byte sequence fpdf's built-in (core)
// fonts expect, so punctuation our strings carry — em dashes, bullets,
// ellipses — renders correctly instead of as mojibake. All text passed to the
// page goes through tr.
type pdfDoc struct {
	pdf *fpdf.Fpdf
	tr  func(string) string
}

// newReportPDF starts a portrait A4 document with the shared title block:
// the product name, the document title, the tenant line, the period/scope line,
// and a generation timestamp. contentWidth is the usable width between margins.
func newReportPDF(title, tenantLine, periodLine string) *pdfDoc {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pdfMargin, pdfMargin, pdfMargin)
	pdf.SetAutoPageBreak(true, pdfMargin)
	pdf.AddPage()

	d := &pdfDoc{pdf: pdf, tr: pdf.UnicodeTranslatorFromDescriptor("cp1252")}

	// Brand eyebrow.
	pdf.SetFont(pdfFontFamily, "B", 9)
	pdf.SetTextColor(120, 120, 120)
	pdf.CellFormat(0, 5, d.tr(pdfBrand), "", 1, "L", false, 0, "")

	// Document title.
	pdf.SetFont(pdfFontFamily, "B", 18)
	pdf.SetTextColor(20, 20, 20)
	pdf.CellFormat(0, 9, d.tr(title), "", 1, "L", false, 0, "")

	// Tenant + period/scope lines.
	pdf.SetFont(pdfFontFamily, "", 10)
	pdf.SetTextColor(80, 80, 80)
	if tenantLine != "" {
		pdf.CellFormat(0, 5, d.tr(tenantLine), "", 1, "L", false, 0, "")
	}
	if periodLine != "" {
		pdf.CellFormat(0, 5, d.tr(periodLine), "", 1, "L", false, 0, "")
	}

	// Generation timestamp.
	pdf.SetFont(pdfFontFamily, "I", 8)
	pdf.SetTextColor(140, 140, 140)
	pdf.CellFormat(0, 5, "Generated "+time.Now().UTC().Format("2006-01-02 15:04 MST"), "", 1, "L", false, 0, "")

	// Divider.
	pdf.Ln(2)
	pdf.SetDrawColor(210, 210, 210)
	y := pdf.GetY()
	pdf.Line(pdfMargin, y, 210-pdfMargin, y)
	pdf.Ln(4)
	pdf.SetTextColor(20, 20, 20)
	return d
}

// sectionHeading renders a bold section title (e.g. "Profit & Loss").
func (d *pdfDoc) sectionHeading(text string) {
	d.pdf.Ln(2)
	d.pdf.SetFont(pdfFontFamily, "B", 12)
	d.pdf.SetTextColor(20, 20, 20)
	d.pdf.CellFormat(0, 7, d.tr(text), "", 1, "L", false, 0, "")
	d.pdf.Ln(1)
}

// keyValue renders a two-column "label … amount" line, the amount right-aligned
// in a monospaced font so figures line up. Used for statement line items.
func (d *pdfDoc) keyValue(label, amount string) {
	const labelW = 110.0
	d.pdf.SetFont(pdfFontFamily, "", 10)
	d.pdf.SetTextColor(40, 40, 40)
	d.pdf.CellFormat(labelW, 6, d.tr(label), "", 0, "L", false, 0, "")
	d.pdf.SetFont("Courier", "", 10)
	d.pdf.CellFormat(0, 6, d.tr(amount), "", 1, "R", false, 0, "")
}

// totalRow renders a bold "label … amount" line above a thin rule — the running
// total / net figure that closes a section.
func (d *pdfDoc) totalRow(label, amount string) {
	const labelW = 110.0
	y := d.pdf.GetY()
	d.pdf.SetDrawColor(210, 210, 210)
	d.pdf.Line(pdfMargin, y, 210-pdfMargin, y)
	d.pdf.Ln(1)
	d.pdf.SetFont(pdfFontFamily, "B", 10)
	d.pdf.SetTextColor(20, 20, 20)
	d.pdf.CellFormat(labelW, 6, d.tr(label), "", 0, "L", false, 0, "")
	d.pdf.SetFont("Courier", "B", 10)
	d.pdf.CellFormat(0, 6, d.tr(amount), "", 1, "R", false, 0, "")
}

// table renders a simple bordered table: a header row (bold, shaded) followed by
// the data rows. widths are in mm and must sum to <= the content width (180mm on
// A4 with the standard margins). aligns is per-column ("L"|"R"|"C"); columns
// after len(aligns) default to left. Numeric columns use a monospaced font.
func (d *pdfDoc) table(headers []string, widths []float64, aligns []string, rows [][]string) {
	alignOf := func(i int) string {
		if i < len(aligns) {
			return aligns[i]
		}
		return "L"
	}

	// Header row.
	d.pdf.SetFont(pdfFontFamily, "B", 8)
	d.pdf.SetFillColor(238, 240, 243)
	d.pdf.SetTextColor(40, 40, 40)
	d.pdf.SetDrawColor(210, 210, 210)
	for i, h := range headers {
		d.pdf.CellFormat(widths[i], 7, d.tr(h), "1", 0, alignOf(i), true, 0, "")
	}
	d.pdf.Ln(-1)

	// Data rows.
	d.pdf.SetTextColor(40, 40, 40)
	for _, row := range rows {
		for i, cell := range row {
			align := alignOf(i)
			if align == "R" {
				d.pdf.SetFont("Courier", "", 8)
			} else {
				d.pdf.SetFont(pdfFontFamily, "", 8)
			}
			d.pdf.CellFormat(widths[i], 6, d.tr(cell), "1", 0, align, false, 0, "")
		}
		d.pdf.Ln(-1)
	}
}

// note renders a small muted paragraph (footnotes / balance check).
func (d *pdfDoc) note(text string) {
	d.pdf.Ln(2)
	d.pdf.SetFont(pdfFontFamily, "I", 8)
	d.pdf.SetTextColor(120, 120, 120)
	d.pdf.MultiCell(0, 4, d.tr(text), "", "L", false)
	d.pdf.SetTextColor(20, 20, 20)
}

// bytes finalises the document and returns the encoded PDF, or any error fpdf
// accumulated during layout.
func (d *pdfDoc) bytes() ([]byte, error) {
	var buf pdfBuffer
	if err := d.pdf.Output(&buf); err != nil {
		return nil, err
	}
	if err := d.pdf.Error(); err != nil {
		return nil, err
	}
	return buf.b, nil
}

// pdfBuffer is a tiny io.Writer so we can capture the PDF without importing
// bytes here (reports_handlers.go already owns the bytes import for CSV).
type pdfBuffer struct{ b []byte }

func (p *pdfBuffer) Write(b []byte) (int, error) {
	p.b = append(p.b, b...)
	return len(b), nil
}

// tenantLine formats the tenant identity line for the title block.
func tenantLine(tenantID fmt.Stringer) string {
	return "Tenant " + tenantID.String()
}
