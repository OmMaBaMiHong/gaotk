package tokencenter

// Usage is TokenCenter-owned business data. It is independent of the Chaos
// catalog publication and never changes natural rankings or source facts.
import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type ServiceUsage struct {
	Total     int64     `json:"total"`
	Boards    int64     `json:"boards"`
	Models    int64     `json:"models"`
	Platforms int64     `json:"platforms"`
	StartedAt time.Time `json:"startedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type usageStore interface {
	ServiceUsage(context.Context) (ServiceUsage, error)
	RecordServiceClick(context.Context, string) (ServiceUsage, error)
}

var errUsageUnavailable = errors.New("service usage storage unavailable")

const usageColumns = `board_clicks + model_clicks + platform_clicks, board_clicks, model_clicks, platform_clicks, started_at, updated_at`

func scanUsage(row interface{ Scan(...any) error }) (ServiceUsage, error) {
	var u ServiceUsage
	err := row.Scan(&u.Total, &u.Boards, &u.Models, &u.Platforms, &u.StartedAt, &u.UpdatedAt)
	return u, err
}

func (s *SQLStore) ServiceUsage(ctx context.Context) (ServiceUsage, error) {
	return scanUsage(s.db.QueryRowContext(ctx, `SELECT `+usageColumns+` FROM tokencenter_service_usage WHERE id = 1`))
}

func (s *SQLStore) RecordServiceClick(ctx context.Context, kind string) (ServiceUsage, error) {
	// One atomic single-row update, so concurrent clicks cannot overwrite one
	// another. Repeat clicks intentionally count; no person/visitor identity is stored.
	return scanUsage(s.db.QueryRowContext(ctx, `UPDATE tokencenter_service_usage SET
		board_clicks = board_clicks + CASE WHEN $1 = 'board' THEN 1 ELSE 0 END,
		model_clicks = model_clicks + CASE WHEN $1 = 'model' THEN 1 ELSE 0 END,
		platform_clicks = platform_clicks + CASE WHEN $1 = 'platform' THEN 1 ELSE 0 END,
		updated_at = NOW() WHERE id = 1 RETURNING `+usageColumns, kind))
}

func (h *httpHandler) usage(c *gin.Context) {
	store, ok := h.service.store.(usageStore)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errUsageUnavailable.Error()})
		return
	}
	var usage ServiceUsage
	var err error
	if c.Request.Method == http.MethodPost {
		var input struct {
			Kind string `json:"kind"`
		}
		if c.ShouldBindJSON(&input) != nil || (input.Kind != "board" && input.Kind != "model" && input.Kind != "platform") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "kind must be board, model or platform"})
			return
		}
		usage, err = store.RecordServiceClick(c.Request.Context(), input.Kind)
	} else {
		usage, err = store.ServiceUsage(c.Request.Context())
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errUsageUnavailable.Error()})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, usage)
}
