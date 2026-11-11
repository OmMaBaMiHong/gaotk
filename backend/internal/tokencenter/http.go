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
	public.GET("/tokencenter/models/:id", h.model)
	admin.GET("/tokencenter/catalog", h.catalog)
	admin.POST("/tokencenter/import", h.importCatalog)
	admin.POST("/tokencenter/sync", h.syncCatalog)
	public.POST("/tokencenter/applications", h.submitApplication)
	public.POST("/tokencenter/applications/status", h.applicationStatus)
	admin.GET("/tokencenter/applications", h.listApplications)
	admin.POST("/tokencenter/applications/:id/review", h.reviewApplication)
}

type httpHandler struct{ service *Service }

func (h *httpHandler) catalog(c *gin.Context) {
	catalog, ok := h.readCatalog(c, c.Query("view") == "browse")
	if !ok {
		return
	}
	c.JSON(http.StatusOK, projectModelBoard(catalog, c.Query("ranking")))
}

func (h *httpHandler) model(c *gin.Context) {
	catalog, ok := h.readCatalog(c, false)
	if !ok {
		return
	}
	for _, model := range projectModelBoard(catalog, c.Query("ranking")).Models {
		if model.ID == c.Param("id") {
			c.JSON(http.StatusOK, model)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "model not found"})
}

func (h *httpHandler) readCatalog(c *gin.Context, browse bool) (Catalog, bool) {
	var catalog Catalog
	var err error
	if id := c.Param("id"); id != "" {
		catalog, err = h.service.ModelCatalog(c.Request.Context(), id)
	} else if browse {
		catalog, err = h.service.BrowseCatalog(c.Request.Context())
	} else {
		catalog, err = h.service.Catalog(c.Request.Context())
	}
	if errors.Is(err, ErrEmpty) || errors.Is(err, ErrSourceRevoked) || errors.Is(err, ErrAdmissionPending) {
		code := "not_collected"
		if errors.Is(err, ErrSourceRevoked) {
			code = "source_revoked"
		}
		if errors.Is(err, ErrAdmissionPending) {
			code = "admission_pending"
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error(), "code": code})
		return Catalog{}, false
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "catalog storage unavailable"})
		return Catalog{}, false
	}
	c.Header("Cache-Control", "no-store")
	return catalog, true
}
func (h *httpHandler) importCatalog(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxCatalogBytes)
	catalog, err := Decode(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	catalog, err = h.service.Import(c.Request.Context(), catalog)
	if errors.Is(err, ErrAdmissionPending) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "admission_pending", "error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "catalog could not be saved"})
		return
	}
	c.JSON(http.StatusOK, catalog)
}
func (h *httpHandler) syncCatalog(c *gin.Context) {
	catalog, err := h.service.Sync(c.Request.Context())
	if errors.Is(err, ErrSourceRevoked) || errors.Is(err, ErrAdmissionPending) {
		code := "source_revoked"
		if errors.Is(err, ErrAdmissionPending) {
			code = "admission_pending"
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error(), "code": code})
		return
	}
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
