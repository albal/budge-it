// Package api wires the HTTP surface: upload intake, transactions, analytics
// and health/metrics endpoints.
package api

import (
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/budge-it/backend/internal/categorize"
	"github.com/budge-it/backend/internal/config"
	"github.com/budge-it/backend/internal/jobs"
	"github.com/budge-it/backend/internal/metrics"
	"github.com/budge-it/backend/internal/models"
	"github.com/budge-it/backend/internal/objectstore"
	"github.com/budge-it/backend/internal/store"
)

var allowedTypes = map[string]bool{
	"text/csv":                 true,
	"application/vnd.ms-excel": true, // some browsers tag CSVs this way
	"application/pdf":          true,
	"image/jpeg":               true,
	"image/png":                true,
}

var allowedExts = map[string]bool{
	".csv": true, ".pdf": true, ".jpg": true, ".jpeg": true, ".png": true,
}

type Server struct {
	cfg     *config.Config
	store   *store.Store
	objects objectstore.Store
	queue   *jobs.Queue
	ready   func() error
}

func NewServer(cfg *config.Config, st *store.Store, objects objectstore.Store, queue *jobs.Queue, ready func() error) *Server {
	return &Server{cfg: cfg, store: st, objects: objects, queue: queue, ready: ready}
}

func (s *Server) Router() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), metrics.GinMiddleware())
	r.MaxMultipartMemory = 8 << 20

	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/readyz", func(c *gin.Context) {
		if err := s.ready(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	v1 := r.Group("/api/v1")
	{
		v1.POST("/uploads", s.createUpload)
		v1.GET("/uploads", s.listUploads)
		v1.GET("/uploads/:id", s.getUpload)
		v1.GET("/transactions", s.listTransactions)
		v1.PATCH("/transactions/:id", s.recategorize)
		v1.GET("/categories", s.listCategories)
		v1.GET("/rules", s.listRules)
		v1.GET("/analytics/summary", s.summary)
		v1.GET("/analytics/categories", s.categoryBreakdown)
	}
	return r
}

func userID(*gin.Context) string {
	// Single-tenant for now; swap for the authenticated subject when auth lands.
	return store.DefaultUserID
}

func (s *Server) createUpload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, s.cfg.MaxUploadBytes+1<<20)

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "multipart 'file' field is required"})
		return
	}
	defer file.Close()

	if header.Size > s.cfg.MaxUploadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": fmt.Sprintf("file exceeds %d MB limit", s.cfg.MaxUploadBytes>>20)})
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	contentType := header.Header.Get("Content-Type")
	if mt, _, err := mime.ParseMediaType(contentType); err == nil {
		contentType = mt
	}
	if !allowedTypes[contentType] && !allowedExts[ext] {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{
			"error": "unsupported file type: upload CSV, PDF, JPEG or PNG"})
		return
	}

	key := uuid.NewString() + ext
	if err := s.objects.Put(c.Request.Context(), key, file, header.Size, contentType); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "storing file: " + err.Error()})
		return
	}

	up := &models.Upload{
		UserID:      userID(c),
		Filename:    filepath.Base(header.Filename),
		ContentType: contentType,
		SizeBytes:   header.Size,
		Status:      models.UploadPending,
		ObjectKey:   key,
	}
	if err := s.store.CreateUpload(c.Request.Context(), up); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	metrics.UploadsTotal.WithLabelValues(contentType).Inc()
	s.queue.Enqueue(up.ID)
	c.JSON(http.StatusAccepted, up)
}

func (s *Server) listUploads(c *gin.Context) {
	uploads, err := s.store.ListUploads(c.Request.Context(), userID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, uploads)
}

func (s *Server) getUpload(c *gin.Context) {
	up, err := s.store.GetUpload(c.Request.Context(), userID(c), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "upload not found"})
		return
	}
	c.JSON(http.StatusOK, up)
}

func (s *Server) listTransactions(c *gin.Context) {
	txns, err := s.store.ListTransactions(c.Request.Context(), userID(c), store.TxnFilter{
		Month:    c.Query("month"),
		Category: c.Query("category"),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, txns)
}

type recategorizeReq struct {
	Category   string `json:"category" binding:"required"`
	CreateRule bool   `json:"createRule"`
	// Pattern optionally narrows the persisted rule; defaults to the
	// transaction's normalized merchant.
	Pattern string `json:"pattern"`
}

func (s *Server) recategorize(c *gin.Context) {
	var req recategorizeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !slices.Contains(categorize.Categories, req.Category) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown category"})
		return
	}
	uid := userID(c)
	merchant, err := s.store.RecategorizeTransaction(c.Request.Context(), uid, c.Param("id"), req.Category)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "transaction not found"})
		return
	}
	if req.CreateRule {
		pattern := categorize.Normalize(req.Pattern)
		if pattern == "" {
			pattern = merchant
		}
		if err := s.store.UpsertRule(c.Request.Context(), uid, pattern, req.Category); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "saving rule: " + err.Error()})
			return
		}
		if err := s.store.ApplyRuleToUncategorized(c.Request.Context(), uid, pattern, req.Category); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "applying rule: " + err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "category": req.Category, "ruleSaved": req.CreateRule})
}

func (s *Server) listCategories(c *gin.Context) {
	c.JSON(http.StatusOK, categorize.Categories)
}

func (s *Server) listRules(c *gin.Context) {
	rules, err := s.store.ListRules(c.Request.Context(), userID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rules)
}

func (s *Server) summary(c *gin.Context) {
	sum, err := s.store.Summary(c.Request.Context(), userID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sum)
}

func (s *Server) categoryBreakdown(c *gin.Context) {
	breakdown, err := s.store.CategoryBreakdown(c.Request.Context(), userID(c), c.Query("month"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, breakdown)
}
