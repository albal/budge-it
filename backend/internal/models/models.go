package models

import "time"

type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"createdAt"`
}

type UploadStatus string

const (
	UploadPending    UploadStatus = "pending"
	UploadProcessing UploadStatus = "processing"
	UploadDone       UploadStatus = "done"
	UploadError      UploadStatus = "error"
)

type Upload struct {
	ID          string       `json:"id"`
	UserID      string       `json:"userId"`
	Filename    string       `json:"filename"`
	ContentType string       `json:"contentType"`
	SizeBytes   int64        `json:"sizeBytes"`
	Status      UploadStatus `json:"status"`
	Error       string       `json:"error,omitempty"`
	ObjectKey   string       `json:"-"`
	TxnCount    int          `json:"txnCount"`
	CreatedAt   time.Time    `json:"createdAt"`
	ProcessedAt *time.Time   `json:"processedAt,omitempty"`
}

type Direction string

const (
	Debit  Direction = "debit"
	Credit Direction = "credit"
)

type Transaction struct {
	ID          string    `json:"id"`
	UserID      string    `json:"userId"`
	UploadID    string    `json:"uploadId"`
	Date        time.Time `json:"date"`
	Description string    `json:"description"`
	Merchant    string    `json:"merchant"`
	// Amount is always positive; Direction says which way money moved.
	Amount    float64   `json:"amount"`
	Direction Direction `json:"direction"`
	Category  string    `json:"category"`
	CreatedAt time.Time `json:"createdAt"`
}

type CategoryRule struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Pattern   string    `json:"pattern"`
	Category  string    `json:"category"`
	CreatedAt time.Time `json:"createdAt"`
}

type MonthlyFlow struct {
	Month   string  `json:"month"` // YYYY-MM
	Inflow  float64 `json:"inflow"`
	Outflow float64 `json:"outflow"`
}

type CategoryTotal struct {
	Category string  `json:"category"`
	Total    float64 `json:"total"`
	Count    int     `json:"count"`
}

type Summary struct {
	TotalInflow  float64       `json:"totalInflow"`
	TotalOutflow float64       `json:"totalOutflow"`
	Net          float64       `json:"net"`
	Months       []MonthlyFlow `json:"months"`
}
