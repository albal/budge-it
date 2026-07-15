package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/budge-it/backend/internal/api"
	"github.com/budge-it/backend/internal/config"
	"github.com/budge-it/backend/internal/db"
	"github.com/budge-it/backend/internal/ingest"
	"github.com/budge-it/backend/internal/jobs"
	"github.com/budge-it/backend/internal/objectstore"
	"github.com/budge-it/backend/internal/store"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	gin.SetMode(gin.ReleaseMode)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		slog.Error("migrations", "error", err)
		os.Exit(1)
	}

	var objects objectstore.Store
	switch {
	case cfg.BucketConfigured():
		objects, err = objectstore.NewS3Store(cfg.S3Endpoint(), cfg.S3AccessKey, cfg.S3SecretKey, cfg.BucketName, cfg.S3UseSSL)
		if err != nil {
			slog.Error("object storage", "error", err)
			os.Exit(1)
		}
		slog.Info("using S3 object storage", "endpoint", cfg.S3Endpoint(), "bucket", cfg.BucketName)
	case cfg.LocalStorageDir != "":
		objects, err = objectstore.NewLocalStore(cfg.LocalStorageDir)
		if err != nil {
			slog.Error("local storage", "error", err)
			os.Exit(1)
		}
		slog.Warn("using local filesystem storage (development only)", "dir", cfg.LocalStorageDir)
	default:
		slog.Error("no storage configured: set BUCKET_HOST/BUCKET_NAME (ObjectBucketClaim) or LOCAL_STORAGE_DIR")
		os.Exit(1)
	}

	st := store.New(pool)
	ocr := ingest.NewHTTPOCRClient(cfg.OCREndpoint, cfg.OCRAPIKey)
	queue := jobs.NewQueue(st, objects, ocr)
	queue.Start(ctx, cfg.Workers)

	server := api.NewServer(cfg, st, objects, queue, func() error {
		pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return pool.Ping(pingCtx)
	})

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("budge-it backend listening", "port", cfg.Port, "workers", cfg.Workers)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "error", err)
	}
	queue.Wait()
}
