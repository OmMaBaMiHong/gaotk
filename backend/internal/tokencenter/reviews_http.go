package tokencenter

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func reviewError(c *gin.Context, err error) {
	status, message := http.StatusInternalServerError, "评价服务暂时不可用，请稍后重试"
	switch {
	case errors.Is(err, ErrReviewInvalid):
		status, message = http.StatusUnprocessableEntity, err.Error()
	case errors.Is(err, ErrReviewNotFound):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, ErrReviewUnavailable):
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, gin.H{"error": message})
}

func (h *httpHandler) submitReview(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	var in ReviewInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "评价内容不是有效的 JSON"})
		return
	}
	receipt, err := h.service.SubmitReview(c.Request.Context(), in)
	if err != nil {
		reviewError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, receipt)
}

func (h *httpHandler) objectReviews(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	result, err := h.service.ObjectReviews(c.Request.Context(), c.Query("objectType"), c.Query("objectId"), limit)
	if err != nil {
		reviewError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}

// setReviewStatus is the post-hoc moderation entry (report takedown, appeal
// outcome, restoring the reserved machine filter). Admin-authenticated.
func (h *httpHandler) setReviewStatus(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": ErrReviewInvalid.Error()})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<10)
	var in struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "评价状态不是有效的 JSON"})
		return
	}
	receipt, err := h.service.SetReviewStatus(c.Request.Context(), id, in.Status)
	if err != nil {
		reviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, receipt)
}
