package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/budge-it/backend/internal/models"
)

// adminStore is the subset of *store.Store the administration endpoints use.
// Narrowing it to an interface keeps these handlers unit-testable without a
// live PostgreSQL.
type adminStore interface {
	ListUsers(ctx context.Context) ([]*models.AdminUser, error)
	GetUserByID(ctx context.Context, id string) (*models.User, error)
	DeleteUser(ctx context.Context, id string) (bool, error)
}

// isAdmin reports whether the authenticated caller is an administrator:
// either they hold the flag claimed by the first login, or their address is
// on the ADMIN_EMAILS allowlist (the out-of-band override).
//
// The flag is read from the database rather than carried in the session so
// that granting or revoking it takes effect immediately, without waiting for
// the 30-day cookie to expire.
func (s *Server) isAdmin(c *gin.Context) (bool, error) {
	email, _ := c.MustGet("email").(string)
	if s.cfg.IsAdmin(email) {
		return true, nil
	}
	u, err := s.admin.GetUserByID(c.Request.Context(), userID(c))
	if err != nil {
		// A session for a deleted account is not an error, just not an admin.
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return u.IsAdmin, nil
}

// requireAdmin gates the /admin routes. It runs after requireAuth, so the
// identity in context is already session-verified.
//
// Note this is authorization only: login is passwordless, so anyone able to
// supply the right address can reach these endpoints. See the security note
// in the README.
func (s *Server) requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		ok, err := s.isAdmin(c)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "administrator access required"})
			return
		}
		c.Next()
	}
}

func (s *Server) listUsers(c *gin.Context) {
	users, err := s.admin.ListUsers(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, users)
}

// deleteUser removes an account and all of its data. Deleting yourself is
// refused: it would revoke your own access mid-session and, since accounts are
// created on first login, silently resurrect an empty one on the next request.
func (s *Server) deleteUser(c *gin.Context) {
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	if id == userID(c) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "you cannot delete your own account"})
		return
	}

	// Look the user up first so a genuine 404 is distinguishable from a
	// delete that raced with another administrator.
	u, err := s.admin.GetUserByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	deleted, err := s.admin.DeleteUser(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !deleted {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true, "id": id, "email": u.Email})
}
