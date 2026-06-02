package server

import (
	"net/http"

	"github.com/google/uuid"
)

// Reusable table-document PDF framework (DOC-PDF).
//
// THE generic building block for every "list of records" PDF a tenant can View
// or Download — customers, suppliers, products today, and ~a dozen more later.
// It sits one layer above the shared letterhead helper (newLetterheadDoc): a
// caller describes a letterheaded table document declaratively with a
// ListDocumentSpec (title, optional sub-lines, an optional meta/filter band,
// the column definitions, the already-formatted rows, and an optional totals
// row) and renderListDocument produces the finished PDF bytes.
//
// The contract that keeps this safe to fan out:
//   - Cells arrive PRE-FORMATTED. Money/litre/rate values are the exact decimal
//     STRINGS the repos return (numeric ::text); this layer renders them
//     verbatim and NEVER parses them into float64, so a figure on the page is
//     byte-identical to the JSON/CSV contracts.
//   - Long tables paginate across pages with the header row repeated at the top
//     of every page, so a 500-row catalogue prints cleanly. The letterhead
//     header is drawn once (page 1); the footer (page X of Y + timestamp) is
//     stamped on every page by the letterhead helper.
//
// The next wave reuses renderListDocument unchanged — only a new spec per
// entity and a thin handler that loads the rows.

// DocumentColumn describes one table column in a ListDocumentSpec.
type DocumentColumn struct {
	// Header is the column label shown in the (repeating) header row.
	Header string
	// Width is the column width in mm. The sum of all widths must be <= the
	// usable content width (documentContentWidth, 180mm on A4 with the standard
	// 15mm margins). Callers size columns to fit.
	Width float64
	// Align is the cell alignment: "L" (default), "R", or "C". Money/numeric
	// columns are conventionally right-aligned.
	Align string
	// Numeric marks a column whose cells are figures (money/litres/rates). It
	// drives the monospaced figure font so decimals line up, mirroring the
	// pdfDoc.table convention where right-aligned columns render monospaced.
	// Setting Numeric is equivalent to Align "R" for font selection; set both
	// for right-aligned figures.
	Numeric bool
}

// ListDocumentSpec declaratively describes a letterheaded table document. It is
// deliberately generic and self-contained: a handler maps its rows into plain
// strings and hands the spec to renderListDocument.
type ListDocumentSpec struct {
	// Title is the large document title under the brand block (e.g. "Customers").
	Title string
	// SubLines are muted lines under the title — typically a record count and/or
	// the scope (e.g. "42 records"). Optional.
	SubLines []string
	// MetaPairs is an optional filter/summary band rendered above the table as
	// "Label: value" key/value lines (e.g. {"Status", "active"}). Describes the
	// filters that produced the rows. Optional; nil/empty renders no band.
	MetaPairs []DocumentMeta
	// Columns define the table header and per-column width/alignment/figure font.
	Columns []DocumentColumn
	// Rows are the already-formatted data rows. Each row's length should match
	// len(Columns); extra cells are ignored and missing cells render blank.
	Rows [][]string
	// TotalsRow is an optional bold summary row rendered under the table with the
	// same column layout (e.g. a "Total" label and column sums as decimal
	// strings). Optional; nil renders no totals row.
	TotalsRow []string
}

// DocumentMeta is one key/value line in the optional meta/filter band.
type DocumentMeta struct {
	Label string
	Value string
}

// documentContentWidth is the usable width between the standard A4 margins
// (210mm page - 2 * 15mm margin). Column widths must sum to <= this.
const documentContentWidth = 210 - 2*pdfMargin

// renderListDocument renders a generic letterheaded table document from the
// spec and returns the encoded PDF. It loads the tenant letterhead, opens a
// letterhead document, draws the optional meta band, paginates the table with a
// repeating header, appends the optional totals row, and finishes with
// doc.bytes(). This is the single entry point every list-document handler (and
// every later-wave entity) reuses.
func (s *Server) renderListDocument(r *http.Request, tenantID uuid.UUID, spec ListDocumentSpec) ([]byte, error) {
	doc := newLetterheadDoc(s.loadLetterhead(r, tenantID), LetterheadOptions{
		Title:    spec.Title,
		SubLines: spec.SubLines,
	})

	// Optional meta/filter band describing the applied filters/scope.
	if len(spec.MetaPairs) > 0 {
		doc.metaBand(spec.MetaPairs)
	}

	doc.listTable(spec.Columns, spec.Rows, spec.TotalsRow)

	return doc.bytes()
}

// RecordDocumentSpec declaratively describes a single formal RECORD document —
// the kind you'd email a counterparty (a purchase order to a supplier, an
// invoice to a customer). Unlike a list document (many rows of one entity) it
// renders ONE entity: a letterhead, an optional party/address block, a
// key/value header (number, dates, status, …), a line-items table, and a small
// stack of totals. It is built on the same letterhead helper as the list
// framework so a record and a list print with identical branding.
type RecordDocumentSpec struct {
	// Title is the document title (e.g. "Purchase Order", "Tax Invoice").
	Title string
	// SubLines are muted lines under the title (e.g. the document number).
	SubLines []string
	// PartyHeading labels the party block (e.g. "Supplier", "Bill to"). When
	// empty the party block is skipped entirely.
	PartyHeading string
	// PartyLines are the counterparty identity lines (name, then optional
	// address/contact lines). Rendered under PartyHeading.
	PartyLines []string
	// MetaPairs is the document's key/value header (number, dates, status, …),
	// rendered as a two-column "Label  value" band.
	MetaPairs []DocumentMeta
	// LineColumns/Lines/LineTotals describe the line-items table, reusing the
	// same paginating listTable as the list framework. Cells are pre-formatted
	// decimal strings — never float64.
	LineColumns []DocumentColumn
	Lines       [][]string
	LineTotals  []string
	// Totals are the closing figures rendered as bold "label  amount" rows under
	// the table (e.g. Subtotal / Tax / Total). Amounts are decimal strings.
	Totals []DocumentMeta
	// Note is an optional muted footnote (terms, remittance instructions).
	Note string
}

// renderRecordDocument renders a single formal record document (PO, invoice)
// from the spec and returns the encoded PDF. It loads the tenant letterhead,
// draws the optional party block, the key/value meta band, the paginating
// line-items table, the closing totals, and an optional note. It is the
// record-shaped counterpart to renderListDocument and reuses the same
// letterhead helper and listTable so both document families share branding and
// pagination.
func (s *Server) renderRecordDocument(r *http.Request, tenantID uuid.UUID, spec RecordDocumentSpec) ([]byte, error) {
	doc := newLetterheadDoc(s.loadLetterhead(r, tenantID), LetterheadOptions{
		Title:    spec.Title,
		SubLines: spec.SubLines,
	})

	if spec.PartyHeading != "" && len(spec.PartyLines) > 0 {
		doc.partyBlock(spec.PartyHeading, spec.PartyLines)
	}

	if len(spec.MetaPairs) > 0 {
		doc.metaBand(spec.MetaPairs)
	}

	if len(spec.LineColumns) > 0 {
		doc.listTable(spec.LineColumns, spec.Lines, spec.LineTotals)
	}

	for _, t := range spec.Totals {
		doc.totalRow(t.Label, t.Value)
	}

	if spec.Note != "" {
		doc.note(spec.Note)
	}

	return doc.bytes()
}

// partyBlock renders the counterparty identity block of a record document: a
// small bold heading ("Supplier" / "Bill to") followed by the party's name and
// address/contact lines. Kept visually distinct from the meta band so the
// reader sees who the document is for at a glance.
func (d *pdfDoc) partyBlock(heading string, lines []string) {
	pdf := d.pdf
	pdf.SetFont(pdfFontFamily, "B", 9)
	pdf.SetTextColor(90, 90, 90)
	pdf.CellFormat(0, 5, d.tr(heading), "", 1, "L", false, 0, "")
	pdf.SetTextColor(20, 20, 20)
	for i, ln := range lines {
		if ln == "" {
			continue
		}
		style := ""
		if i == 0 {
			style = "B" // the party name
		}
		pdf.SetFont(pdfFontFamily, style, 10)
		pdf.CellFormat(0, 5, d.tr(ln), "", 1, "L", false, 0, "")
	}
	pdf.Ln(2)
}

// fitColumnWidths returns cols unchanged when their widths fit within the
// usable content width, or a copy scaled down proportionally when they
// overflow, so a mis-sized column set can never run off the right margin. It
// never scales up: a narrow table stays left-aligned at its declared widths.
func fitColumnWidths(cols []DocumentColumn) []DocumentColumn {
	var total float64
	for _, c := range cols {
		total += c.Width
	}
	if total <= documentContentWidth || total == 0 {
		return cols
	}
	scale := documentContentWidth / total
	out := make([]DocumentColumn, len(cols))
	copy(out, cols)
	for i := range out {
		out[i].Width *= scale
	}
	return out
}

// metaBand renders a compact key/value band ("Label: value") describing the
// filters/scope that produced the document's rows. Kept visually lighter than a
// section heading so it reads as metadata, not content.
func (d *pdfDoc) metaBand(pairs []DocumentMeta) {
	pdf := d.pdf
	pdf.SetFont(pdfFontFamily, "", 9)
	pdf.SetTextColor(90, 90, 90)
	for _, p := range pairs {
		if p.Label == "" && p.Value == "" {
			continue
		}
		pdf.SetFont(pdfFontFamily, "B", 9)
		label := p.Label + ": "
		w := pdf.GetStringWidth(d.tr(label)) + 1
		pdf.CellFormat(w, 5, d.tr(label), "", 0, "L", false, 0, "")
		pdf.SetFont(pdfFontFamily, "", 9)
		pdf.CellFormat(0, 5, d.tr(p.Value), "", 1, "L", false, 0, "")
	}
	pdf.Ln(2)
	pdf.SetTextColor(20, 20, 20)
}

// listTable renders the spec's columns + rows as a bordered table that
// paginates across pages with the header row repeated at the top of every page,
// then appends the optional totals row. It is the paginating counterpart to the
// simpler pdfDoc.table (which relies on fpdf's auto page break and does NOT
// repeat the header): the framework owns pagination so every fanned-out
// document prints long lists cleanly.
func (d *pdfDoc) listTable(cols []DocumentColumn, rows [][]string, totals []string) {
	pdf := d.pdf
	const (
		headerH = 7.0
		rowH    = 6.0
	)
	// The page break threshold: fpdf's auto page break trigger is the bottom
	// margin reserved for the footer (set in newLetterheadDoc). 297mm is the A4
	// height; the footer band reserves pdfMargin+12.
	pageBottom := 297.0 - (pdfMargin + 12)

	// Guard against an over-wide column set spilling past the right margin: if
	// the declared widths sum beyond the usable content width, scale them all
	// down proportionally. Well-sized specs are untouched; this just keeps a
	// fanned-out entity that mis-sizes its columns from running off the page.
	cols = fitColumnWidths(cols)

	alignOf := func(i int) string {
		if i < len(cols) && cols[i].Align != "" {
			return cols[i].Align
		}
		return "L"
	}
	// numericOf decides the figure font: an explicit Numeric flag or a
	// right-aligned column both render monospaced so decimals line up.
	numericOf := func(i int) bool {
		if i >= len(cols) {
			return false
		}
		return cols[i].Numeric || cols[i].Align == "R"
	}

	drawHeader := func() {
		pdf.SetFont(pdfFontFamily, "B", 8)
		pdf.SetFillColor(238, 240, 243)
		pdf.SetTextColor(40, 40, 40)
		pdf.SetDrawColor(210, 210, 210)
		for i, c := range cols {
			pdf.CellFormat(c.Width, headerH, d.tr(c.Header), "1", 0, alignOf(i), true, 0, "")
		}
		pdf.Ln(-1)
	}

	cellAt := func(row []string, i int) string {
		if i < len(row) {
			return row[i]
		}
		return ""
	}

	drawRow := func(row []string, bold bool) {
		pdf.SetTextColor(40, 40, 40)
		for i, c := range cols {
			align := alignOf(i)
			style := ""
			if bold {
				style = "B"
			}
			if numericOf(i) {
				pdf.SetFont("Courier", style, 8)
			} else {
				pdf.SetFont(pdfFontFamily, style, 8)
			}
			fill := false
			if bold {
				fill = true
				pdf.SetFillColor(244, 246, 248)
			}
			pdf.CellFormat(c.Width, rowH, d.tr(cellAt(row, i)), "1", 0, align, fill, 0, "")
		}
		pdf.Ln(-1)
	}

	drawHeader()
	for _, row := range rows {
		// Repeat the header at the top of a fresh page when the next row would
		// cross the footer threshold.
		if pdf.GetY()+rowH > pageBottom {
			pdf.AddPage()
			drawHeader()
		}
		drawRow(row, false)
	}

	if len(totals) > 0 {
		if pdf.GetY()+rowH > pageBottom {
			pdf.AddPage()
			drawHeader()
		}
		drawRow(totals, true)
	}
	pdf.SetTextColor(20, 20, 20)
}
