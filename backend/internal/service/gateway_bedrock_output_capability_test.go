package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBedrockOutputCapabilityForwardWire(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, field, metadataModel string
		metadataLimit, want        int64
		missing                    bool
	}{
		{name: "explicit above metadata", field: `,"max_tokens":131072`, metadataModel: "claude-sonnet-4-5", metadataLimit: 42000, want: 131072},
		{name: "observed alias capability", metadataModel: "claude-sonnet-4-5", metadataLimit: 125000, want: 125000},
		{name: "observed resolved capability", metadataModel: "us.anthropic.claude-sonnet-4-5-20250929-v1:0", metadataLimit: 196608, want: 196608},
		{name: "no observed capability", missing: true},
		{name: "zero is not observed capability", metadataModel: "claude-sonnet-4-5", missing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"fixture","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))}}}
			svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			account := &Account{Platform: PlatformAnthropic, Type: AccountTypeBedrock, Credentials: map[string]any{"auth_mode": "apikey", "api_key": "fixture", "aws_region": "us-east-1"}}
			if tc.metadataModel != "" {
				account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{tc.metadataModel: {ID: tc.metadataModel, MaxOutputTokens: tc.metadataLimit}}})
			}
			body := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"ok"}]` + tc.field + `}`)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
			_, err := svc.forwardBedrock(context.Background(), c, account, &ParsedRequest{Model: "claude-sonnet-4-5", Body: NewRequestBodyRef(body)}, time.Now())
			if tc.missing {
				require.ErrorContains(t, err, "requires max_tokens")
				require.Empty(t, upstream.requestBodies)
				return
			}
			require.NoError(t, err)
			require.Len(t, upstream.requestBodies, 1)
			require.Equal(t, tc.want, gjson.GetBytes(upstream.requestBodies[0], "max_tokens").Int())
		})
	}
}

func TestBedrockCCCompatOutputCapability(t *testing.T) {
	groupID := int64(1)
	channels := &ChannelService{}
	channels.cache.Store(&channelCache{loadedAt: time.Now(), channelByGroupID: map[int64]*Channel{groupID: {Status: StatusActive, FeaturesConfig: map[string]any{featureKeyBedrockCCCompat: true}}}})
	svc := &GatewayService{channelService: channels}
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeBedrock}
	for _, limit := range []int64{0, 125000} {
		account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{"claude-sonnet-4-5": {ID: "claude-sonnet-4-5", MaxOutputTokens: limit}}})
		for _, explicit := range []bool{false, true} {
			body := []byte(`{"model":"claude-sonnet-4-5","messages":[]}`)
			if explicit {
				body = []byte(`{"model":"claude-sonnet-4-5","messages":[],"max_tokens":131072}`)
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			result := svc.ApplyBedrockCCCompat(c, body, "claude-sonnet-4-5", account, &groupID)
			if explicit {
				require.Equal(t, int64(131072), gjson.GetBytes(result, "max_tokens").Int())
			} else if limit > 0 {
				require.Equal(t, limit, gjson.GetBytes(result, "max_tokens").Int())
			} else {
				require.False(t, gjson.GetBytes(result, "max_tokens").Exists())
			}
		}
	}
}

func TestBedrockThinkingBudgetForwardBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, thinking string
		missing        bool
	}{
		{name: "legacy manual missing", thinking: `{"type":"enabled"}`, missing: true},
		{name: "legacy explicit manual", thinking: `{"type":"enabled","budget_tokens":2048}`},
		{name: "disabled needs no budget", thinking: `{"type":"disabled"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))}}}
			svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			account := &Account{Platform: PlatformAnthropic, Type: AccountTypeBedrock, Credentials: map[string]any{"auth_mode": "apikey", "api_key": "fixture"}}
			model := "us.anthropic.claude-opus-4-5-20251101-v1:0"
			body := []byte(`{"model":"` + model + `","max_tokens":10000,"messages":[{"role":"user","content":"ok"}],"thinking":` + tc.thinking + `}`)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
			_, err := svc.forwardBedrock(context.Background(), c, account, &ParsedRequest{Model: model, Body: NewRequestBodyRef(body)}, time.Now())
			if tc.missing {
				require.ErrorContains(t, err, "requires explicit thinking.budget_tokens")
				require.Empty(t, upstream.requestBodies)
				return
			}
			require.NoError(t, err)
			require.JSONEq(t, tc.thinking, gjson.GetBytes(upstream.requestBodies[0], "thinking").Raw)
		})
	}
}

func TestBedrockLegacySanitizerDoesNotInventThinkingBudget(t *testing.T) {
	body := []byte(`{"thinking":{"type":"enabled"},"messages":[]}`)
	result := sanitizeBedrockThinking(body, "claude-opus-4-5")
	require.JSONEq(t, string(body), string(result))
}
