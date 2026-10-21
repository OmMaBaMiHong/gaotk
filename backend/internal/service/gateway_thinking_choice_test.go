//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type thinkingChoiceUpstream struct {
	bodies  [][]byte
	message string
}

func (u *thinkingChoiceUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.bodies = append(u.bodies, body)
	return &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(`{"type":"error","error":{"type":"invalid_request_error","message":"` + u.message + `"}}`))}, nil
}
func (u *thinkingChoiceUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func TestForwardUpstreamErrorsDoNotChangeThinkingOrBudgets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, message := range []string{"thinking.budget_tokens must be >= 1024", "Invalid signature in thinking block"} {
		t.Run(message, func(t *testing.T) {
			upstream := &thinkingChoiceUpstream{message: message}
			cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
			svc := &GatewayService{cfg: cfg, httpUpstream: upstream, responseHeaderFilter: compileResponseHeaderFilter(cfg), rateLimitService: &RateLimitService{}, settingService: &SettingService{settingRepo: &settingRepoStub{values: map[string]string{SettingKeyRectifierSettings: `{"enabled":true,"signature_enabled":true,"api_key_signature_enabled":true,"thinking_budget_enabled":true}`}}}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
			account := newAnthropicAPIKeyAccountForTest()
			account.Extra = map[string]any{"anthropic_passthrough": false}
			body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":1000,"thinking":{"type":"enabled","budget_tokens":500},"messages":[{"role":"user","content":"write prose"}]}`)
			_, _ = svc.Forward(context.Background(), c, account, &ParsedRequest{Body: NewRequestBodyRef(body), Model: "claude-sonnet-4-5"})
			require.Len(t, upstream.bodies, 1, "an unchanged request must not be retried with invented parameters")
			sent := upstream.bodies[0]
			require.Equal(t, "enabled", gjson.GetBytes(sent, "thinking.type").String())
			require.Equal(t, int64(500), gjson.GetBytes(sent, "thinking.budget_tokens").Int())
			require.Equal(t, int64(1000), gjson.GetBytes(sent, "max_tokens").Int())
		})
	}
}

func TestSignatureRetryPreservesThinkingAndToolHistory(t *testing.T) {
	for _, mode := range []string{"enabled", "adaptive", "disabled"} {
		body := []byte(`{"thinking":{"type":"` + mode + `","budget_tokens":2048},"max_tokens":70000,"output_config":{"effort":"high"},"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"private reasoning","signature":"valid-signature"},{"type":"tool_use","id":"call-1","name":"save_draft","input":{"content":"novel prose"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"saved"}]}]}`)
		for _, filter := range []func([]byte, string) []byte{FilterThinkingBlocksForRetry, FilterSignatureSensitiveBlocksForRetry} {
			out := filter(body, "claude-sonnet-4-5")
			require.Equal(t, gjson.GetBytes(body, "thinking").Raw, gjson.GetBytes(out, "thinking").Raw)
			require.Equal(t, int64(70000), gjson.GetBytes(out, "max_tokens").Int())
			require.Equal(t, "high", gjson.GetBytes(out, "output_config.effort").String())
			require.Equal(t, "call-1", gjson.GetBytes(out, "messages.0.content.#(type==tool_use).id").String())
			require.Equal(t, "saved", gjson.GetBytes(out, "messages.1.content.0.content").String())
			if mode != "disabled" {
				require.Equal(t, "valid-signature", gjson.GetBytes(out, "messages.0.content.#(type==thinking).signature").String())
			}
			require.NotContains(t, gjson.GetBytes(out, "messages.0.content.#(type==text).text").String(), "private reasoning")
		}
	}
}

func TestSignatureRetryOnlyDropsClearlyInvalidThinking(t *testing.T) {
	body := []byte(`{"thinking":{"type":"adaptive"},"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"missing signature"},{"type":"thinking","thinking":"valid","signature":"real-signature"},{"type":"redacted_thinking","data":"encrypted"},{"type":"text","text":"answer"}]}]}`)
	out := FilterThinkingBlocksForRetry(body, "claude-sonnet-4-5")
	require.Equal(t, "adaptive", gjson.GetBytes(out, "thinking.type").String())
	require.Len(t, gjson.GetBytes(out, "messages.0.content").Array(), 3)
	require.Equal(t, "real-signature", gjson.GetBytes(out, "messages.0.content.0.signature").String())
	require.Equal(t, "encrypted", gjson.GetBytes(out, "messages.0.content.1.data").String())
	require.Equal(t, "answer", gjson.GetBytes(out, "messages.0.content.2.text").String())
}

func TestAnthropicOutputUsesCapabilityOrExplicitConfig(t *testing.T) {
	account := newAnthropicAPIKeyAccountForTest()
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{"known-model": {ID: "known-model", MaxOutputTokens: 42000}}})
	req := &apicompat.AnthropicRequest{Model: "known-model"}
	require.NoError(t, applyAnthropicOutputCapability(req, account))
	require.Equal(t, 42000, req.MaxTokens)
	req.MaxTokens = 17
	require.NoError(t, applyAnthropicOutputCapability(req, account))
	require.Equal(t, 17, req.MaxTokens)
	req = &apicompat.AnthropicRequest{Model: "unknown-model"}
	require.ErrorContains(t, applyAnthropicOutputCapability(req, account), "max_tokens")
	require.Zero(t, req.MaxTokens)
}

func TestAnthropicManualThinkingNeedsExplicitBudgetOnlyOnOriginalProtocol(t *testing.T) {
	account := newAnthropicAPIKeyAccountForTest()
	req := &apicompat.AnthropicRequest{Model: "claude-sonnet-4-5", MaxTokens: 42000, Thinking: &apicompat.AnthropicThinking{Type: "enabled"}}
	require.ErrorContains(t, applyAnthropicOutputCapability(req, account), "budget_tokens")
	req.Thinking.BudgetTokens = 3000
	require.NoError(t, applyAnthropicOutputCapability(req, account))
	require.Equal(t, 3000, req.Thinking.BudgetTokens)
	account.Credentials["base_url"] = "https://compat.example/anthropic"
	req.Thinking.BudgetTokens = 0
	req.MaxTokens = 0
	require.NoError(t, applyAnthropicOutputCapability(req, account))
	require.Zero(t, req.MaxTokens)
	require.Zero(t, req.Thinking.BudgetTokens)
}
