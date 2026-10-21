//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudeThinkingOffNativeAndCountWire(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ model, thinking, effort string }{
		{"claude-opus-5-5", "adaptive", "low"},
		{"claude-sonnet-5-5", "between_tools", "low"},
		{"claude-haiku-5-5", "disabled", ""},
	} {
		for _, count := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/count=%t", tc.model, count), func(t *testing.T) {
				body := []byte(`{"model":"public-claude","max_tokens":1024,"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"prior","signature":"signed"},{"type":"tool_use","id":"tool_1","name":"lookup","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":"retained"}]}],"thinking":{"type":"disabled"}}`)
				parsed := &ParsedRequest{Model: "public-claude", Body: NewRequestBodyRef(body)}
				response := `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":1}}`
				if count {
					response = `{"input_tokens":42}`
				}
				upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}}
				account := newAnthropicAPIKeyAccountForTest()
				account.Credentials["model_mapping"] = map[string]any{"public-claude": tc.model}
				svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream, rateLimitService: &RateLimitService{}}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				if count {
					require.NoError(t, svc.ForwardCountTokens(context.Background(), c, account, parsed))
				} else {
					_, err := svc.Forward(context.Background(), c, account, parsed)
					require.NoError(t, err)
				}
				require.NotNil(t, upstream.lastReq)
				require.Equal(t, tc.thinking, gjson.GetBytes(upstream.lastBody, "thinking.type").String())
				require.Equal(t, tc.effort, gjson.GetBytes(upstream.lastBody, "output_config.effort").String())
				require.JSONEq(t, gjson.GetBytes(body, "messages").Raw, gjson.GetBytes(upstream.lastBody, "messages").Raw)
				require.False(t, gjson.GetBytes(upstream.lastBody, "thinking.budget_tokens").Exists())
			})
		}
	}
}

func TestClaudeThinkingOffFourConversionWires(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ model, thinking string }{
		{"claude-opus-5-5", "adaptive"},
		{"claude-sonnet-5-5", "between_tools"},
		{"claude-haiku-5-5", "disabled"},
	} {
		for _, gateway := range []string{"anthropic", "openai"} {
			for _, chat := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/chat=%t", tc.model, gateway, chat), func(t *testing.T) {
					body := []byte(`{"model":"public-claude","max_output_tokens":1024,"input":"hello","reasoning":{"effort":"none"}}`)
					if chat {
						body = []byte(`{"model":"public-claude","max_tokens":1024,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"disabled"}}`)
					}
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
					account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{"public-claude": tc.model}}}
					var wire []byte
					if gateway == "anthropic" {
						upstream := &anthropicHTTPUpstreamRecorder{resp: nativeAnthropicStreamResponse()}
						svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}
						if chat {
							_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, nil)
							require.NoError(t, err)
						} else {
							_, err := svc.ForwardAsResponses(context.Background(), c, account, body, nil)
							require.NoError(t, err)
						}
						wire = upstream.lastBody
					} else {
						upstream := &httpUpstreamRecorder{resp: nativeAnthropicStreamResponse()}
						svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
						account = nativeAnthropicTestAccount()
						account.Credentials["model_mapping"] = map[string]any{"public-claude": tc.model}
						if chat {
							_, err := svc.forwardChatCompletionsViaNativeAnthropic(context.Background(), c, account, body, "")
							require.NoError(t, err)
						} else {
							_, err := svc.forwardResponsesViaNativeAnthropic(context.Background(), c, account, body, "")
							require.NoError(t, err)
						}
						wire = upstream.lastBody
					}
					require.Equal(t, tc.model, gjson.GetBytes(wire, "model").String())
					require.Equal(t, tc.thinking, gjson.GetBytes(wire, "thinking.type").String())
					require.Equal(t, "low", gjson.GetBytes(wire, "output_config.effort").String())
					require.Equal(t, int64(1024), gjson.GetBytes(wire, "max_tokens").Int())
					require.False(t, gjson.GetBytes(wire, "thinking.budget_tokens").Exists())
				})
			}
		}
	}
}
