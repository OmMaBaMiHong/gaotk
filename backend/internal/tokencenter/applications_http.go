package tokencenter

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func applicationError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	message := "申请服务暂时不可用，请稍后重试"
	if errors.Is(err, ErrApplicationNotFound) {
		status = http.StatusNotFound
		message = err.Error()
	}
	if errors.Is(err, ErrApplicationConflict) {
		status = http.StatusConflict
		message = err.Error()
	}
	if errors.Is(err, ErrApplicationUnavailable) {
		status = http.StatusServiceUnavailable
	}
	if errors.Is(err, ErrApplicationInvalid) {
		status = http.StatusUnprocessableEntity
		message = err.Error()
	}
	c.JSON(status, gin.H{"error": message})
}
func (h *httpHandler) submitApplication(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var in ApplicationInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": "申请内容不是有效的 JSON"})
		return
	}
	if _, err := normalizeApplication(in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	receipt, err := h.service.SubmitApplication(c.Request.Context(), in)
	if err != nil {
		applicationError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, receipt)
}
func (h *httpHandler) applicationStatus(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var in struct {
		Receipt string `json:"receipt"`
	}
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Receipt) == "" {
		c.JSON(400, gin.H{"error": "请填写申请查询凭证"})
		return
	}
	receipt, err := h.service.ApplicationStatus(c.Request.Context(), strings.TrimSpace(in.Receipt))
	if err != nil {
		applicationError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, receipt)
}
func (h *httpHandler) listApplications(c *gin.Context) {
	status := c.Query("status")
	if status != "" && !applicationStatus(status) {
		c.JSON(400, gin.H{"error": "未知申请状态"})
		return
	}
	var before int64
	if raw := c.Query("before"); raw != "" {
		var err error
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before <= 0 {
			c.JSON(400, gin.H{"error": "无效分页位置"})
			return
		}
	}
	store, err := h.service.applications()
	if err != nil {
		applicationError(c, err)
		return
	}
	page, err := store.ListApplications(c.Request.Context(), status, before)
	if err != nil {
		applicationError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, page)
}
func (h *httpHandler) reviewApplication(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"error": "无效申请编号"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var in ApplicationReview
	if c.ShouldBindJSON(&in) != nil || in.Revision <= 0 || !applicationStatus(in.Status) || strings.TrimSpace(in.Note) == "" {
		c.JSON(400, gin.H{"error": "请填写处理状态、说明和当前版本"})
		return
	}
	row, err := h.service.ReviewApplication(c.Request.Context(), id, in, "admin")
	if err != nil {
		applicationError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, row)
}
