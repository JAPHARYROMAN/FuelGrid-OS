package server

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/japharyroman/fuelgrid-os/internal/reporting"
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

// ---- Premium branded report PDF (blueprint §13.2) ----
//
// renderEnvelopePDF turns any rendered ReportEnvelope into a boardroom-ready,
// branded PDF: the tenant letterhead (logo + identity when present, graceful
// fallback when not), the report title + period, a filters-used line, an
// executive-summary block (the envelope summary + the top insights), a KPI grid,
// the columnar table, a recommended-actions list, a prepared-by line and a
// confidentiality footer. It is deterministic and decimal-string-accurate — every
// money/litre figure is the exact string the envelope already carries; nothing is
// reparsed or recomputed for the page. Charts are deliberately omitted (a clean
// KPI + table + summary layout is the bar) so no chart-to-image dependency is
// pulled in.
//
// branding is the tenant letterhead (may be the zero value — the helper renders a
// graceful FuelGrid-only header). preparedBy is the human label of the actor who
// requested the export; generatedAt is injected so deterministic tests can pin it.

// kpiColumns is how many KPI cards sit per row in the executive KPI grid.
const kpiColumns = 3

// renderEnvelopePDF lays out the full premium document and returns the encoded
// PDF bytes (or any error fpdf accumulated).
func renderEnvelopePDF(env ReportEnvelope, branding LetterheadBranding, preparedBy string, generatedAt time.Time) ([]byte, error) {
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	// A confidentiality line always stamps the footer; if the tenant configured
	// their own footer note we keep it and append the confidentiality marker.
	confidential := "Confidential — prepared for internal use only"
	if branding.FooterNote != "" {
		branding.FooterNote = branding.FooterNote + "  •  " + confidential
	} else {
		branding.FooterNote = confidential
	}

	subLines := []string{}
	if env.Metadata.Period != "" {
		subLines = append(subLines, "Period: "+env.Metadata.Period)
	}
	if filtersLine := envelopeFiltersLine(env); filtersLine != "" {
		subLines = append(subLines, filtersLine)
	}

	doc := newLetterheadDoc(branding, LetterheadOptions{
		Title:       envelopeTitle(env),
		SubLines:    subLines,
		GeneratedAt: generatedAt,
	})
	// Pin the PDF's own creation/modification dates to generatedAt so the encoded
	// bytes are DETERMINISTIC for a given report snapshot — the audited checksum is
	// then stable across re-renders (fpdf otherwise stamps the wall-clock time into
	// the PDF metadata, which would change the bytes every run). SetCatalogSort
	// makes fpdf order its internal font/resource catalogs consistently, removing
	// the last source of run-to-run byte variance (font-map iteration order).
	doc.pdf.SetCreationDate(generatedAt)
	doc.pdf.SetModificationDate(generatedAt)
	doc.pdf.SetCatalogSort(true)

	doc.executiveSummary(env)
	doc.kpiGrid(env.Summary)
	doc.insightsBlock(env.Insights)
	doc.dataQualityBlock(env.DataQuality)
	doc.envelopeTable(env.Table)
	doc.recommendedActionsBlock(env.RecommendedActions)
	doc.preparedByBlock(preparedBy, generatedAt)

	return doc.bytes()
}

// envelopeTitle is the document title — the report's own title, defaulting to a
// humanised report key when the metadata title is blank.
func envelopeTitle(env ReportEnvelope) string {
	if t := strings.TrimSpace(env.Metadata.Title); t != "" {
		return t
	}
	if k := strings.TrimSpace(env.Metadata.ReportKey); k != "" {
		return strings.Title(strings.ReplaceAll(k, "-", " ")) //nolint:staticcheck // ASCII report keys; Title is sufficient
	}
	return "Report"
}

// envelopeFiltersLine renders the filters-used map as a single muted line, sorted
// for determinism. Returns "" when there are no filters.
func envelopeFiltersLine(env ReportEnvelope) string {
	if len(env.FiltersUsed) == 0 {
		return ""
	}
	keys := make([]string, 0, len(env.FiltersUsed))
	for k := range env.FiltersUsed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := env.FiltersUsed[k]
		if v == "" {
			continue
		}
		parts = append(parts, k+": "+v)
	}
	if len(parts) == 0 {
		return ""
	}
	return "Filters — " + strings.Join(parts, "  •  ")
}

// executiveSummary renders the leadership-facing summary block: a one-line
// headline assembled from the first few KPIs, then the single highest-severity
// insight as a sentence. Kept short and prose-like so a reader sees the story
// before the grid.
func (d *pdfDoc) executiveSummary(env ReportEnvelope) {
	d.sectionHeading("Executive summary")
	headline := executiveHeadline(env.Summary)
	if headline == "" && len(env.Insights) == 0 {
		d.note("No summary figures are available for this report yet.")
		return
	}
	if headline != "" {
		d.pdf.SetFont(pdfFontFamily, "", 10)
		d.pdf.SetTextColor(40, 40, 40)
		d.pdf.MultiCell(0, 5, d.tr(headline), "", "L", false)
	}
	// Surface the single most-severe insight as the narrative line.
	if lead := leadInsight(env.Insights); lead != "" {
		d.pdf.Ln(1)
		d.pdf.SetFont(pdfFontFamily, "I", 9)
		d.pdf.SetTextColor(90, 90, 90)
		d.pdf.MultiCell(0, 4.6, d.tr(lead), "", "L", false)
		d.pdf.SetTextColor(20, 20, 20)
	}
}

// executiveHeadline assembles a sentence from up to the first three KPI metrics
// ("Sales value 12,345.67 TZS; Margin 1,234.56 TZS; Open exceptions 0.").
func executiveHeadline(summary []summaryMetric) string {
	if len(summary) == 0 {
		return ""
	}
	n := len(summary)
	if n > 3 {
		n = 3
	}
	parts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		m := summary[i]
		seg := m.Label + " " + m.Value
		if m.Unit != "" {
			seg += " " + m.Unit
		}
		parts = append(parts, seg)
	}
	return strings.Join(parts, ";  ") + "."
}

// leadInsight returns the message of the highest-severity insight (critical >
// warning > info), or "" when there are none.
func leadInsight(insights []reporting.Insight) string {
	rank := func(sev reporting.Severity) int {
		switch sev {
		case reporting.SeverityCritical:
			return 3
		case reporting.SeverityWarning:
			return 2
		default:
			return 1
		}
	}
	best := -1
	bestMsg := ""
	for i := range insights {
		if r := rank(insights[i].Severity); r > best {
			best = r
			bestMsg = insights[i].Message
		}
	}
	return bestMsg
}

// kpiGrid renders the headline metrics as a card grid (kpiColumns per row): a
// boxed cell with the label above the value (+ unit), in the figure font so money
// columns read crisply. The value strings are rendered verbatim.
func (d *pdfDoc) kpiGrid(summary []summaryMetric) {
	if len(summary) == 0 {
		return
	}
	d.sectionHeading("Key figures")
	const (
		contentW = 210 - 2*pdfMargin // 180mm on A4
		gap      = 4.0
		cardH    = 16.0
	)
	cardW := (contentW - gap*(kpiColumns-1)) / kpiColumns

	for i := 0; i < len(summary); i += kpiColumns {
		// Page-break guard: if the next card row would overflow, start a page.
		if d.pdf.GetY()+cardH > 297-pdfMargin-12 {
			d.pdf.AddPage()
		}
		rowY := d.pdf.GetY()
		for c := 0; c < kpiColumns; c++ {
			idx := i + c
			if idx >= len(summary) {
				break
			}
			m := summary[idx]
			x := pdfMargin + float64(c)*(cardW+gap)
			d.pdf.SetXY(x, rowY)
			d.pdf.SetDrawColor(220, 222, 226)
			d.pdf.SetFillColor(248, 249, 251)
			d.pdf.CellFormat(cardW, cardH, "", "1", 0, "", true, 0, "")

			// Label.
			d.pdf.SetXY(x+3, rowY+2.5)
			d.pdf.SetFont(pdfFontFamily, "", 7)
			d.pdf.SetTextColor(110, 110, 110)
			d.pdf.CellFormat(cardW-6, 3.5, d.tr(strings.ToUpper(m.Label)), "", 2, "L", false, 0, "")

			// Value (+ unit).
			d.pdf.SetXY(x+3, rowY+7)
			d.pdf.SetFont("Courier", "B", 12)
			d.pdf.SetTextColor(20, 20, 20)
			value := m.Value
			d.pdf.CellFormat(cardW-6, 5.5, d.tr(value), "", 2, "L", false, 0, "")
			if m.Unit != "" {
				d.pdf.SetX(x + 3)
				d.pdf.SetFont(pdfFontFamily, "", 6.5)
				d.pdf.SetTextColor(130, 130, 130)
				d.pdf.CellFormat(cardW-6, 3, d.tr(m.Unit), "", 0, "L", false, 0, "")
			}
		}
		d.pdf.SetXY(pdfMargin, rowY+cardH+gap)
	}
	d.pdf.SetTextColor(20, 20, 20)
}

// insightsBlock lists the deterministic insights as a bulleted set, each prefixed
// by its severity word, so the reader sees the same analysis the screen shows.
func (d *pdfDoc) insightsBlock(insights []reporting.Insight) {
	if len(insights) == 0 {
		return
	}
	d.sectionHeading("Insights")
	for i := range insights {
		ins := insights[i]
		label := severityLabel(ins.Severity)
		d.pdf.SetFont(pdfFontFamily, "B", 8)
		d.pdf.SetTextColor(severityRGB(ins.Severity))
		d.pdf.CellFormat(22, 4.6, d.tr(label), "", 0, "L", false, 0, "")
		d.pdf.SetFont(pdfFontFamily, "", 9)
		d.pdf.SetTextColor(50, 50, 50)
		d.pdf.MultiCell(0, 4.6, d.tr(ins.Message), "", "L", false)
		d.pdf.Ln(0.5)
	}
	d.pdf.SetTextColor(20, 20, 20)
}

// dataQualityBlock renders the data-quality warnings as a muted caveats list, so
// the printed document never reads as more certain than the figures support.
func (d *pdfDoc) dataQualityBlock(items []dataQualityItem) {
	if len(items) == 0 {
		return
	}
	d.sectionHeading("Data quality")
	for i := range items {
		d.note("• " + items[i].Message)
	}
}

// envelopeTable renders the envelope's generic columnar table. Numeric-looking
// cells are right-aligned in the figure font so money/litre columns line up;
// everything else is left-aligned text. Column widths are distributed evenly
// across the content width.
func (d *pdfDoc) envelopeTable(t reportTable) {
	if len(t.Columns) == 0 || len(t.Rows) == 0 {
		return
	}
	d.sectionHeading("Detail")
	const contentW = 210 - 2*pdfMargin
	n := len(t.Columns)
	widths := make([]float64, n)
	aligns := make([]string, n)
	// Decide alignment per column from the FIRST data row: numeric -> right.
	for c := 0; c < n; c++ {
		widths[c] = contentW / float64(n)
		aligns[c] = "L"
		if len(t.Rows) > 0 && c < len(t.Rows[0]) && looksNumeric(t.Rows[0][c]) {
			aligns[c] = "R"
		}
	}
	d.table(t.Columns, widths, aligns, t.Rows)
}

// recommendedActionsBlock lists the deduplicated recommended actions as a
// numbered to-do, the operator's takeaways from the report.
func (d *pdfDoc) recommendedActionsBlock(actions []string) {
	if len(actions) == 0 {
		return
	}
	d.sectionHeading("Recommended actions")
	d.pdf.SetFont(pdfFontFamily, "", 9)
	d.pdf.SetTextColor(50, 50, 50)
	for i, a := range actions {
		d.pdf.SetX(pdfMargin)
		d.pdf.MultiCell(0, 4.8, d.tr(stringsItoa(i+1)+". "+a), "", "L", false)
	}
	d.pdf.SetTextColor(20, 20, 20)
}

// preparedByBlock stamps who prepared the document and when, just above the
// footer rule — the boardroom signature line.
func (d *pdfDoc) preparedByBlock(preparedBy string, generatedAt time.Time) {
	d.pdf.Ln(4)
	d.pdf.SetDrawColor(225, 225, 225)
	y := d.pdf.GetY()
	d.pdf.Line(pdfMargin, y, 210-pdfMargin, y)
	d.pdf.Ln(1.5)
	d.pdf.SetFont(pdfFontFamily, "", 8)
	d.pdf.SetTextColor(110, 110, 110)
	who := preparedBy
	if who == "" {
		who = "FuelGrid OS"
	}
	line := "Prepared by " + who + "  •  Generated " + generatedAt.Format("2006-01-02 15:04 MST")
	d.pdf.CellFormat(0, 4.5, d.tr(line), "", 1, "L", false, 0, "")
	d.pdf.SetTextColor(20, 20, 20)
}

// severityLabel maps an insight severity to its printed word.
func severityLabel(sev reporting.Severity) string {
	switch sev {
	case reporting.SeverityCritical:
		return "CRITICAL"
	case reporting.SeverityWarning:
		return "WARNING"
	default:
		return "INFO"
	}
}

// severityRGB maps an insight severity to a muted accent colour (text only —
// never colour-alone; the severity word always accompanies it).
func severityRGB(sev reporting.Severity) (int, int, int) {
	switch sev {
	case reporting.SeverityCritical:
		return 176, 42, 42 // deep red
	case reporting.SeverityWarning:
		return 176, 120, 16 // amber
	default:
		return 60, 90, 150 // info blue
	}
}

// looksNumeric reports whether a cell value is a plain decimal number (optionally
// signed), so the table can right-align money/litre columns. A leading '-' and a
// single '.' are allowed; anything else (dates with '-', labels) is text.
func looksNumeric(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	dots, digits := 0, 0
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '-' && i == 0:
			// leading sign ok
		case r == '.':
			dots++
		default:
			return false
		}
	}
	return digits > 0 && dots <= 1
}

// stringsItoa is a tiny int->string helper kept local so this file needs no extra
// import beyond strconv (which reports_pdf.go does not otherwise use).
func stringsItoa(n int) string {
	return strconv.Itoa(n)
}
