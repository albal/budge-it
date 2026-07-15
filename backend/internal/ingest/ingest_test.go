package ingest

import (
	"strings"
	"testing"

	"github.com/budge-it/backend/internal/models"
)

func TestParseCSVSignedAmount(t *testing.T) {
	csvData := `Date,Description,Amount
2024-03-01,TESCO STORES 3297,-45.20
2024-03-02,SALARY MARCH,"2,500.00"
2024-03-03,NETFLIX.COM,(9.99)`

	txns, err := ParseCSV(strings.NewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	if len(txns) != 3 {
		t.Fatalf("got %d transactions, want 3", len(txns))
	}
	if txns[0].Direction != models.Debit || txns[0].Amount != 45.20 {
		t.Errorf("row 0: %+v", txns[0])
	}
	if txns[1].Direction != models.Credit || txns[1].Amount != 2500.00 {
		t.Errorf("row 1: %+v", txns[1])
	}
	if txns[2].Direction != models.Debit || txns[2].Amount != 9.99 {
		t.Errorf("row 2 (parenthesized negative): %+v", txns[2])
	}
}

func TestParseCSVDebitCreditColumns(t *testing.T) {
	csvData := `Date,Description,Money Out,Money In
15/03/2024,COSTA COFFEE,£3.50,
16/03/2024,REFUND ARGOS,,£25.00`

	txns, err := ParseCSV(strings.NewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	if len(txns) != 2 {
		t.Fatalf("got %d transactions, want 2", len(txns))
	}
	if txns[0].Direction != models.Debit || txns[0].Amount != 3.50 {
		t.Errorf("row 0: %+v", txns[0])
	}
	if txns[1].Direction != models.Credit || txns[1].Amount != 25.00 {
		t.Errorf("row 1: %+v", txns[1])
	}
}

func TestParseCSVRejectsGarbage(t *testing.T) {
	if _, err := ParseCSV(strings.NewReader("not,a,statement\n1,2,3")); err == nil {
		t.Fatal("expected error for CSV without recognizable columns")
	}
}

func TestParseStatementText(t *testing.T) {
	text := `ACME BANK STATEMENT MARCH 2024
Date        Description                 Amount
01/03/2024  TESCO STORES 3297           £45.20
02/03/2024  SALARY ACME CORP            2,500.00 CR
03/03/2024  AMZN MKTPLACE*0442          -12.99
Closing balance                         3,000.00`

	txns, err := ParseStatementText(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(txns) != 3 {
		t.Fatalf("got %d transactions, want 3: %+v", len(txns), txns)
	}
	if txns[0].Direction != models.Debit {
		t.Errorf("unmarked amount should default to debit: %+v", txns[0])
	}
	if txns[1].Direction != models.Credit || txns[1].Amount != 2500 {
		t.Errorf("CR marker should mean credit: %+v", txns[1])
	}
	if txns[2].Direction != models.Debit || txns[2].Amount != 12.99 {
		t.Errorf("negative amount should mean debit: %+v", txns[2])
	}
}
