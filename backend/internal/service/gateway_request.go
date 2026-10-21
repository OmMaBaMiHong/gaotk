package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unsafe"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Anthropic requires max_tokens; use the upstream's model capability, never a task budget.
func applyAnthropicOutputCapability(req *apicompat.AnthropicRequest, account *Account) error {
	originalProtocol := false
	if account != nil {
		endpoint, _ := url.Parse(account.GetBaseURL())
		originalProtocol = account.Platform == PlatformAnthropic && (account.IsOAuth() || (endpoint != nil && endpoint.Hostname() == "api.anthropic.com"))
	}
	if originalProtocol && req.Thinking != nil && req.Thinking.Type == "enabled" && req.Thinking.BudgetTokens <= 0 {
		return fmt.Errorf("Anthropic model %q requires explicit thinking.budget_tokens for manual thinking; configure it or use a model supporting adaptive thinking", req.Model)
	}
	if req.MaxTokens > 0 {
		return nil
	}
	if metadata, ok := account.GetUpstreamModelMetadata(req.Model); ok && metadata.MaxOutputTokens > 0 {
		req.MaxTokens = int(metadata.MaxOutputTokens)
		return nil
	}
	if originalProtocol {
		return fmt.Errorf("Anthropic model %q requires max_tokens: configure its upstream max_output_tokens capability or provide an explicit request value", req.Model)
	}
	return nil
}

var (
	// 这些字节模式用于 fast-path 判断，避免每次 []byte("...") 产生临时分配。
	patternTypeThinking         = []byte(`"type":"thinking"`)
	patternTypeThinkingSpaced   = []byte(`"type": "thinking"`)
	patternTypeRedactedThinking = []byte(`"type":"redacted_thinking"`)
	patternTypeRedactedSpaced   = []byte(`"type": "redacted_thinking"`)

	patternThinkingField       = []byte(`"thinking":`)
	patternThinkingFieldSpaced = []byte(`"thinking" :`)

	patternEmptyContent       = []byte(`"content":[]`)
	patternEmptyContentSpaced = []byte(`"content": []`)
	patternEmptyContentSp1    = []byte(`"content" : []`)
	patternEmptyContentSp2    = []byte(`"content" :[]`)

	// Fast-path patterns for empty text blocks: {"type":"text","text":""}
	patternEmptyText       = []byte(`"text":""`)
	patternEmptyTextSpaced = []byte(`"text": ""`)
	patternEmptyTextSp1    = []byte(`"text" : ""`)
	patternEmptyTextSp2    = []byte(`"text" :""`)

	sessionUserAgentProductPattern = regexp.MustCompile(`([A-Za-z0-9._-]+)/[A-Za-z0-9._-]+`)
	sessionUserAgentVersionPattern = regexp.MustCompile(`\bv?\d+(?:\.\d+){1,3}\b`)
)

// SessionContext 粘性会话上下文，用于区分不同来源的请求。
// 仅在 GenerateSessionHash 第 3 级 fallback（消息内容 hash）时混入，
// 避免不同用户发送相同消息产生相同 hash 导致账号集中。
type SessionContext struct {
	ClientIP  string
	UserAgent string
	APIKeyID  int64
}

type jsonRange struct {
	start int        // 原始请求体中的起始偏移（闭区间）
	end   int        // 原始请求体中的结束偏移（开区间）
	kind  gjson.Type // JSON 值类型，用于调用方做轻量分支
}

type RequestBodyRef struct {
	data []byte
}

func NewRequestBodyRef(data []byte) *RequestBodyRef {
	return &RequestBodyRef{data: data}
}

func (b *RequestBodyRef) Bytes() []byte {
	if b == nil {
		return nil
	}
	return b.data
}

func (b *RequestBodyRef) Len() int {
	if b == nil {
		return 0
	}
	return len(b.data)
}

func (b *RequestBodyRef) Replace(data []byte) {
	if b == nil {
		return
	}
	b.data = data
}

func missingJSONRange() jsonRange {
	return jsonRange{start: -1, end: -1}
}

func rangeFromResult(r gjson.Result) jsonRange {
	if r.Raw == "" || r.Index <= 0 {
		return missingJSONRange()
	}
	end := r.Index + len(r.Raw)
	if end < r.Index {
		return missingJSONRange()
	}
	return jsonRange{start: r.Index, end: end, kind: r.Type}
}

func (r jsonRange) exists() bool {
	return r.start >= 0 && r.end >= r.start
}

// clearGatewayRequestDerivedState 清空绑定当前 body 的轻量派生字段，防止 ReplaceBody 后读到旧值。
func clearGatewayRequestDerivedState(parsed *ParsedRequest) {
	if parsed == nil {
		return
	}
	parsed.Model = ""
	parsed.Stream = false
	parsed.MetadataUserID = ""
	parsed.HasSystem = false
	parsed.ThinkingEnabled = false
	parsed.OutputEffort = ""
	parsed.Speed = ""
	parsed.MaxTokens = 0
	parsed.systemRange = missingJSONRange()
	parsed.messagesRange = missingJSONRange()
	parsed.inputRange = missingJSONRange()
}

func clearGatewayRequestRanges(parsed *ParsedRequest) {
	if parsed == nil {
		return
	}
	parsed.HasSystem = false
	parsed.systemRange = missingJSONRange()
	parsed.messagesRange = missingJSONRange()
	parsed.inputRange = missingJSONRange()
}

func setGatewayRequestRanges(parsed *ParsedRequest, protocol string, jsonStr string) {
	if parsed == nil {
		return
	}
	switch protocol {
	case domain.PlatformGemini:
		if sysParts := gjson.Get(jsonStr, "systemInstruction.parts"); sysParts.Exists() && sysParts.IsArray() {
			parsed.systemRange = rangeFromResult(sysParts)
		}
		if contents := gjson.Get(jsonStr, "contents"); contents.Exists() && contents.IsArray() {
			parsed.messagesRange = rangeFromResult(contents)
		}
	default:
		if sys := gjson.Get(jsonStr, "system"); sys.Exists() {
			parsed.HasSystem = true
			parsed.systemRange = rangeFromResult(sys)
		}
		if msgs := gjson.Get(jsonStr, "messages"); msgs.Exists() && msgs.IsArray() {
			parsed.messagesRange = rangeFromResult(msgs)
		}
		if protocol == "responses" {
			if input := gjson.Get(jsonStr, "input"); input.Exists() {
				parsed.inputRange = rangeFromResult(input)
			}
		}
	}
}

const claudeCodeLongContextModelSuffix = "[1m]"

// Claude Code treats [1m] as a client-side context selector and normally removes it
// before provider requests. Normalize leaked suffixes, including its duplicated form.
func normalizeClaudeCodeLongContextModel(model string) string {
	for len(model) > len(claudeCodeLongContextModelSuffix) &&
		strings.EqualFold(model[len(model)-len(claudeCodeLongContextModelSuffix):], claudeCodeLongContextModelSuffix) {
		model = model[:len(model)-len(claudeCodeLongContextModelSuffix)]
	}
	return model
}

// parseGatewayRequestCurrentBody 只做标量和 raw range 轻量解析，不恢复 system/messages 对象图。
func parseGatewayRequestCurrentBody(parsed *ParsedRequest, protocol string) error {
	if parsed == nil || parsed.Body == nil {
		return fmt.Errorf("empty request body")
	}

	bodyBytes := parsed.Body.Bytes()
	if !gjson.ValidBytes(bodyBytes) {
		return DescribeInvalidJSON(bodyBytes)
	}

	// 只在当前函数内零拷贝读取 JSON 字段；ReplaceBody 后必须重新进入本函数刷新派生状态。
	jsonStr := *(*string)(unsafe.Pointer(&bodyBytes))
	clearGatewayRequestDerivedState(parsed)
	parsed.protocol = protocol

	modelResult := gjson.Get(jsonStr, "model")
	if modelResult.Exists() {
		if modelResult.Type != gjson.String {
			return fmt.Errorf("invalid model field type")
		}
		parsed.Model = modelResult.String()
		if protocol == domain.PlatformAnthropic {
			normalizedModel := normalizeClaudeCodeLongContextModel(parsed.Model)
			if normalizedModel != parsed.Model {
				normalizedBody, err := sjson.SetBytes(bodyBytes, "model", normalizedModel)
				if err != nil {
					return fmt.Errorf("normalize model field: %w", err)
				}
				parsed.Body.Replace(normalizedBody)
				bodyBytes = normalizedBody
				jsonStr = *(*string)(unsafe.Pointer(&bodyBytes))
				parsed.Model = normalizedModel
			}
		}
	}

	streamResult := gjson.Get(jsonStr, "stream")
	if streamResult.Exists() {
		if streamResult.Type != gjson.True && streamResult.Type != gjson.False {
			return fmt.Errorf("invalid stream field type")
		}
		parsed.Stream = streamResult.Bool()
	}

	parsed.MetadataUserID = gjson.Get(jsonStr, "metadata.user_id").String()

	thinkingType := gjson.Get(jsonStr, "thinking.type").String()
	parsed.ThinkingEnabled = thinkingType == "enabled" || thinkingType == "adaptive" || thinkingType == "between_tools" ||
		(protocol == domain.PlatformAnthropic && isClaude55SignedThinkingModel(parsed.Model))

	parsed.OutputEffort = strings.TrimSpace(gjson.Get(jsonStr, "output_config.effort").String())
	if protocol == domain.PlatformAnthropic {
		parsed.Speed = strings.ToLower(strings.TrimSpace(gjson.Get(jsonStr, "speed").String()))
	}

	maxTokensResult := gjson.Get(jsonStr, "max_tokens")
	if maxTokensResult.Exists() && maxTokensResult.Type == gjson.Number {
		f := maxTokensResult.Float()
		if !math.IsNaN(f) && !math.IsInf(f, 0) && f == math.Trunc(f) &&
			f <= float64(math.MaxInt) && f >= float64(math.MinInt) {
			parsed.MaxTokens = int(f)
		}
	}

	setGatewayRequestRanges(parsed, protocol, jsonStr)
	return nil
}

func refreshGatewayRequestRanges(parsed *ParsedRequest, protocol string) error {
	return parseGatewayRequestCurrentBody(parsed, protocol)
}

// DescribeInvalidJSON returns a diagnostic error for a request body that
// failed JSON validation. It re-parses with encoding/json (failure path only)
// to pinpoint the first offending byte, so operators can distinguish genuinely
// invalid JSON from a truncated / partially consumed body. The error carries
// only length/offset/character information — never body content — so callers
// may safely wrap or log it.
func DescribeInvalidJSON(body []byte) error {
	var raw json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			return fmt.Errorf("invalid json (len=%d, offset=%d): %s", len(body), syntaxErr.Offset, syntaxErr.Error())
		}
		return fmt.Errorf("invalid json (len=%d): %w", len(body), err)
	}
	// gjson rejected the body but encoding/json accepted it (divergent edge
	// cases, e.g. certain malformed UTF-8 sequences); report the basics.
	return fmt.Errorf("invalid json (len=%d)", len(body))
}

// ParsedRequest 保存网关请求的预解析结果
//
// 性能优化说明：
// 原实现在多个位置重复解析请求体（Handler、Service 各解析一次）：
// 1. gateway_handler.go 解析获取 model 和 stream
// 2. gateway_service.go 再次解析获取 system、messages、metadata
// 3. GenerateSessionHash 又一次解析获取会话哈希所需字段
//
// 新实现一次解析，多处复用：
// 1. 在 Handler 层统一调用 ParseGatewayRequest 一次性解析
// 2. 将解析结果 ParsedRequest 传递给 Service 层
// 3. 避免重复 json.Unmarshal，减少 CPU 和内存开销
type ParsedRequest struct {
	Body            *RequestBodyRef // 原始请求体引用（保留用于转发）；替换内容请走 ReplaceBody
	Model           string          // 请求的模型名称
	Stream          bool            // 是否为流式请求
	MetadataUserID  string          // metadata.user_id（用于会话亲和）
	HasSystem       bool            // 是否包含 system 字段（包含 null 也视为显式传入）
	ThinkingEnabled bool            // 是否开启 thinking（部分平台会影响最终模型名）
	OutputEffort    string          // output_config.effort（Claude API 的推理强度控制）
	Speed           string          // Anthropic speed（当前可计费值为 "fast"）
	MaxTokens       int             // max_tokens 值（用于探测请求拦截）
	SessionContext  *SessionContext // 可选：请求上下文区分因子（nil 时行为不变）

	protocol      string    // 当前 Body 的协议格式，用于 Body 替换后刷新 raw range
	systemRange   jsonRange // system/systemInstruction.parts 的 raw JSON 范围，绑定 Body 当前内容
	messagesRange jsonRange // messages/contents 的 raw JSON 范围，绑定 Body 当前内容
	inputRange    jsonRange // Responses API input 的 raw JSON 范围，绑定 Body 当前内容

	// GroupID 请求所属分组 ID（来自 API Key）
	GroupID *int64

	// OnUpstreamAccepted 上游接受请求后立即调用（用于提前释放串行锁）
	// 流式请求在收到 2xx 响应头后调用，避免持锁等流完成
	OnUpstreamAccepted func()
}

// NormalizeSessionUserAgent reduces UA noise for sticky-session and digest hashing.
// It preserves the set of product names from Product/Version tokens while
// discarding version-only changes and incidental comments.
func NormalizeSessionUserAgent(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	matches := sessionUserAgentProductPattern.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return normalizeSessionUserAgentFallback(raw)
	}

	products := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		product := strings.ToLower(strings.TrimSpace(match[1]))
		if product == "" {
			continue
		}
		if _, exists := seen[product]; exists {
			continue
		}
		seen[product] = struct{}{}
		products = append(products, product)
	}
	if len(products) == 0 {
		return normalizeSessionUserAgentFallback(raw)
	}
	sort.Strings(products)
	return strings.Join(products, "+")
}

func normalizeSessionUserAgentFallback(raw string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(raw), " "))
	normalized = sessionUserAgentVersionPattern.ReplaceAllString(normalized, "")
	return strings.Join(strings.Fields(normalized), " ")
}

// ParseGatewayRequest 解析网关请求体并返回结构化结果。
// protocol 指定请求协议格式（domain.PlatformAnthropic / domain.PlatformGemini），
// 不同协议使用不同的 system/messages 字段名。
func ParseGatewayRequest(body *RequestBodyRef, protocol string) (*ParsedRequest, error) {
	parsed := &ParsedRequest{Body: body}
	if err := parseGatewayRequestCurrentBody(parsed, protocol); err != nil {
		return nil, err
	}
	return parsed, nil
}

func (p *ParsedRequest) raw(r jsonRange) []byte {
	if p == nil || p.Body == nil || !r.exists() {
		return nil
	}
	body := p.Body.Bytes()
	if r.end > len(body) {
		return nil
	}
	return body[r.start:r.end]
}

func (p *ParsedRequest) SystemRaw() []byte {
	return p.raw(p.systemRange)
}

func (p *ParsedRequest) MessagesRaw() []byte {
	return p.raw(p.messagesRange)
}

func (p *ParsedRequest) InputRaw() []byte {
	return p.raw(p.inputRange)
}

func (p *ParsedRequest) DecodeSystem(dst any) error {
	raw := p.SystemRaw()
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

func (p *ParsedRequest) DecodeMessages(dst any) error {
	raw := p.MessagesRaw()
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

func (p *ParsedRequest) SystemValue() (any, bool) {
	raw := p.SystemRaw()
	if len(raw) == 0 {
		return nil, false
	}
	var system any
	if err := json.Unmarshal(raw, &system); err != nil {
		return nil, false
	}
	return system, true
}

// CloneForBody 为单次账号尝试创建独立 body 视图，避免 failover 复用已改写的 ParsedRequest。
func (p *ParsedRequest) CloneForBody(body []byte) (*ParsedRequest, error) {
	if p == nil {
		return nil, fmt.Errorf("parse request: empty request")
	}
	clone := *p
	clone.Body = NewRequestBodyRef(body)
	clone.OnUpstreamAccepted = nil
	if err := refreshGatewayRequestRanges(&clone, clone.protocol); err != nil {
		return nil, err
	}
	return &clone, nil
}

// ReplaceBody 统一刷新当前 body 和 raw range，保证后续 helper 读取的是最新请求体。
func (p *ParsedRequest) ReplaceBody(data []byte) error {
	if p == nil {
		return fmt.Errorf("parse request: empty request")
	}
	if p.Body == nil {
		p.Body = NewRequestBodyRef(data)
	} else {
		p.Body.Replace(data)
	}
	if err := refreshGatewayRequestRanges(p, p.protocol); err != nil {
		clearGatewayRequestRanges(p)
		return err
	}
	return nil
}

// sliceRawFromBody 返回 Result.Raw 对应的原始字节切片。
// 优先使用 Result.Index 直接从 body 切片，避免对大字段（如 messages）产生额外拷贝。
// 当 Index 不可用时，退化为复制（理论上极少发生）。
func sliceRawFromBody(body []byte, r gjson.Result) []byte {
	if r.Index > 0 {
		end := r.Index + len(r.Raw)
		if end <= len(body) {
			return body[r.Index:end]
		}
	}
	// fallback: 不影响正确性，但会产生一次拷贝
	return []byte(r.Raw)
}

// stripEmptyTextBlocksFromSlice removes empty text blocks from a content slice (including nested tool_result content).
// Returns (cleaned slice, true) if any blocks were removed, or (original, false) if unchanged.
func stripEmptyTextBlocksFromSlice(blocks []any) ([]any, bool) {
	var result []any
	changed := false
	for i, block := range blocks {
		blockMap, ok := block.(map[string]any)
		if !ok {
			if result != nil {
				result = append(result, block)
			}
			continue
		}
		blockType, _ := blockMap["type"].(string)

		// Strip empty text blocks
		if blockType == "text" {
			if txt, _ := blockMap["text"].(string); txt == "" {
				if result == nil {
					result = make([]any, 0, len(blocks))
					result = append(result, blocks[:i]...)
				}
				changed = true
				continue
			}
		}

		// Recurse into tool_result nested content
		if blockType == "tool_result" {
			if nestedContent, ok := blockMap["content"].([]any); ok {
				if cleaned, nestedChanged := stripEmptyTextBlocksFromSlice(nestedContent); nestedChanged {
					if result == nil {
						result = make([]any, 0, len(blocks))
						result = append(result, blocks[:i]...)
					}
					changed = true
					blockCopy := make(map[string]any, len(blockMap))
					for k, v := range blockMap {
						blockCopy[k] = v
					}
					blockCopy["content"] = cleaned
					result = append(result, blockCopy)
					continue
				}
			}
		}

		if result != nil {
			result = append(result, block)
		}
	}
	if !changed {
		return blocks, false
	}
	return result, true
}

// StripEmptyTextBlocks removes empty text blocks from the request body (including nested tool_result content).
// This is a lightweight pre-filter for the initial request path to prevent upstream 400 errors.
// Returns the original body unchanged if no empty text blocks are found.
func StripEmptyTextBlocks(body []byte) []byte {
	// Fast path: check if body contains empty text patterns
	hasEmptyTextBlock := bytes.Contains(body, patternEmptyText) ||
		bytes.Contains(body, patternEmptyTextSpaced) ||
		bytes.Contains(body, patternEmptyTextSp1) ||
		bytes.Contains(body, patternEmptyTextSp2)
	if !hasEmptyTextBlock {
		return body
	}

	jsonStr := *(*string)(unsafe.Pointer(&body))
	msgsRes := gjson.Get(jsonStr, "messages")
	if !msgsRes.Exists() || !msgsRes.IsArray() {
		return body
	}

	var messages []any
	if err := json.Unmarshal(sliceRawFromBody(body, msgsRes), &messages); err != nil {
		return body
	}

	modified := false
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		content, ok := msgMap["content"].([]any)
		if !ok {
			continue
		}
		if cleaned, changed := stripEmptyTextBlocksFromSlice(content); changed {
			modified = true
			msgMap["content"] = cleaned
		}
	}

	if !modified {
		return body
	}

	msgsBytes, err := json.Marshal(messages)
	if err != nil {
		return body
	}
	out, err := sjson.SetRawBytes(body, "messages", msgsBytes)
	if err != nil {
		return body
	}
	return out
}

// isClaude55SignedThinkingModel identifies models whose default thinking mode
// requires signed history to survive protocol conversion and request filtering.
func isClaude55SignedThinkingModel(model string) bool {
	return claude.IsOpus55(model) || claude.IsSonnet55(model) || claude.IsHaiku55(model)
}

// Normalize on/off requests to the confirmed model's supported modes.
// Conversion callers retain the original body so disabled still maps to OpenAI effort none.
func normalizeAndValidateClaude55Request(body []byte, model string) ([]byte, error) {
	if isClaude55SignedThinkingModel(model) && gjson.GetBytes(body, "thinking.type").String() == "enabled" && !gjson.GetBytes(body, "thinking.budget_tokens").Exists() {
		next, err := sjson.SetBytes(body, "thinking.type", "adaptive")
		if err != nil {
			return body, err
		}
		body = next
	}
	if gjson.GetBytes(body, "thinking.type").String() == "disabled" && (claude.IsOpus55(model) || claude.IsSonnet55(model)) {
		mode := "adaptive"
		if claude.IsSonnet55(model) {
			mode = "between_tools"
		}
		next, err := sjson.SetBytes(body, "thinking", map[string]string{"type": mode})
		if err != nil {
			return body, err
		}
		next, err = sjson.SetBytes(next, "output_config.effort", "low")
		if err != nil {
			return body, err
		}
		body = next
	}
	return body, validateClaude55Request(body, model)
}

// validateClaude55Request rejects settings that the upstream cannot honor.
// Call before OAuth mimicry can remove tool_choice or alter thinking defaults.
func validateClaude55Request(body []byte, model string) error {
	if !isClaude55SignedThinkingModel(model) {
		return nil
	}
	isSonnet55 := claude.IsSonnet55(model)
	isHaiku55 := claude.IsHaiku55(model)
	thinkingType := gjson.GetBytes(body, "thinking.type").String()
	switch thinkingType {
	case "disabled", "enabled":
		if isHaiku55 {
			// haiku-5-5：enabled 一律 400（用 adaptive）；disabled 仅 high 及以下
			// 可用，xhigh/max 上 400。见官方 migration guide。
			if thinkingType == "enabled" {
				return fmt.Errorf("claude-haiku-5-5 requires adaptive thinking; omit thinking or use thinking.type=adaptive and output_config.effort")
			}
			switch gjson.GetBytes(body, "output_config.effort").String() {
			case "xhigh", "max":
				return fmt.Errorf("claude-haiku-5-5 thinking.type=disabled supports only low, medium or high effort")
			}
			break
		}
		if isSonnet55 {
			return fmt.Errorf("claude-sonnet-5-5 requires adaptive thinking or thinking.type=between_tools; omit thinking or use one of those modes")
		}
		return fmt.Errorf("claude-opus-5-5 requires adaptive thinking; omit thinking or use thinking.type=adaptive and output_config.effort")
	case "between_tools":
		if !isSonnet55 {
			if isHaiku55 {
				return fmt.Errorf("claude-haiku-5-5 requires adaptive thinking; thinking.type=between_tools is unsupported (use thinking.type=disabled at high effort or below)")
			}
			return fmt.Errorf("claude-opus-5-5 requires adaptive thinking; thinking.type=between_tools is unsupported")
		}
		effort := gjson.GetBytes(body, "output_config.effort").String()
		if effort == "xhigh" || effort == "max" {
			return fmt.Errorf("claude-sonnet-5-5 thinking.type=between_tools supports only low, medium or high effort")
		}
		for _, field := range []string{"thinking.display", "thinking.budget_tokens", "thinking.block_binding"} {
			if gjson.GetBytes(body, field).Exists() {
				return fmt.Errorf("claude-sonnet-5-5 thinking.type=between_tools does not support %s", field)
			}
		}
	}
	modelName := "claude-opus-5-5"
	if isSonnet55 {
		modelName = "claude-sonnet-5-5"
	}
	if isHaiku55 {
		modelName = "claude-haiku-5-5"
	}
	// haiku-5-5 接受强制 tool_choice（响应以 tool call 开头、无 thinking 块），
	// 与 opus/sonnet-5-5 不同，不拦截。见官方 migration guide。
	if !isHaiku55 {
		if gjson.GetBytes(body, "tool_choice").String() == "required" {
			return fmt.Errorf("%s does not support forced tool_choice; use auto or none", modelName)
		}
		switch gjson.GetBytes(body, "tool_choice.type").String() {
		case "any", "tool", "function", "custom", "namespace":
			return fmt.Errorf("%s does not support forced tool_choice; use auto or none", modelName)
		}
	}
	if isSonnet55 || isHaiku55 {
		if temperature := gjson.GetBytes(body, "temperature"); temperature.Exists() && (temperature.Type != gjson.Number || temperature.Float() != 1) {
			return fmt.Errorf("%s does not support non-default temperature", modelName)
		}
		// Haiku 5.5 只接受显式 top_p=0.99；保留 Sonnet 5.5 既有的区间策略。
		if topP := gjson.GetBytes(body, "top_p"); topP.Exists() && (topP.Type != gjson.Number ||
			(isHaiku55 && topP.Float() != 0.99) || (isSonnet55 && (topP.Float() < 0.99 || topP.Float() > 1))) {
			return fmt.Errorf("%s does not support non-default top_p", modelName)
		}
		if gjson.GetBytes(body, "top_k").Exists() {
			return fmt.Errorf("%s does not support top_k", modelName)
		}
		if isHaiku55 && gjson.GetBytes(body, "temperature").Exists() && gjson.GetBytes(body, "top_p").Exists() {
			return fmt.Errorf("%s does not allow temperature and top_p together; omit both", modelName)
		}
	}
	return nil
}

// FilterThinkingBlocks removes thinking blocks from request body
// Returns filtered body or original body if filtering fails (fail-safe)
// This prevents 400 errors from invalid thinking block signatures.
//
// mappedModel 是「实际发给上游的模型 ID」(after account model mapping)，用于按
// 协议族分流。仅 anthropic-strict 走原过滤逻辑；passback-required 与 unknown
// 一律保留全部 thinking block，避免误伤第三方兼容上游
// (DeepSeek `/anthropic`、Kimi `/coding`、GLM、Moonshot 等)，详见
// .pensieve/short-term/knowledge/thinking-block-filter-third-party-upstream-inversion/。
//
// 策略 (anthropic-strict only)：
//   - 当 thinking.type 不是 "enabled"/"adaptive"：移除所有 thinking 相关块
//   - 当 thinking.type 是 "enabled"/"adaptive"：移除缺失/无效 signature 的 thinking 块（避免 400）
//   - 对要求签名历史完整的 5.5 模型，一旦有无效块，移除历史中的全部 thinking 块
func FilterThinkingBlocks(body []byte, mappedModel string) []byte {
	if !ShouldPreFilterThinkingBlocks(mappedModel) {
		return body
	}
	return filterThinkingBlocksInternal(body, isClaude55SignedThinkingModel(mappedModel))
}

// FilterThinkingBlocksForRetry repairs malformed historical blocks without changing
// the current request's thinking choice or converting protocol data into prose.
func FilterThinkingBlocksForRetry(body []byte, mappedModel string) []byte {
	if !ShouldApplyRetryFilters(mappedModel) {
		return body
	}
	var req map[string]any
	if json.Unmarshal(body, &req) != nil {
		return body
	}
	messages, ok := req["messages"].([]any)
	if !ok {
		return body
	}
	mode := gjson.GetBytes(body, "thinking.type").String()
	thinkingEnabled := mode == "enabled" || mode == "adaptive" || mode == "between_tools" || (mode == "" && isClaude55SignedThinkingModel(mappedModel))
	changed := false
	for _, message := range messages {
		msg, ok := message.(map[string]any)
		if !ok {
			continue
		}
		content, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		cleaned, emptyChanged := stripEmptyTextBlocksFromSlice(content)
		kept := make([]any, 0, len(cleaned))
		for _, block := range cleaned {
			value, ok := block.(map[string]any)
			if !ok {
				kept = append(kept, block)
				continue
			}
			typ, _ := value["type"].(string)
			invalid := false
			switch typ {
			case "thinking":
				signature, _ := value["signature"].(string)
				thought, _ := value["thinking"].(string)
				invalid = !thinkingEnabled || thought == "" || signature == "" || signature == antigravity.DummyThoughtSignature
			case "redacted_thinking":
				data, _ := value["data"].(string)
				invalid = !thinkingEnabled || data == ""
			case "":
				_, invalid = value["thinking"]
			}
			if invalid {
				emptyChanged = true
				continue
			}
			kept = append(kept, block)
		}
		if len(kept) == 0 {
			placeholder := "(content removed)"
			if msg["role"] == "assistant" {
				placeholder = "(assistant content removed)"
			}
			kept = append(kept, map[string]any{"type": "text", "text": placeholder})
			emptyChanged = true
		}
		if emptyChanged {
			msg["content"] = kept
			changed = true
		}
	}
	if !changed {
		return body
	}
	encoded, err := json.Marshal(messages)
	if err != nil {
		return body
	}
	out, err := sjson.SetRawBytes(body, "messages", encoded)
	if err != nil {
		return body
	}
	return out
}

// removeThinkingDependentContextStrategies 从 context_management.edits 中移除
// 需要 thinking 启用的策略（如 clear_thinking_20251015）。
// 当顶层 "thinking" 字段被禁用时必须调用，否则上游会返回
// "strategy requires thinking to be enabled or adaptive"。
func removeThinkingDependentContextStrategies(body []byte) []byte {
	jsonStr := *(*string)(unsafe.Pointer(&body))
	editsRes := gjson.Get(jsonStr, "context_management.edits")
	if !editsRes.Exists() || !editsRes.IsArray() {
		return body
	}

	var filtered []json.RawMessage
	hasRemoved := false
	editsRes.ForEach(func(_, v gjson.Result) bool {
		if v.Get("type").String() == "clear_thinking_20251015" {
			hasRemoved = true
			return true
		}
		filtered = append(filtered, json.RawMessage(v.Raw))
		return true
	})

	if !hasRemoved {
		return body
	}

	if len(filtered) == 0 {
		if b, err := sjson.DeleteBytes(body, "context_management.edits"); err == nil {
			return b
		}
		return body
	}

	filteredBytes, err := json.Marshal(filtered)
	if err != nil {
		return body
	}
	if b, err := sjson.SetRawBytes(body, "context_management.edits", filteredBytes); err == nil {
		return b
	}
	return body
}

// anthropicBetaContextManagementToken 是 context_management 字段受的 beta token。
// 与 claude.BetaContextManagement 保持一致；在本文件本地定义以避免震荡
// claude package 的该常量含义。
const anthropicBetaContextManagementToken = "context-management-2025-06-27"

// sanitizeAnthropicBodyForBetaTokens 是对 Anthropic 直连路径上 body↔beta header
// **能力维度**对称约束的统一实现，与 Bedrock 路径的
// `sanitizeBedrockFieldsForBetaTokens` 对称。
//
// 问题场景：
//   - context_management 是 Claude Code CLI 2.1.87+ 默认携带的 beta 字段
//     （含 clear_thinking_20251015 等清理策略）
//   - 其被 Anthropic 上游接受的前提是 anthropic-beta header 含
//     `context-management-2025-06-27`
//   - 若两侧不一致上游 Pydantic schema 拒收：
//     "context_management: Extra inputs are not permitted"
//
// fallbacks 场景（与 context_management 同构）：
//   - `fallbacks` / `fallback_credit_token` 是 beta Messages API 的
//     server-side refusal fallback 字段；标准 Messages schema 没有它们，
//     客户端（Claude Code / SDK / OpenCode 等）最近开始默认透传
//     `"fallbacks":"default"`（或模型列表）
//   - 上游接受的前提是 anthropic-beta 含 `server-side-fallback-2026-07-01`
//     （fallback_credit_token 额外接受 credit beta，见下）
//   - 缺 token 时上游 Pydantic extra='forbid' 拒收：
//     "fallbacks: Extra inputs are not permitted"
//   - 本仓不写入该字段，全部来自客户端透传；OAuth mimic 用
//     FullClaudeCodeMimicryBetas 覆盖客户端 beta（该列表不含 fallback beta），
//     若不 strip，body 字段与 header 不对称 → 所有模型 400
//
// thinking.block_binding 场景：
//   - Claude Fable 5.1 的会话前缀绑定控制受
//     `thinking-binding-controls-2026-08-01` beta 保护
//   - 缺 token 时上游拒收：
//     "thinking.adaptive.block_binding: Extra inputs are not permitted"
//
// message-level output_config 场景：
//   - pi-ai（Harness 使用的 Anthropic provider）会为 opus5 生成形如
//     `{"role":"system","content":[],"output_config":{"effort":"high"}}` 的控制消息，
//     并请求 `mid-conversation-output-config-2026-07-01` beta
//   - 该 output_config 是 **message 级**字段，只有该 beta 保护；顶层 output_config/effort
//     不受它约束
//   - OAuth mimic 用 FullClaudeCodeMimicryBetas 覆盖客户端 beta；固定列表漏该 beta 时
//     body 字段与 header 不对称 → 上游报 "output_config: Extra inputs are not permitted"
//   - 缺 token 时净化消息级 output_config（详见 stripAnthropicMessageOutputConfigUnlessBeta）
//
// 本函数按最终发送的 anthropic-beta header 决定是否保留 body 中的上述字段：
// 缺对应 beta token → strip；客户端 header 已带对应 beta → 保留（不过度删除）。
// 这将限制完全建立在 "能力维度" 上，与 model 名 / token type / mimicry 子路径无关。
//
// 调用约束：必须在 CCH 签名之前调用，否则签名 hash 与最终 body
// 不一致，上游会以 third-party 拒收。
//
// 返回 (sanitized, changed)：changed 表示是否发生实际删除，供调用方决定
// 是否重用原 body 引用。
func sanitizeAnthropicBodyForBetaTokens(body []byte, anthropicBetaHeader string) ([]byte, bool) {
	if len(body) == 0 {
		return body, false
	}

	changed := false

	// context_management：需要 context-management beta。
	if b, deleted := stripAnthropicBodyFieldUnlessBeta(
		body, "context_management", anthropicBetaHeader, anthropicBetaContextManagementToken,
	); deleted {
		body, changed = b, true
	}

	// thinking.block_binding：需要 thinking-binding-controls beta。
	if b, deleted := stripAnthropicBodyFieldUnlessBeta(
		body, "thinking.block_binding", anthropicBetaHeader, claude.BetaThinkingBindingControls,
	); deleted {
		body, changed = b, true
	}

	// fallbacks：server-side refusal fallback，仅接受 server-side-fallback beta。
	if b, deleted := stripAnthropicBodyFieldUnlessBeta(
		body, "fallbacks", anthropicBetaHeader, claude.BetaServerSideFallback,
	); deleted {
		body, changed = b, true
	}

	// fallback_credit_token：server-side-fallback 或（新旧任一）fallback-credit beta
	// 任意一个即可保留。
	if b, deleted := stripAnthropicBodyFieldUnlessBeta(
		body, "fallback_credit_token", anthropicBetaHeader,
		claude.BetaServerSideFallback, claude.BetaFallbackCredit, claude.BetaFallbackCreditLegacy,
	); deleted {
		body, changed = b, true
	}

	// messages[].output_config：mid-conversation-output-config beta 专属字段。
	// 顶层 output_config / effort 不受该 beta 约束，本分支只净化消息内字段。
	if b, deleted := stripAnthropicMessageOutputConfigUnlessBeta(body, anthropicBetaHeader); deleted {
		body, changed = b, true
	}

	return body, changed
}

// stripAnthropicBodyFieldUnlessBeta 当 field 存在且 anthropic-beta header 不含
// requiredTokens 中**任何一个** token 时删除该字段（保留条件：含任一 required token）。
// 单 token 调用即「缺该 beta 则 strip」。返回 (newBody, deleted)。
func stripAnthropicBodyFieldUnlessBeta(body []byte, field, anthropicBetaHeader string, requiredTokens ...string) ([]byte, bool) {
	if !gjson.GetBytes(body, field).Exists() {
		return body, false
	}
	for _, token := range requiredTokens {
		if anthropicBetaTokensContains(anthropicBetaHeader, token) {
			return body, false
		}
	}
	b, err := sjson.DeleteBytes(body, field)
	if err != nil {
		// 不应发生：gjson 刚验证过字段存在 + body 是合法 JSON。如果 sjson 仍报错，
		// 调用方会拿到原 body（视为未删除），但此前 computeFinalAnthropicBeta 可能已按
		// "strip 后" 计算了 finalBeta——两侧会不一致。记录 warning 最小限度提醒运维。
		logger.LegacyPrintf("service.gateway",
			"[BetaFieldSanitize] sjson.DeleteBytes(%s) failed unexpectedly: %v (body len=%d). "+
				"body and final anthropic-beta header may be out of sync.", field, err, len(body))
		return body, false
	}
	return b, true
}

// anthropicBetaTokensContains 检测逗号分隔的 anthropic-beta header 是否含指定 token。
// 宋体空格宽容；区分大小写（Anthropic beta token 始终是小写）。
func anthropicBetaTokensContains(header, token string) bool {
	if header == "" || token == "" {
		return false
	}
	for _, part := range strings.Split(header, ",") {
		if strings.TrimSpace(part) == token {
			return true
		}
	}
	return false
}

// stripAnthropicMessageOutputConfigUnlessBeta 在 anthropic-beta header 缺
// mid-conversation-output-config beta 时，净化 **messages[].output_config**：
//   - 仅为携带 message-level output_config 的消息剥该字段；
//   - 若该消息 role=system 且 content 无正文（缺失 / null / 空 string / 空 array /
//     仅空 text 块），整条删除（pi-ai 为 opus5 生成的空 system 控制消息即此形态）；
//   - system 有正文则保留正文与其余字段；user/assistant 只剥字段，绝不整条删除；
//   - 无任何消息携带该字段时返回原 body（字节 no-op）。
//
// header 含该 beta 时完全保留。顶层 output_config / effort 不属于该 beta 保护范围，
// 本函数不做任何处理。多条删除用「稳健重建」实现，保留其余字段与消息先后顺序。
func stripAnthropicMessageOutputConfigUnlessBeta(body []byte, anthropicBetaHeader string) ([]byte, bool) {
	if anthropicBetaTokensContains(anthropicBetaHeader, claude.BetaMidConversationOutputConfig) {
		return body, false
	}
	// 快速路径：body 中不含 output_config 字面量时无需解析。
	if !bytes.Contains(body, []byte("output_config")) {
		return body, false
	}
	msgsRes := gjson.GetBytes(body, "messages")
	if !msgsRes.Exists() || !msgsRes.IsArray() {
		return body, false
	}

	hasMessageOutputConfig := false
	for _, msg := range msgsRes.Array() {
		if msg.Get("output_config").Exists() {
			hasMessageOutputConfig = true
			break
		}
	}
	if !hasMessageOutputConfig {
		return body, false
	}

	var messages []json.RawMessage
	if err := json.Unmarshal([]byte(msgsRes.Raw), &messages); err != nil {
		// gjson 的 IsArray 只做形态判断、不保证 JSON 完整合法；此分支保守返回原 body。
		return body, false
	}

	changed := false
	rebuilt := make([]json.RawMessage, 0, len(messages))
	for _, msg := range messages {
		if !gjson.GetBytes(msg, "output_config").Exists() {
			rebuilt = append(rebuilt, msg)
			continue
		}
		changed = true

		// 空正文的 system 控制消息整条删除；其余消息只剥字段。
		if gjson.GetBytes(msg, "role").String() == "system" &&
			!anthropicMessageContentHasBody(gjson.GetBytes(msg, "content")) {
			continue
		}
		stripped, err := sjson.DeleteBytes(msg, "output_config")
		if err != nil {
			// 不应发生：字段存在且 msg 是合法 JSON。保守整条保留，不产出半成品。
			rebuilt = append(rebuilt, msg)
			continue
		}
		rebuilt = append(rebuilt, json.RawMessage(stripped))
	}
	if !changed {
		return body, false
	}

	rebuiltBytes, err := json.Marshal(rebuilt)
	if err != nil {
		return body, false
	}
	out, err := sjson.SetRawBytes(body, "messages", rebuiltBytes)
	if err != nil {
		return body, false
	}
	return out, true
}

// anthropicMessageContentHasBody 判断单条消息的 content 是否携带正文。
// 返回 false 表示「无正文」：content 缺失 / null / 空 string / 空 array /
// 仅由空 text 块构成。未知或非 text 内容块（image / tool_use / tool_result 等）
// 一律视为有正文，避免把未来新增内容块误判为空而连带删除整条 system 消息。
func anthropicMessageContentHasBody(content gjson.Result) bool {
	switch {
	case !content.Exists():
		return false
	case content.Type == gjson.String:
		return content.String() != ""
	case content.IsArray():
		var blocks []any
		if err := json.Unmarshal([]byte(content.Raw), &blocks); err != nil {
			return true // 无法解析时保守视为有正文
		}
		cleaned, _ := stripEmptyTextBlocksFromSlice(blocks)
		return len(cleaned) > 0
	default:
		// null 视为无正文；object / number / bool 等非标准形态保守保留。
		return content.Type != gjson.Null
	}
}

// FilterSignatureSensitiveBlocksForRetry keeps tool arguments and results typed;
// a signature error cannot authorize replaying them as ordinary conversation text.
func FilterSignatureSensitiveBlocksForRetry(body []byte, mappedModel string) []byte {
	return FilterThinkingBlocksForRetry(body, mappedModel)
}

func filterThinkingBlocksInternal(body []byte, alwaysThinking bool) []byte {
	// Fast path: if body doesn't contain "thinking", skip parsing
	if !bytes.Contains(body, []byte(`"type":"thinking"`)) &&
		!bytes.Contains(body, []byte(`"type": "thinking"`)) &&
		!bytes.Contains(body, []byte(`"type":"redacted_thinking"`)) &&
		!bytes.Contains(body, []byte(`"type": "redacted_thinking"`)) &&
		!bytes.Contains(body, []byte(`"thinking":`)) &&
		!bytes.Contains(body, []byte(`"thinking" :`)) {
		return body
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}

	// Check if thinking is enabled
	thinkingEnabled := alwaysThinking
	if thinking, ok := req["thinking"].(map[string]any); ok {
		if thinkType, ok := thinking["type"].(string); ok && (thinkType == "enabled" || thinkType == "adaptive") {
			thinkingEnabled = true
		}
	}

	messages, ok := req["messages"].([]any)
	if !ok {
		return body
	}

	filtered := false
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			continue
		}

		role, _ := msgMap["role"].(string)
		content, ok := msgMap["content"].([]any)
		if !ok {
			continue
		}

		newContent := make([]any, 0, len(content))
		filteredThisMessage := false

		for _, block := range content {
			blockMap, ok := block.(map[string]any)
			if !ok {
				newContent = append(newContent, block)
				continue
			}

			blockType, _ := blockMap["type"].(string)

			if blockType == "thinking" || blockType == "redacted_thinking" {
				// When thinking is enabled and this is an assistant message,
				// only keep thinking blocks with valid signatures
				if thinkingEnabled && role == "assistant" {
					if alwaysThinking && blockType == "redacted_thinking" {
						if data, ok := blockMap["data"].(string); ok && data != "" {
							newContent = append(newContent, block)
							continue
						}
					}
					signature, _ := blockMap["signature"].(string)
					if signature != "" && signature != antigravity.DummyThoughtSignature {
						newContent = append(newContent, block)
						continue
					}
				}
				filtered = true
				filteredThisMessage = true
				continue
			}

			// Handle blocks without type discriminator but with "thinking" key
			if blockType == "" {
				if _, hasThinking := blockMap["thinking"]; hasThinking {
					filtered = true
					filteredThisMessage = true
					continue
				}
			}

			newContent = append(newContent, block)
		}

		if filteredThisMessage {
			msgMap["content"] = newContent
		}
	}

	if !filtered {
		return body
	}
	if alwaysThinking {
		for _, msg := range messages {
			msgMap, ok := msg.(map[string]any)
			if !ok {
				continue
			}
			content, ok := msgMap["content"].([]any)
			if !ok {
				continue
			}
			cleaned := make([]any, 0, len(content))
			for _, block := range content {
				blockMap, ok := block.(map[string]any)
				if !ok {
					cleaned = append(cleaned, block)
					continue
				}
				blockType, _ := blockMap["type"].(string)
				if blockType == "thinking" || blockType == "redacted_thinking" {
					continue
				}
				if blockType == "" {
					if _, hasThinking := blockMap["thinking"]; hasThinking {
						continue
					}
				}
				cleaned = append(cleaned, block)
			}
			if len(cleaned) == 0 {
				placeholder := "(content removed)"
				if role, _ := msgMap["role"].(string); role == "assistant" {
					placeholder = "(assistant content removed)"
				}
				cleaned = append(cleaned, map[string]any{"type": "text", "text": placeholder})
			}
			msgMap["content"] = cleaned
		}
	}

	newBody, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return newBody
}

// NormalizeClaudeOutputEffort normalizes Claude's output_config.effort value.
// Returns nil for empty or unrecognized values.
func NormalizeClaudeOutputEffort(raw string) *string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return nil
	}
	switch value {
	case "low", "medium", "high", "xhigh", "max":
		return &value
	default:
		return nil
	}
}

// DefaultEffortForThinkingEnabled 给"开启了 thinking 但协议层没有 effort 档位概念"
// 的国产模型族返回一个默认 effort 字符串（"high"），用于 usage_log.reasoning_effort
// 字段，避免该字段长期为 NULL 导致用量分析无法区分 thinking 开/关。
//
// 适用范围（按 ResolveThinkingProtocol 的 PassbackRequired 集合做白名单过滤）：
//   - Kimi (kimi-* / moonshot-*)
//   - GLM (glm-*)
//   - MiniMax (minimax-m*)
//   - Qwen thinking 变体 (qwen[1-4]?-*-thinking)
//
// **排除 DeepSeek**：DeepSeek 原生支持 reasoning_effort: high/max，客户端可显式指定，
// 网关不应注入默认值覆盖客户端意图（即便客户端没发，DeepSeek 上游自己会用 high default
// ——但那是上游行为，不是我们的语义注入）。
//
// 适用场景由调用方守卫：仅当 (1) ResolveThinkingProtocol == PassbackRequired
// (2) 已确认 thinking 启用（Anthropic: parsed.ThinkingEnabled；OpenAI: 见
// OpenAIBodyHasThinkingEnabled) (3) 已有 effort 解析返回 nil 三者同时成立时调用。
//
// 返回值固定指向 "high"。理由：Kimi/GLM/MiniMax 启用 thinking 都是"深度推理模式"，
// 等同 Claude/OpenAI 的 high 档位语义；用 high 比 medium/normal 更贴近实际行为，
// 也与 DeepSeek thinking-enabled 的默认 effort 一致。
//
// 未来兼容性：如果这些厂商后续加入真实 effort 档位（如 Kimi 跟进 DeepSeek 的
// reasoning_effort: high/max），客户端开始显式发 effort 值时，调用方的守卫条件 (3)
// 会因 extractor 返回非 nil 而不触发本函数，自动让出。
func DefaultEffortForThinkingEnabled(mappedModel string) *string {
	if ResolveThinkingProtocol(mappedModel) != ThinkingProtocolPassbackRequired {
		return nil
	}
	// DeepSeek 在 PassbackRequired 集合里但有原生 effort 支持，排除。
	if strings.HasPrefix(strings.ToLower(mappedModel), "deepseek-") {
		return nil
	}
	effort := "high"
	return &effort
}

// OpenAIBodyHasThinkingEnabled 检测 OpenAI 协议的请求体里是否启用了 thinking。
//
// 国产 OpenAI-兼容上游（GLM via thinkingFormat=zai / Kimi 等）在请求体里用
// `thinking: {type: "enabled"}` 或 `thinking: {type: "adaptive"}` 表达启用。
// 仅 "enabled" / "adaptive" 视为开启；"disabled" 或缺省 → 视为关闭。
//
// 配合 DefaultEffortForThinkingEnabled 使用：OpenAI 路径上 reasoning_effort 解析为空
// 但本函数返回 true 时，给 usage_log 填默认 effort。
func OpenAIBodyHasThinkingEnabled(body []byte) bool {
	thinkingType := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "thinking.type").String()))
	return thinkingType == "enabled" || thinkingType == "adaptive"
}

// ApplyThinkingEnabledFallback 补丁已解析出的 effort，仅在 effort 为 nil 且
// 检测到 body 里 thinking 启用 + mappedModel 属于国产 passback-required 上游时，
// 返回 DefaultEffortForThinkingEnabled 的默认值（"high"）。不覆盖已解析出的值。
//
// 适用于 OpenAI 网关的多条路径调用方（避免重复的 if-nil 表达式）。
func ApplyThinkingEnabledFallback(effort *string, body []byte, mappedModel string) *string {
	if effort != nil {
		return effort
	}
	if !OpenAIBodyHasThinkingEnabled(body) {
		return nil
	}
	return DefaultEffortForThinkingEnabled(mappedModel)
}

// NormalizeGLMOpenAIReasoningEffort rewrites OpenAI Chat Completions
// reasoning_effort values to the GLM native scale used by z.ai: high/max.
// It only applies to glm-* mapped models and leaves all other providers untouched.
func NormalizeGLMOpenAIReasoningEffort(body []byte, mappedModel string) ([]byte, bool) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(mappedModel)), "glm-") {
		return body, false
	}

	path := "reasoning.effort"
	raw := strings.TrimSpace(gjson.GetBytes(body, path).String())
	if raw == "" {
		path = "reasoning_effort"
		raw = strings.TrimSpace(gjson.GetBytes(body, path).String())
	}
	if raw == "" {
		return body, false
	}

	mapped := normalizeGLMOpenAIReasoningEffort(raw)
	if isGLM53Model(mappedModel) && mapped == "high" && normalizeEffortToken(raw) == "low" {
		mapped = "low"
	}
	if mapped == "" || mapped == raw {
		return body, false
	}

	modified, err := sjson.SetBytes(body, path, mapped)
	if err != nil {
		return body, false
	}
	return modified, true
}

func normalizeEffortToken(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	return strings.NewReplacer("-", "", "_", "", " ", "").Replace(value)
}

func isGLM53Model(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), "glm-5.3")
}

func normalizeGLMOpenAIReasoningEffort(raw string) string {
	value := normalizeEffortToken(raw)
	if value == "" {
		return ""
	}

	switch value {
	case "low", "medium", "high":
		return "high"
	case "xhigh", "extrahigh", "max", "ultracode":
		return "max"
	default:
		return ""
	}
}

// NormalizeGLM53AnthropicThinking maps explicit client thinking effort onto the
// GLM-5.3 Anthropic-compatible scale. Requests without an effort or thinking
// preference are left unchanged so the upstream default remains in effect.
func NormalizeGLM53AnthropicThinking(body []byte, mappedModel string) ([]byte, bool) {
	if !isGLM53Model(mappedModel) {
		return body, false
	}

	raw := gjson.GetBytes(body, "output_config.effort").String()
	if strings.TrimSpace(raw) == "" {
		raw = gjson.GetBytes(body, "thinking.type").String()
	}

	var effort string
	switch normalizeEffortToken(raw) {
	case "disabled", "off", "none", "minimal", "low":
		effort = "low"
	case "enabled", "adaptive", "medium", "high":
		effort = "high"
	case "xhigh", "max", "ultra":
		effort = "max"
	default:
		return body, false
	}

	modified, err := sjson.SetBytes(body, "thinking.type", "enabled")
	if err != nil {
		return body, false
	}
	modified, err = sjson.SetBytes(modified, "output_config.effort", effort)
	if err != nil {
		return body, false
	}
	return modified, true
}

// isThinkingBudgetConstraintError detects whether an upstream error message indicates
// a budget_tokens constraint violation (e.g. "budget_tokens >= 1024").
// Also recognizes Baseten's final-answer reserve constraint.
// For the original budget constraint, all three conditions must be true:
//  1. Contains "budget_tokens" or "budget tokens"
//  2. Contains "thinking"
//  3. Contains ">= 1024" or "greater than or equal to 1024" or ("1024" + "input should be")
func isThinkingBudgetConstraintError(errMsg string) bool {
	m := strings.ToLower(errMsg)
	if isFinalAnswerReserveError(m) {
		return true
	}

	// Condition 1: budget_tokens or budget tokens
	hasBudget := strings.Contains(m, "budget_tokens") || strings.Contains(m, "budget tokens")
	if !hasBudget {
		return false
	}

	// Condition 2: thinking
	if !strings.Contains(m, "thinking") {
		return false
	}

	// Condition 3: constraint indicator
	if strings.Contains(m, ">= 1024") || strings.Contains(m, "greater than or equal to 1024") {
		return true
	}
	if strings.Contains(m, "1024") && strings.Contains(m, "input should be") {
		return true
	}

	return false
}

// isFinalAnswerReserveError matches the specific reserve constraint, rather than
// treating arbitrary reasoning quota or context-length errors as repairable.
func isFinalAnswerReserveError(errMsg string) bool {
	m := strings.ToLower(errMsg)
	return strings.Contains(m, "must be greater than 1024 to reserve tokens for a final answer") &&
		strings.Contains(m, "baseten reasoning is enabled")
}

// NormalizeChineseLLMThinking rewrites the top-level `thinking` object for Chinese
// LLM providers that use Anthropic-compatible endpoints but have different accepted
// values for `thinking.type`. Currently scoped to:
//   - MiniMax M-series (`MiniMax-m*`, covering M2.x / M3 / M3.x): official docs accept
//     only `thinking.type` of "adaptive" or "disabled"; "enabled" is not a valid value
//     and may be rejected/ignored. Pi-ai and other Anthropic-SDK clients default to
//     "enabled" (Anthropic-original) and never auto-rewrite for non-Anthropic models.
//
// Non-MiniMax models (Kimi/GLM/DeepSeek) currently accept "enabled" as-is, so this
// function is intentionally a no-op for them. New Chinese LLM quirks should be
// added here as separate case branches.
//
// Returns (modified body, true) if a rewrite was applied, or (original body, false)
// if no rewrite was needed. Caller should be on the Anthropic forward path AFTER
// FilterThinkingBlocks and BEFORE building the upstream request, only for
// passback-required models (ResolveThinkingProtocol == PassbackRequired).
func NormalizeChineseLLMThinking(body []byte, mappedModel string) ([]byte, bool) {
	modelLower := strings.ToLower(mappedModel)
	if !strings.HasPrefix(modelLower, "minimax-m") {
		return body, false
	}
	thinkingType := gjson.GetBytes(body, "thinking.type").String()
	if thinkingType != "enabled" {
		return body, false
	}
	modified, err := sjson.SetBytes(body, "thinking.type", "adaptive")
	if err != nil {
		return body, false
	}
	return modified, true
}
