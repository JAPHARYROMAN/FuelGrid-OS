package calibration

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ParseCSV reads a strapping chart from CSV with a header row
// `dip_mm,volume_litres`. Validation is strict and all-or-nothing: it
// returns an error (naming the offending line) on the first problem rather
// than partially accepting rows.
//
// Rules:
//   - header must be exactly dip_mm,volume_litres (case-insensitive)
//   - at least two data rows
//   - dip_mm strictly increasing (so: sorted, no duplicates)
//   - dip_mm and volume_litres non-negative
//   - volume_litres non-decreasing (more depth never means less fuel)
func ParseCSV(r io.Reader) ([]Entry, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = 2
	cr.TrimLeadingSpace = true

	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, errors.New("calibration: file is empty")
	}
	if err != nil {
		return nil, fmt.Errorf("calibration: cannot read header: %w", err)
	}
	if !isExpectedHeader(header) {
		return nil, errors.New(`calibration: header must be "dip_mm,volume_litres"`)
	}

	var (
		entries []Entry
		line    = 1 // header was line 1; data starts at line 2
	)
	for {
		line++
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("calibration: line %d: %w", line, err)
		}

		dip, err := strconv.ParseFloat(strings.TrimSpace(rec[0]), 64)
		if err != nil {
			return nil, fmt.Errorf("calibration: line %d: invalid dip_mm %q", line, rec[0])
		}
		vol, err := strconv.ParseFloat(strings.TrimSpace(rec[1]), 64)
		if err != nil {
			return nil, fmt.Errorf("calibration: line %d: invalid volume_litres %q", line, rec[1])
		}
		if dip < 0 || vol < 0 {
			return nil, fmt.Errorf("calibration: line %d: dip_mm and volume_litres must be non-negative", line)
		}

		if len(entries) > 0 {
			prev := entries[len(entries)-1]
			if dip <= prev.DipMM {
				return nil, fmt.Errorf("calibration: line %d: dip_mm must strictly increase (got %g after %g)", line, dip, prev.DipMM)
			}
			if vol < prev.VolumeLitres {
				return nil, fmt.Errorf("calibration: line %d: volume_litres must not decrease (got %g after %g)", line, vol, prev.VolumeLitres)
			}
		}

		entries = append(entries, Entry{DipMM: dip, VolumeLitres: vol})
	}

	if len(entries) < 2 {
		return nil, errors.New("calibration: need at least two data rows to interpolate")
	}
	return entries, nil
}

func isExpectedHeader(h []string) bool {
	if len(h) != 2 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(h[0]), "dip_mm") &&
		strings.EqualFold(strings.TrimSpace(h[1]), "volume_litres")
}
