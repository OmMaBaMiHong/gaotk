package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// CheckinHandler 每日签到处理器（薄壳：鉴权取 userID 后透传给 service）。
type CheckinHandler struct {
	checkinService *service.CheckinService
}

// NewCheckinHandler 创建签到处理器
func NewCheckinHandler(checkinService *service.CheckinService) *CheckinHandler {
	return &CheckinHandler{
		checkinService: checkinService,
	}
}

// GetStatus 返回签到面板数据
// GET /api/v1/user/checkin
func (h *CheckinHandler) GetStatus(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	status, err := h.checkinService.Status(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, status)
}

// Checkin 执行今日签到
// POST /api/v1/user/checkin
func (h *CheckinHandler) Checkin(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	result, err := h.checkinService.Checkin(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, result)
}
