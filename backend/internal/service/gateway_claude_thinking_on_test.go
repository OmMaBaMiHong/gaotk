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

func TestClaude55ThinkingOnNormalizationPreservesExplicitChoices(t *testing.T) {
	for _, model := range []string{"claude-opus-5-5", "anthropic/claude-sonnet-5.5", "claude-haiku-5-5"} {
		for _, effort := range []string{"", "low", "high"} {
			body := []byte(`{"thinking":{"type":"enabled","display":"omitted"}}`)
			if effort != "" {
				body = []byte(`{"thinking":{"type":"enabled","display":"omitted"},"output_config":{"effort":"` + effort + `"}}`)
			}
			out, err := normalizeAndValidateClaude55Request(body, model)
			require.NoError(t, err, model)
			require.Equal(t, "adaptive", gjson.GetBytes(out, "thinking.type").String())
			require.Equal(t, "omitted", gjson.GetBytes(out, "thinking.display").String())
			require.Equal(t, effort, gjson.GetBytes(out, "output_config.effort").String())
			require.False(t, gjson.GetBytes(out, "thinking.budget_tokens").Exists())
		}
		for _, budget := range []int{0, 2048} {
			body := []byte(fmt.Sprintf(`{"thinking":{"type":"enabled","budget_tokens":%d}}`, budget))
			out, err := normalizeAndValidateClaude55Request(body, model)
			require.ErrorContains(t, err, "requires adaptive thinking")
			require.JSONEq(t, string(body), string(out), "explicit manual configuration must not be silently discarded")
		}
	}
}

func TestClaude55ThinkingOnNativeAndCountWire(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, model := range []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"} {
		for _, count := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/count=%t", model, count), func(t *testing.T) {
				body := []byte(`{"model":"public-claude","max_tokens":1024,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"enabled"},"output_config":{"effort":"low"}}`)
				parsed := &ParsedRequest{Model: "public-claude", Body: NewRequestBodyRef(body)}
				response := `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":1}}`
				if count {
					response = `{"input_tokens":42}`
				}
				upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}}
				account := newAnthropicAPIKeyAccountForTest()
				account.Credentials["model_mapping"] = map[string]any{"public-claude": model}
				svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream, rateLimitService: &RateLimitService{}}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				if count {
					require.NoError(t, svc.ForwardCountTokens(context.Background(), c, account, parsed))
				} else {
					_, err := svc.Forward(context.Background(), c, account, parsed)
					require.NoError(t, err)
				}
				require.NotNil(t, upstream.lastReq)
				require.Equal(t, "adaptive", gjson.GetBytes(upstream.lastBody, "thinking.type").String())
				require.Equal(t, "low", gjson.GetBytes(upstream.lastBody, "output_config.effort").String())
				require.False(t, gjson.GetBytes(upstream.lastBody, "thinking.budget_tokens").Exists())
			})
		}
	}
}

func TestClaude55ThinkingOnFourConversionWires(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, model := range []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"} {
		for _, gateway := range []string{"anthropic", "openai"} {
			for _, chat := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/chat=%t", model, gateway, chat), func(t *testing.T) {
					body := []byte(`{"model":"public-claude","max_output_tokens":1024,"input":"hello","thinking":{"type":"enabled"},"reasoning":{"effort":"high"}}`)
					if chat {
						body = []byte(`{"model":"public-claude","max_tokens":1024,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"enabled"},"reasoning_effort":"high"}`)
					}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
					account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{"public-claude": model}}}
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
						account.Credentials["model_mapping"] = map[string]any{"public-claude": model}
						if chat {
							_, err := svc.forwardChatCompletionsViaNativeAnthropic(context.Background(), c, account, body, "")
							require.NoError(t, err)
						} else {
							_, err := svc.forwardResponsesViaNativeAnthropic(context.Background(), c, account, body, "")
							require.NoError(t, err)
						}
						wire = upstream.lastBody
					}
					require.Equal(t, model, gjson.GetBytes(wire, "model").String())
					require.Equal(t, "adaptive", gjson.GetBytes(wire, "thinking.type").String())
					require.Equal(t, "high", gjson.GetBytes(wire, "output_config.effort").String())
					require.False(t, gjson.GetBytes(wire, "thinking.budget_tokens").Exists())
				})
			}
		}
	}
}
