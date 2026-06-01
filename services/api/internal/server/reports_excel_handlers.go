package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// Excel (XLSX) report exports (REPORTING).
//
// These endpoints mirror the existing revenue / reconciliation / financials CSV
// exports (reports_handlers.go) but produce a clean .xlsx workbook: a frozen
// header row, a styled header band, and currency/number formatting so the file
// opens ready-to-read in Excel/Sheets. Money/litre figures are written as exact
// numbers parsed from their decimal strings ONLY for the cell value — the
// figures are not recomputed. Each export is permission-gated by the route and
// audited via writeExportFile exactly like the CSV/GL exports.

const (
	xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	// moneyFmt is an accounting-style number format with thousands separators.
	moneyFmt  = "#,##0.00"
	numberFmt = "#,##0.######"
)

// xlsxColumn describes one column: its header and number format (empty = text).
type xlsxColumn struct {
	header string
	// numFmt is an Excel number format applied to the data cells; "" leaves the
	// value as-is (text). "money"/"number" are mapped to the shared formats.
	numFmt string
}

// buildWorkbook renders a single-sheet workbook with a frozen, styled header
// row and per-column number formats. rows are raw string cell values; numeric
// columns are parsed to float for the cell so Excel treats them as numbers.
// Returns the serialised .xlsx bytes.
func buildWorkbook(sheet string, cols []xlsxColumn, rows [][]string) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	idx, err := f.NewSheet(sheet)
	if err != nil {
		return nil, err
	}
	f.SetActiveSheet(idx)
	if def := f.GetSheetName(0); def != "" && def != sheet {
		_ = f.DeleteSheet(def)
	}

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"1F2937"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	if err != nil {
		return nil, err
	}
	moneyStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: ptr(moneyFmt)})
	if err != nil {
		return nil, err
	}
	numberStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: ptr(numberFmt)})
	if err != nil {
		return nil, err
	}

	// Header row.
	for c, col := range cols {
		cell, _ := excelize.CoordinatesToCellName(c+1, 1)
		_ = f.SetCellStr(sheet, cell, col.header)
		_ = f.SetCellStyle(sheet, cell, cell, headerStyle)
		_ = f.SetColWidth(sheet, colLetter(c+1), colLetter(c+1), 18)
	}
	// Freeze the header row.
	_ = f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})

	// Data rows.
	for ri, row := range rows {
		for c, col := range cols {
			cell, _ := excelize.CoordinatesToCellName(c+1, ri+2)
			if c >= len(row) {
				continue
			}
			val := row[c]
			switch col.numFmt {
			case "money", "number":
				if n, err := strconv.ParseFloat(val, 64); err == nil {
					_ = f.SetCellFloat(sheet, cell, n, -1, 64)
					if col.numFmt == "money" {
						_ = f.SetCellStyle(sheet, cell, cell, moneyStyle)
					} else {
						_ = f.SetCellStyle(sheet, cell, cell, numberStyle)
					}
					continue
				}
				_ = f.SetCellStr(sheet, cell, val)
			default:
				_ = f.SetCellStr(sheet, cell, val)
			}
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func ptr[T any](v T) *T { return &v }

// colLetter returns the Excel column letter for a 1-based index.
func colLetter(n int) string {
	name, _ := excelize.ColumnNumberToName(n)
	return name
}

// handleExportRevenueXLSX mirrors handleExportRevenueCSV as a styled workbook.
func (s *Server) handleExportRevenueXLSX(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}
	ctx := r.Context()
	station, err := s.stations.Get(ctx, actor.TenantID, stationID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	days, err := s.revenue.RecentDays(ctx, actor.TenantID, stationID, 90)
	if err != nil {
		s.logger.Error("export revenue xlsx: recent days", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	cols := []xlsxColumn{
		{"Business date", ""}, {"Status", ""},
		{"Gross revenue", "money"}, {"Net revenue", "money"}, {"Tax total", "money"},
		{"COGS total", "money"}, {"Margin total", "money"}, {"Cash", "money"},
		{"Mobile money", "money"}, {"Card", "money"}, {"Credit", "money"},
		{"Voucher", "money"}, {"Tender total", "money"}, {"Cash variance", "money"},
	}
	rows := make([][]string, 0, len(days))
	for i := range days {
		d := days[i]
		rows = append(rows, []string{
			d.BusinessDate.Format(dateLayout), d.Status, d.GrossRevenue, d.NetRevenue, d.TaxTotal,
			d.CogsTotal, d.MarginTotal, d.CashTotal, d.MobileMoneyTotal, d.CardTotal,
			d.CreditTotal, d.VoucherTotal, d.TenderTotal, d.CashVariance,
		})
	}
	body, err := buildWorkbook("Revenue days", cols, rows)
	if err != nil {
		s.logger.Error("export revenue xlsx: build", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	filename := fmt.Sprintf("revenue-%s.xlsx", station.Code)
	s.writeExportFile(w, r, actor, "revenue_day", "xlsx", filename, xlsxContentType, body, map[string]any{
		"station_id": stationID.String(), "station_code": station.Code, "row_count": len(rows),
	})
}

// handleExportReconciliationXLSX mirrors handleExportReconciliationCSV.
func (s *Server) handleExportReconciliationXLSX(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}
	ctx := r.Context()
	station, err := s.stations.Get(ctx, actor.TenantID, stationID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var dayID uuid.UUID
	var businessDate string
	if raw := r.URL.Query().Get("operating_day_id"); raw != "" {
		dayID, err = uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid operating_day_id")
			return
		}
		day, derr := s.operations.GetDay(ctx, actor.TenantID, dayID)
		if errors.Is(derr, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "operating day not found")
			return
		}
		if derr != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		businessDate = day.BusinessDate.Format(dateLayout)
	} else {
		day, derr := s.operations.LatestActiveDayForStation(ctx, actor.TenantID, stationID)
		if derr == nil {
			dayID = day.ID
			businessDate = day.BusinessDate.Format(dateLayout)
		} else if !errors.Is(derr, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	cols := []xlsxColumn{
		{"Tank", ""}, {"Business date", ""}, {"Opening book", "number"},
		{"Deliveries", "number"}, {"Sales", "number"}, {"Adjustments", "number"},
		{"Closing book", "number"}, {"Closing physical", "number"}, {"Variance (L)", "number"},
		{"Variance %", "number"}, {"Tolerance %", "number"}, {"Status", ""},
	}
	rows := [][]string{}
	if dayID != uuid.Nil {
		recs, rerr := s.reconciliation.ListForStationDay(ctx, actor.TenantID, stationID, dayID)
		if rerr != nil {
			s.logger.Error("export reconciliation xlsx: list", "error", rerr)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for i := range recs {
			rec := recs[i]
			rows = append(rows, []string{
				rec.TankID.String(), businessDate, rec.OpeningBook, rec.DeliveriesTotal, rec.SalesTotal,
				rec.AdjustmentsTotal, rec.ClosingBook, rec.ClosingPhysical, rec.VarianceLitres,
				rec.VariancePercent, rec.TolerancePercent, rec.Status,
			})
		}
	}
	body, err := buildWorkbook("Reconciliation", cols, rows)
	if err != nil {
		s.logger.Error("export reconciliation xlsx: build", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	filename := fmt.Sprintf("reconciliation-%s.xlsx", station.Code)
	s.writeExportFile(w, r, actor, "reconciliation", "xlsx", filename, xlsxContentType, body, map[string]any{
		"station_id": stationID.String(), "station_code": station.Code,
		"operating_day_id": dayID.String(), "business_date": businessDate, "row_count": len(rows),
	})
}

// handleExportFinancialsXLSX mirrors handleExportFinancialsCSV: a summary sheet
// of P&L + balance-sheet line items.
func (s *Server) handleExportFinancialsXLSX(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ctx := r.Context()
	period := r.URL.Query().Get("period")
	from, to, label := resolveReportPeriod(period, time.Now())

	is, err := s.accounting.IncomeStatement(ctx, actor.TenantID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	bs, err := s.accounting.BalanceSheet(ctx, actor.TenantID, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	cols := []xlsxColumn{{"Statement", ""}, {"Line item", ""}, {"Amount", "money"}}
	rows := [][]string{
		{"Income statement", "Revenue", is.Revenue},
		{"Income statement", "Expenses", is.Expenses},
		{"Income statement", "Net profit", is.NetProfit},
		{"Balance sheet", "Assets", bs.Assets},
		{"Balance sheet", "Liabilities", bs.Liabilities},
		{"Balance sheet", "Equity", bs.Equity},
		{"Balance sheet", "Retained earnings", bs.RetainedEarnings},
		{"Balance sheet", "Net income", bs.NetIncome},
		{"Balance sheet", "Balanced", strconv.FormatBool(bs.Balanced)},
	}
	body, err := buildWorkbook("Financials", cols, rows)
	if err != nil {
		s.logger.Error("export financials xlsx: build", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	filename := fmt.Sprintf("financials-%s.xlsx", label)
	s.writeExportFile(w, r, actor, "financials", "xlsx", filename, xlsxContentType, body, map[string]any{
		"period": label, "from": from.Format(dateLayout), "to": to.Format(dateLayout), "row_count": len(rows),
	})
}
