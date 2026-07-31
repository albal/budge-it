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
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"github.com/budge-it/backend/internal/auth"
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
	// admin is the subset of the store the administration endpoints use,
	// kept as an interface so those handlers can be tested without a
	// database. It points at store in production.
	admin adminStore
}

func NewServer(cfg *config.Config, st *store.Store, objects objectstore.Store, queue *jobs.Queue, ready func() error) *Server {
	return &Server{cfg: cfg, store: st, objects: objects, queue: queue, ready: ready, admin: st}
}

func (s *Server) Router() *gin.Engine {
	r := gin.New()
	// otelgin emits a trace span per request to the OTLP endpoint (Elastic
	// APM); health/readiness probes and metric scrapes would drown out real
	// traffic, so they're filtered.
	r.Use(gin.Recovery(), metrics.GinMiddleware(), otelgin.Middleware("budgeit-backend",
		otelgin.WithGinFilter(func(c *gin.Context) bool {
			switch c.FullPath() {
			case "/healthz", "/readyz", "/metrics":
				return false
			}
			return true
		})))
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
		v1.POST("/auth/login", s.login)
		v1.GET("/version", s.version)

		authed := v1.Group("")
		authed.Use(s.requireAuth())
		{
			authed.POST("/auth/logout", s.logout)
			authed.GET("/auth/me", s.me)
			authed.POST("/uploads", s.createUpload)
			authed.GET("/uploads", s.listUploads)
			authed.GET("/uploads/:id", s.getUpload)
			authed.GET("/transactions", s.listTransactions)
			authed.DELETE("/transactions", s.clearTransactions)
			authed.PATCH("/transactions/:id", s.recategorize)
			authed.GET("/categories", s.listCategories)
			authed.POST("/categories", s.addCategory)
			authed.GET("/rules", s.listRules)
			authed.GET("/analytics/summary", s.summary)
			authed.GET("/analytics/categories", s.categoryBreakdown)

			// Administration: authenticated *and* on the ADMIN_EMAILS
			// allowlist. Nested under authed so an anonymous caller gets
			// 401 rather than leaking that the routes exist.
			admin := authed.Group("/admin")
			admin.Use(s.requireAdmin())
			{
				admin.GET("/users", s.listUsers)
				admin.DELETE("/users/:id", s.deleteUser)
			}
		}
	}
	return r
}

// requireAuth verifies the session cookie and stashes the authenticated
// user's ID/email in the request context; it aborts with 401 otherwise.
func (s *Server) requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie(auth.CookieName)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
			return
		}
		uid, email, err := auth.Verify(s.cfg.SessionSecret, cookie)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
			return
		}
		c.Set("userID", uid)
		c.Set("email", email)
		c.Next()
	}
}

func userID(c *gin.Context) string {
	return c.MustGet("userID").(string)
}

// secureCookie reports whether the request arrived over TLS, directly or via
// the OpenShift Route's edge termination (which forwards this header).
func secureCookie(c *gin.Context) bool {
	return c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
}

type loginReq struct {
	Email string `json:"email" binding:"required"`
}

// login has no password: providing an email is sufficient to sign in as it,
// creating the account on first use.
func (s *Server) login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	email := strings.TrimSpace(req.Email)
	if !strings.Contains(email, "@") || len(email) > 254 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "enter a valid email address"})
		return
	}
	u, err := s.store.GetOrCreateUserByEmail(c.Request.Context(), email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	token := auth.Sign(s.cfg.SessionSecret, u.ID, u.Email, auth.TTL)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(auth.CookieName, token, int(auth.TTL.Seconds()), "/", "", secureCookie(c), true)
	c.JSON(http.StatusOK, u)
}

func (s *Server) logout(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(auth.CookieName, "", -1, "/", "", secureCookie(c), true)
	c.JSON(http.StatusOK, gin.H{"loggedOut": true})
}

func (s *Server) me(c *gin.Context) {
	// isAdmin drives whether the frontend offers the admin link; the server
	// re-checks it on every /admin request regardless. A lookup failure
	// shouldn't break the session check, so it degrades to "not an admin".
	admin, err := s.isAdmin(c)
	if err != nil {
		admin = false
	}
	c.JSON(http.StatusOK, gin.H{
		"id":      userID(c),
		"email":   c.MustGet("email").(string),
		"isAdmin": admin,
	})
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
	Category string `json:"category" binding:"required"`
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
	cats, err := s.allCategories(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !slices.Contains(cats, req.Category) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown category"})
		return
	}
	uid := userID(c)
	merchant, err := s.store.RecategorizeTransaction(c.Request.Context(), uid, c.Param("id"), req.Category)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "transaction not found"})
		return
	}
	// A manual re-categorization is a statement about the merchant, not just
	// this one line: persist it as a rule (so future ingests use it) and
	// retag every existing transaction with the same merchant.
	pattern := categorize.Normalize(req.Pattern)
	if pattern == "" {
		pattern = merchant
	}
	if err := s.store.UpsertRule(c.Request.Context(), uid, pattern, req.Category); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "saving rule: " + err.Error()})
		return
	}
	if err := s.store.RecategorizeByMerchant(c.Request.Context(), uid, pattern, req.Category); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "applying rule: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "category": req.Category, "ruleSaved": true})
}

// clearTransactions wipes the user's uploads; transactions go with them via
// ON DELETE CASCADE. Category rules survive so re-uploaded statements come
// back with the user's categorization intact.
func (s *Server) clearTransactions(c *gin.Context) {
	if err := s.store.ClearTransactions(c.Request.Context(), userID(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"cleared": true})
}

func (s *Server) version(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"commit":             s.cfg.Version,
		"kibanaUrl":          s.cfg.KibanaURL,
		"kibanaDashboardUrl": s.cfg.KibanaDashboardURL,
	})
}

// allCategories is the built-in list plus the user's custom categories.
func (s *Server) allCategories(c *gin.Context) ([]string, error) {
	custom, err := s.store.ListCustomCategories(c.Request.Context(), userID(c))
	if err != nil {
		return nil, err
	}
	return append(append([]string{}, categorize.Categories...), custom...), nil
}

func (s *Server) listCategories(c *gin.Context) {
	cats, err := s.allCategories(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cats)
}

func (s *Server) addCategory(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 40 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "category name must be 1-40 characters"})
		return
	}
	for _, b := range categorize.Categories {
		if strings.EqualFold(b, name) {
			c.JSON(http.StatusConflict, gin.H{"error": "category already exists"})
			return
		}
	}
	inserted, err := s.store.AddCustomCategory(c.Request.Context(), userID(c), name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !inserted {
		c.JSON(http.StatusConflict, gin.H{"error": "category already exists"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"name": name})
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
