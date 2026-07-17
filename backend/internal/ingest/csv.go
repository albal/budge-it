// Package ingest turns uploaded statements (CSV or OCR-extracted text) into
// parsed transactions ready for categorization.
package ingest

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/budge-it/backend/internal/models"
)

// ParsedTxn is one statement line before categorization.
type ParsedTxn struct {
	Date        time.Time
	Description string
	Amount      float64 // always positive
	Direction   models.Direction
}

var dateLayouts = []string{
	"2006-01-02", "02/01/2006", "01/02/2006", "2/1/2006", "1/2/2006",
	"02-01-2006", "01-02-2006", "02/01/06", "01/02/06",
	"02 Jan 2006", "2 Jan 2006", "Jan 2, 2006", "Jan 02, 2006",
	"02 Jan 06", "2006/01/02",
}

func parseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// cleanAmount strips currency symbols, thousands separators, asterisks and
// surrounding parentheses (accounting negatives), returning the value and
// whether it was negative.
func cleanAmount(s string) (float64, bool, error) {
	s = strings.TrimSpace(s)
	neg := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		neg = true
		s = s[1 : len(s)-1]
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9' || r == '.':
			b.WriteRune(r)
		case r == '-':
			neg = true
		}
	}
	if b.Len() == 0 {
		return 0, false, fmt.Errorf("no numeric value in %q", s)
	}
	v, err := strconv.ParseFloat(b.String(), 64)
	if err != nil {
		return 0, false, err
	}
	return v, neg, nil
}

// parseFlag reads a direction flag cell ("D", "DR", "Debit", "C", "CR",
// "Credit"); ok is false for anything else so callers fall back to the sign.
func parseFlag(s string) (models.Direction, bool) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "D", "DR", "DBIT", "DEBIT":
		return models.Debit, true
	case "C", "CR", "CRDT", "CREDIT":
		return models.Credit, true
	}
	return "", false
}

// ParseCSV maps statement CSVs onto transactions. It detects columns by
// header name and supports single signed Amount columns, separate Debit /
// Credit columns, and card-style Amount + Debit/Credit Flag pairs.
func ParseCSV(r io.Reader) ([]ParsedTxn, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("invalid CSV: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("CSV has no data rows")
	}

	header := records[0]
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	find := func(names ...string) int {
		for _, n := range names {
			if i, ok := col[n]; ok {
				return i
			}
		}
		return -1
	}

	dateIdx := find("date", "transaction date", "posted date", "value date")
	descIdx := find("description", "merchant", "details", "narrative", "memo", "payee", "transaction description")
	amtIdx := find("amount", "value", "transaction amount", "billing amount", "billed amount")
	debitIdx := find("debit", "debit amount", "money out", "paid out", "withdrawal")
	creditIdx := find("credit", "credit amount", "money in", "paid in", "deposit")
	// Card statements often pair a positive amount with a separate direction
	// flag column (e.g. "D"/"C") instead of a signed amount.
	flagIdx := find("debit/credit flag", "debit / credit flag", "debit/credit", "dr/cr indicator", "dr/cr")

	if dateIdx < 0 || descIdx < 0 || (amtIdx < 0 && debitIdx < 0 && creditIdx < 0) {
		return nil, fmt.Errorf("CSV must have Date, Description and Amount (or Debit/Credit) columns; got headers %v", header)
	}

	var txns []ParsedTxn
	var badRows int
	for _, rec := range records[1:] {
		get := func(i int) string {
			if i >= 0 && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		date, ok := parseDate(get(dateIdx))
		if !ok {
			badRows++
			continue
		}
		desc := strings.TrimSpace(strings.Trim(get(descIdx), "*"))
		if desc == "" {
			badRows++
			continue
		}

		var amount float64
		var dir models.Direction
		switch {
		case amtIdx >= 0 && get(amtIdx) != "":
			v, neg, err := cleanAmount(get(amtIdx))
			if err != nil {
				badRows++
				continue
			}
			amount = v
			if flag, ok := parseFlag(get(flagIdx)); ok {
				dir = flag // an explicit flag beats the amount's sign
			} else if neg {
				dir = models.Debit
			} else {
				dir = models.Credit
			}
		case debitIdx >= 0 && get(debitIdx) != "":
			v, _, err := cleanAmount(get(debitIdx))
			if err != nil {
				badRows++
				continue
			}
			amount, dir = v, models.Debit
		case creditIdx >= 0 && get(creditIdx) != "":
			v, _, err := cleanAmount(get(creditIdx))
			if err != nil {
				badRows++
				continue
			}
			amount, dir = v, models.Credit
		default:
			badRows++
			continue
		}
		if amount == 0 {
			continue
		}
		txns = append(txns, ParsedTxn{Date: date, Description: desc, Amount: amount, Direction: dir})
	}
	if len(txns) == 0 {
		return nil, fmt.Errorf("no parseable transactions found (%d rows rejected)", badRows)
	}
	return txns, nil
}
