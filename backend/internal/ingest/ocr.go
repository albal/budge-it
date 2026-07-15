package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/budge-it/backend/internal/models"
)

// OCRClient extracts raw text from a PDF or image statement.
type OCRClient interface {
	ExtractText(ctx context.Context, r io.Reader, filename, contentType string) (string, error)
}

// HTTPOCRClient posts the file to an external OCR service (e.g. a Tesseract
// server or hosted OCR API) as multipart/form-data and expects either a JSON
// body with a "text" field or a plain-text body.
type HTTPOCRClient struct {
	Endpoint string
	APIKey   string
	Client   *http.Client
}

func NewHTTPOCRClient(endpoint, apiKey string) *HTTPOCRClient {
	return &HTTPOCRClient{
		Endpoint: endpoint,
		APIKey:   apiKey,
		Client:   &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *HTTPOCRClient) ExtractText(ctx context.Context, r io.Reader, filename, contentType string) (string, error) {
	if c.Endpoint == "" {
		return "", fmt.Errorf("OCR service not configured: set OCR_ENDPOINT to process PDF/image statements")
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, r); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("OCR request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("OCR service returned %d: %s", resp.StatusCode, snippet)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", err
	}
	var parsed struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parsed) == nil && parsed.Text != "" {
		return parsed.Text, nil
	}
	return string(raw), nil
}

// statementLine matches "  12/03/2024  SOME MERCHANT NAME   -£42.50" style
// lines: a leading date, a description, and a trailing amount.
var statementLine = regexp.MustCompile(
	`^\s*(\d{1,2}[/-]\d{1,2}[/-]\d{2,4}|\d{4}-\d{2}-\d{2}|\d{1,2}\s+[A-Za-z]{3,9}\s+\d{2,4})\s+(.+?)\s+(\(?-?[£$€]?\s?[\d,]+\.\d{2}\)?)(\s*(?:CR|DR|DB))?\s*$`)

// ParseStatementText applies line heuristics to OCR output. Lines that don't
// look like transactions (headers, balances, footers) are skipped.
func ParseStatementText(text string) ([]ParsedTxn, error) {
	var txns []ParsedTxn
	for _, line := range strings.Split(text, "\n") {
		m := statementLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		date, ok := parseDate(normalizeSpaces(m[1]))
		if !ok {
			continue
		}
		desc := strings.TrimSpace(strings.Trim(m[2], "*"))
		lower := strings.ToLower(desc)
		if desc == "" || strings.Contains(lower, "balance") || strings.Contains(lower, "statement") {
			continue
		}
		amount, neg, err := cleanAmount(m[3])
		if err != nil || amount == 0 {
			continue
		}
		dir := models.Debit
		marker := strings.TrimSpace(m[4])
		switch {
		case marker == "CR":
			dir = models.Credit
		case marker == "DR" || marker == "DB" || neg:
			dir = models.Debit
		case !neg && marker == "":
			// Unmarked positive amounts on bank statements are usually debits;
			// obvious income keywords flip it.
			if strings.Contains(lower, "salary") || strings.Contains(lower, "payroll") ||
				strings.Contains(lower, "deposit") || strings.Contains(lower, "refund") {
				dir = models.Credit
			}
		}
		txns = append(txns, ParsedTxn{Date: date, Description: desc, Amount: amount, Direction: dir})
	}
	if len(txns) == 0 {
		return nil, fmt.Errorf("no transactions recognized in OCR text")
	}
	return txns, nil
}

func normalizeSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
