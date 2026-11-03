package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/tokencenter"
	"github.com/gin-gonic/gin"
)

func TestOptionalTokenCenterRoutesDisabledWithoutDedicatedDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	// A disabled catalog must not construct account middleware or register any
	// handlers that would dereference the absent service.
	registerTokenCenterRoutes(router, nil, nil, nil, nil, nil, nil)
	if len(router.Routes()) != 1 {
		t.Fatal("disabled TokenCenter added routes to the account server")
	}
	for path, want := range map[string]int{"/health": http.StatusOK, "/api/v1/tokencenter/catalog": http.StatusNotFound} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != want {
			t.Fatalf("disabled TokenCenter changed %s: status %d", path, response.Code)
		}
	}
}

func TestOptionalTokenCenterRoutesPreserveConfiguredEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, err := tokencenter.NewService(nil, tokencenter.Config{Interval: time.Minute, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	registerTokenCenterRoutes(router, &config.Config{}, nil, nil, nil, nil, service)
	want := map[string]bool{
		"GET /api/v1/tokencenter/catalog":       true,
		"GET /api/v1/admin/tokencenter/catalog": true,
		"POST /api/v1/admin/tokencenter/import": true,
		"POST /api/v1/admin/tokencenter/sync":   true,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("configured TokenCenter endpoints missing: %v", want)
	}
}
