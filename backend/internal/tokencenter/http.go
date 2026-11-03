package tokencenter

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes returns the catalog directly, without the account API's envelope.
// The caller owns authentication on admin; public deliberately has no auth requirement.
func RegisterRoutes(public, admin *gin.RouterGroup, service *Service) {
	h := &httpHandler{service: service}
	public.GET("/tokencenter/catalog", h.catalog)
	admin.GET("/tokencenter/catalog", h.catalog)
	admin.POST("/tokencenter/import", h.importCatalog)
	admin.POST("/tokencenter/sync", h.syncCatalog)
}

type httpHandler struct{ service *Service }

func (h *httpHandler) catalog(c *gin.Context) {
	catalog, err := h.service.Catalog(c.Request.Context())
	if errors.Is(err, ErrEmpty) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "catalog storage unavailable"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, catalog)
}
func (h *httpHandler) importCatalog(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxCatalogBytes)
	catalog, err := Decode(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	catalog, err = h.service.Import(c.Request.Context(), catalog)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "catalog could not be saved"})
		return
	}
	c.JSON(http.StatusOK, catalog)
}
func (h *httpHandler) syncCatalog(c *gin.Context) {
	catalog, err := h.service.Sync(c.Request.Context())
	if errors.Is(err, ErrNoSource) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "catalog sync failed; last successful snapshot retained"})
		return
	}
	c.JSON(http.StatusOK, catalog)
}

// DevTokenAuth is used only by cmd/tokencenter. The main app keeps its admin auth.
func DevTokenAuth(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		provided := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if token == "" || c.GetHeader("Authorization") == provided || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "admin bearer token required"})
			return
		}
		c.Next()
	}
}
