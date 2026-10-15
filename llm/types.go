// Package llm 是统一的大模型接入层（对应 Dart 的 conatus_llm）。
//
// 支持 OpenAI 兼容的两种请求形态（[APIStyle]）：
//
//   - [StyleChat]：`{baseURL}/chat/completions`，消息用 `messages`；
//   - [StyleResponses]：`{baseURL}/responses`，消息用 `input`。
//
// 非流式走 Provider.Chat，流式走 Provider.ChatStream。流式按 T1 定论 D-T1-1 用
// `iter.Seq2[StreamEvent, error]`（拉取式 + `context.Context` 取消），不做 Dart 的
// `Stream` 广播语义。
//
// 本文件只放**类型层**（消息 / 工具调用 / 图片 / 结果 / 流式事件与它们的 JSON 投影）；
// Provider 契约与 `StreamChatResult` 累积器、OpenAI 兼容实现、重试与回退装饰器
// 分别在 `provider.go` / `openai.go` / `retry.go` / `fallback.go`。
package llm

// APIStyle 是 OpenAI 兼容端点的请求形态。
type APIStyle int

const (
	// StyleChat 使用 `{baseURL}/chat/completions` 与 `messages`。
	StyleChat APIStyle = iota
	// StyleResponses 使用 `{baseURL}/responses` 与 `input`。
	StyleResponses
)

func (s APIStyle) String() string {
	if s == StyleResponses {
		return "responses"
	}
	return "chat"
}

// 消息角色。
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// ToolCall 是模型请求的一次工具调用。
type ToolCall struct {
	// ID 是调用标识，与工具结果消息的 [Message.ToolCallID] 配对。
	ID string
	// Name 是工具名。
	Name string
	// Arguments 是原始 JSON 参数串（未解析）。
	Arguments string
}

// JSON 编译为 Chat Completions 的 tool_call 项。
func (c ToolCall) JSON() map[string]any {
	return map[string]any{
		"id":   c.ID,
		"type": "function",
		"function": map[string]any{
			"name":      c.Name,
			"arguments": c.Arguments,
		},
	}
}

// Image 是聊天消息携带的图片（多模态输入）。
type Image struct {
	// MIMEType 是图片 MIME 类型，如 `image/png`。
	MIMEType string
	// Base64Data 是图片字节的 base64 编码（不含 data URL 前缀）。
	Base64Data string
}

// DataURL 返回 data URL 形式，供 Chat Completions 的 `image_url` 与 Responses 的
// `input_image` 共用。
func (i Image) DataURL() string {
	return "data:" + i.MIMEType + ";base64," + i.Base64Data
}

// Message 是聊天消息。
//
//	用户消息：  Message{Role: RoleUser, Content: "你好"}
//	带图消息：  Message{Role: RoleUser, Content: "看这张图", Images: []Image{...}}
//	助手工具调用：Message{Role: RoleAssistant, ToolCalls: []ToolCall{...}}
//	工具结果：  Message{Role: RoleTool, Content: "结果", ToolCallID: "call_1"}
type Message struct {
	Role    string
	Content string
	// ToolCalls 是助手消息请求的工具调用。
	ToolCalls []ToolCall
	// ToolCallID 是工具结果消息对应的调用 id（Role 为 [RoleTool] 时必填）。
	ToolCallID string
	// Cacheable 标记该消息属于可缓存的稳定前缀（如 system prompt + 工具定义）。
	//
	// 纯本地标记，**不会**写入请求体：豆包 / DeepSeek 的前缀缓存由服务端自动生效，
	// 塞入非标字段可能被拒。它只用于缓存装饰器计算连续可缓存前缀与缓存度量。
	Cacheable bool
	// Images 是消息携带的图片；为空时消息退化为纯文本。
	Images []Image
}

// JSON 编译为 Chat Completions 的消息项。
func (m Message) JSON() map[string]any {
	fields := map[string]any{"role": m.Role}
	if len(m.Images) == 0 {
		fields["content"] = m.Content
	} else {
		parts := make([]any, 0, len(m.Images)+1)
		parts = append(parts, map[string]any{"type": "text", "text": m.Content})
		for _, image := range m.Images {
			parts = append(parts, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": image.DataURL()},
			})
		}
		fields["content"] = parts
	}
	if m.ToolCallID != "" {
		fields["tool_call_id"] = m.ToolCallID
	}
	if len(m.ToolCalls) > 0 {
		calls := make([]any, 0, len(m.ToolCalls))
		for _, call := range m.ToolCalls {
			calls = append(calls, call.JSON())
		}
		fields["tool_calls"] = calls
	}
	return fields
}

// Result 是一次聊天补全的结果。
type Result struct {
	Content  string
	Provider string
	Model    string
	Usage    map[string]any
	// ToolCalls 是模型请求的工具调用；无工具调用时为空。
	ToolCalls []ToolCall
	// Reasoning 是模型的思考过程（如 Kimi 的 `reasoning_content`）；未输出时为空串。
	Reasoning string
}

// StreamEvent 是流式事件（对应 Dart 的 sealed class）。
//
// 三个实现：[TextDelta]（正文增量）、[ReasoningDelta]（思考增量）、
// [StreamDone]（流结束，携带累计用量、结束原因与工具调用）。
type StreamEvent interface {
	isStreamEvent()
}

// TextDelta 是正文增量。
type TextDelta struct {
	Text string
}

func (TextDelta) isStreamEvent() {}

// ReasoningDelta 是思考增量（如豆包 `reasoning_content` / Responses 的 reasoning 事件）。
type ReasoningDelta struct {
	Text string
}

func (ReasoningDelta) isStreamEvent() {}

// StreamDone 是流结束事件。
//
// 由 ChatStream 在流末尾产出一次；若流中途失败则不产出。
type StreamDone struct {
	Usage map[string]any
	// FinishReason 为结束原因；未知时为空串。
	FinishReason string
	// ToolCalls 是流式累积完成的工具调用。
	//
	// 工具参数以 JSON 分片到达，须攒到流结束才能得到完整调用，故在终态一次性给出。
	ToolCalls []ToolCall
	// Provider 是产出该流的提供商名（供累积器还原 [Result]）。
	Provider string
	// Model 是产出该流的模型名；未知时为空串。
	Model string
}

func (StreamDone) isStreamEvent() {}
