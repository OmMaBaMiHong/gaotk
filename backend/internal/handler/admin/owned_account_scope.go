package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// OwnedAccountScope adapts only explicitly registered owner routes to the original handlers.
func (h *AccountHandler) OwnedAccountScope(channel *ChannelHandler) gin.HandlerFunc {
	return func(c *gin.Context) {
		subject, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok || subject.UserID <= 0 {
			response.Unauthorized(c, "User not authenticated")
			c.Abort()
			return
		}
		var groups service.SavingsReceivingGroups
		if channel != nil {
			groups = channel.channelService
		}
		ctx := service.WithOwnedAccountScope(c.Request.Context(), subject.UserID, groups, h.openaiOAuthService)
		c.Request = c.Request.WithContext(ctx)
		verify := func(id int64) bool {
			account, err := h.adminService.GetAccount(ctx, id)
			if err != nil || service.CheckOwnedAccount(ctx, account) != nil {
				response.NotFound(c, "Account not found")
				c.Abort()
				return false
			}
			return true
		}
		if raw := c.Param("id"); raw != "" {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || id <= 0 {
				response.BadRequest(c, "Invalid account ID")
				c.Abort()
				return
			}
			if !verify(id) {
				return
			}
		}
		if c.Request.Body != nil && c.Request.Method != "GET" && c.Request.Method != "DELETE" {
			body, err := io.ReadAll(io.LimitReader(c.Request.Body, 16*1024*1024+1))
			if err != nil || len(body) > 16*1024*1024 {
				response.BadRequest(c, "Invalid request body")
				c.Abort()
				return
			}
			if len(bytes.TrimSpace(body)) > 0 {
				var payload map[string]any
				decoder := json.NewDecoder(bytes.NewReader(body))
				decoder.UseNumber()
				if err := decoder.Decode(&payload); err != nil {
					response.BadRequest(c, "Invalid JSON request")
					c.Abort()
					return
				}
				// Batch operations must pass an explicit, completely owned set. No admin filters.
				if ids, ok := payload["account_ids"].([]any); ok {
					if len(ids) > 100 {
						response.BadRequest(c, "Too many account IDs")
						c.Abort()
						return
					}
					for _, raw := range ids {
						value, ok := raw.(json.Number)
						if !ok {
							response.BadRequest(c, "Invalid account ID")
							c.Abort()
							return
						}
						id, err := value.Int64()
						if err != nil || id <= 0 {
							response.BadRequest(c, "Invalid account ID")
							c.Abort()
							return
						}
						if !verify(id) {
							return
						}
					}
				}
				if raw, ok := payload["account_id"].(json.Number); ok {
					id, err := raw.Int64()
					if err != nil || id <= 0 {
						response.BadRequest(c, "Invalid account ID")
						c.Abort()
						return
					}
					if !verify(id) {
						return
					}
				}
				sanitizeOwnedAccountPayload(payload)
				body, err = json.Marshal(payload)
				if err != nil {
					response.BadRequest(c, "Invalid request body")
					c.Abort()
					return
				}
			}
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			c.Request.ContentLength = int64(len(body))
		}
		query := c.Request.URL.Query()
		for _, key := range []string{"owner_user_id", "group", "group_id", "include_scheduler_score", "scheduler_score_min", "scheduler_score_max"} {
			query.Del(key)
		}
		c.Request.URL.RawQuery = query.Encode()
		c.Next()
	}
}

func sanitizeOwnedAccountPayload(payload map[string]any) {
	// The shared import validator requires a non-nil proxies array. Preserve that
	// contract while discarding all proxy records from owner-supplied imports.
	if _, ok := payload["proxies"].([]any); ok {
		payload["proxies"] = []any{}
	}
	if extra, ok := payload["extra"].(map[string]any); ok {
		if safe := service.OwnedAccountExtra(extra); len(safe) > 0 {
			payload["extra"] = safe
		} else {
			delete(payload, "extra")
		}
	}
	if credentials, ok := payload["credentials"].(map[string]any); ok {
		payload["credentials"] = service.OwnedAccountCredentials(credentials)
	}
	for _, key := range []string{"owner_user_id", "rental_policy_id", "rental_identity", "rental_status", "proxy_id", "proxy_key", "group_ids", "priority", "concurrency", "rate_multiplier", "load_factor", "credential_extras", "upstream_billing_probe_enabled", "upstream_billing_rate_sync_enabled", "confirm_mixed_channel_risk", "skip_default_group_bind", "filters", "base_url", "credentials_extra"} {
		delete(payload, key)
	}
	if data, ok := payload["data"].(map[string]any); ok {
		sanitizeOwnedAccountPayload(data)
	}
	if accounts, ok := payload["accounts"].([]any); ok {
		for _, item := range accounts {
			if account, ok := item.(map[string]any); ok {
				sanitizeOwnedAccountPayload(account)
			}
		}
	}
}

func redactOwnedAccountDTO(account *dto.Account) *dto.Account {
	if account != nil {
		account.Proxy = nil
		account.Groups = nil
		account.AccountGroups = nil
	}
	return account
}

// An allowlist avoids accidentally exposing new consumer/private DTO fields in owner usage.
func ownedAccountUsageDTO(log *service.UsageLog) map[string]any {
	encoded, _ := json.Marshal(dto.UsageLogFromServiceAdmin(log))
	var all map[string]any
	_ = json.Unmarshal(encoded, &all)
	out := make(map[string]any)
	for _, key := range []string{"id", "account_id", "model", "upstream_model", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "cache_creation_5m_tokens", "cache_creation_1h_tokens", "input_cost", "output_cost", "cache_creation_cost", "cache_read_cost", "total_cost", "actual_cost", "rate_multiplier", "account_rate_multiplier", "account_stats_cost", "billing_type", "billing_mode", "request_type", "stream", "duration_ms", "first_token_ms", "image_count", "image_size", "service_tier", "created_at"} {
		if value, ok := all[key]; ok {
			out[key] = value
		}
	}
	return out
}

func accountDTOForContext(ctx context.Context, account *service.Account) *dto.Account {
	out := dto.AccountFromService(account)
	if service.OwnedAccountUserID(ctx) > 0 {
		redactOwnedAccountDTO(out)
	}
	return out
}
