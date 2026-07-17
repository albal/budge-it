// Package jobs runs asynchronous statement processing: fetch the staged file,
// extract transactions (CSV parse or OCR), categorize, persist, then purge
// the staged object.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/budge-it/backend/internal/categorize"
	"github.com/budge-it/backend/internal/ingest"
	"github.com/budge-it/backend/internal/metrics"
	"github.com/budge-it/backend/internal/models"
	"github.com/budge-it/backend/internal/objectstore"
	"github.com/budge-it/backend/internal/store"
)

type Queue struct {
	store   *store.Store
	objects objectstore.Store
	ocr     ingest.OCRClient
	jobs    chan string
	wg      sync.WaitGroup
}

func NewQueue(st *store.Store, objects objectstore.Store, ocr ingest.OCRClient) *Queue {
	return &Queue{store: st, objects: objects, ocr: ocr, jobs: make(chan string, 256)}
}

// Enqueue schedules an upload for processing. Non-blocking; if the buffer is
// full the upload stays 'pending' and is recovered on next restart.
func (q *Queue) Enqueue(uploadID string) {
	select {
	case q.jobs <- uploadID:
		metrics.QueueDepth.Set(float64(len(q.jobs)))
	default:
		slog.Warn("job queue full, upload left pending for recovery", "upload", uploadID)
	}
}

// Start launches n workers and re-enqueues uploads that were interrupted by a
// previous shutdown.
func (q *Queue) Start(ctx context.Context, n int) {
	for i := 0; i < n; i++ {
		q.wg.Add(1)
		go func() {
			defer q.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case id := <-q.jobs:
					metrics.QueueDepth.Set(float64(len(q.jobs)))
					q.process(ctx, id)
				}
			}
		}()
	}

	pending, err := q.store.PendingUploadIDs(ctx)
	if err != nil {
		slog.Error("recovering pending uploads", "error", err)
		return
	}
	for _, id := range pending {
		q.Enqueue(id)
	}
	if len(pending) > 0 {
		slog.Info("re-enqueued interrupted uploads", "count", len(pending))
	}
}

func (q *Queue) Wait() { q.wg.Wait() }

func (q *Queue) process(ctx context.Context, uploadID string) {
	start := time.Now()
	up, err := q.store.GetUploadByID(ctx, uploadID)
	if err != nil {
		slog.Error("job: load upload", "upload", uploadID, "error", err)
		return
	}
	if up.Status == models.UploadDone {
		return
	}
	if err := q.store.MarkUploadProcessing(ctx, up.ID); err != nil {
		slog.Error("job: mark processing", "upload", up.ID, "error", err)
		return
	}

	txnCount, err := q.extract(ctx, up)
	metrics.JobDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		slog.Error("job failed", "upload", up.ID, "file", up.Filename, "error", err)
		metrics.JobsProcessed.WithLabelValues("error").Inc()
		if dbErr := q.store.MarkUploadError(ctx, up.ID, err.Error()); dbErr != nil {
			slog.Error("job: mark error", "upload", up.ID, "error", dbErr)
		}
		return
	}

	if err := q.store.MarkUploadDone(ctx, up.ID, txnCount); err != nil {
		slog.Error("job: mark done", "upload", up.ID, "error", err)
		return
	}
	// Privacy requirement: purge the staged statement immediately after
	// successful extraction.
	if up.ObjectKey != "" {
		if err := q.objects.Delete(ctx, up.ObjectKey); err != nil {
			slog.Error("job: purge staged object", "upload", up.ID, "key", up.ObjectKey, "error", err)
		}
	}
	metrics.JobsProcessed.WithLabelValues("done").Inc()
	metrics.TransactionsExtracted.Add(float64(txnCount))
	// Non-fatal: the upload is already done; pairing retries on next upload.
	if err := q.markTransfers(ctx, up.UserID); err != nil {
		slog.Warn("transfer detection", "upload", up.ID, "error", err)
	}
	slog.Info("processed statement", "upload", up.ID, "file", up.Filename,
		"transactions", txnCount, "took", time.Since(start).Round(time.Millisecond))
}

func (q *Queue) extract(ctx context.Context, up *models.Upload) (int, error) {
	obj, err := q.objects.Get(ctx, up.ObjectKey)
	if err != nil {
		return 0, fmt.Errorf("fetching staged file: %w", err)
	}
	defer obj.Close()

	var parsed []ingest.ParsedTxn
	if isCSV(up) {
		parsed, err = ingest.ParseCSV(obj)
	} else {
		var text string
		text, err = q.ocr.ExtractText(ctx, obj, up.Filename, up.ContentType)
		if err == nil {
			parsed, err = ingest.ParseStatementText(text)
		}
	}
	if err != nil {
		return 0, err
	}

	rules, err := q.store.ListRules(ctx, up.UserID)
	if err != nil {
		return 0, err
	}
	engineRules := make([]categorize.Rule, 0, len(rules))
	for _, r := range rules {
		engineRules = append(engineRules, categorize.Rule{Pattern: r.Pattern, Category: r.Category})
	}
	engine := categorize.NewEngine(engineRules)

	txns := make([]*models.Transaction, 0, len(parsed))
	for _, p := range parsed {
		category := engine.Categorize(p.Description)
		if p.Direction == models.Credit && category == categorize.Uncategorized {
			category = "Income"
		}
		txns = append(txns, &models.Transaction{
			UserID:      up.UserID,
			UploadID:    up.ID,
			Date:        p.Date,
			Description: p.Description,
			Merchant:    categorize.Normalize(p.Description),
			Amount:      p.Amount,
			Direction:   p.Direction,
			Category:    category,
		})
	}
	if err := q.store.InsertTransactions(ctx, txns); err != nil {
		return 0, fmt.Errorf("saving transactions: %w", err)
	}
	return len(txns), nil
}

// markTransfers re-pairs the user's transactions after new ones arrive: a
// debit and a credit of the same amount in different statements within a few
// days of each other are the two sides of a transfer between the user's own
// accounts, not income or spending.
func (q *Queue) markTransfers(ctx context.Context, userID string) error {
	cands, err := q.store.TransferCandidates(ctx, userID)
	if err != nil {
		return err
	}
	rules, err := q.store.ListRules(ctx, userID)
	if err != nil {
		return err
	}
	engineRules := make([]categorize.Rule, 0, len(rules))
	for _, r := range rules {
		engineRules = append(engineRules, categorize.Rule{
			Pattern: categorize.Normalize(r.Pattern), Category: r.Category,
		})
	}
	eligible := cands[:0]
	for _, c := range cands {
		if !c.RuledByUser(engineRules) {
			eligible = append(eligible, c)
		}
	}
	ids := categorize.MatchTransfers(eligible)
	if len(ids) == 0 {
		return nil
	}
	if err := q.store.SetTransactionsCategory(ctx, userID, ids, categorize.Transfers); err != nil {
		return err
	}
	slog.Info("marked transfer pairs", "transactions", len(ids))
	return nil
}

func isCSV(up *models.Upload) bool {
	return strings.Contains(up.ContentType, "csv") ||
		strings.HasSuffix(strings.ToLower(up.Filename), ".csv")
}
