package llm

import (
	"context"
	"iter"
	"strings"
)

// Provider 是最小契约：名字、非流式补全、流式补全。
//
// 与 Dart 的差别：`close()` 移出主接口——Go 接口不能给方法默认实现，而绝大多数
// 实现（含测试替身）没有资源要释放。需要释放资源的实现满足 [Closer]，由 [Close] 处理。
type Provider interface {
	// Name 是提供商名（出现在 [Result.Provider] 与错误信息里）。
	Name() string
	// Chat 发送非流式聊天补全请求。
	//
	// tools 非空时以原生 function calling 下发；结果里的工具调用见 [Result.ToolCalls]。
	Chat(ctx context.Context, messages []Message, opts ...Option) (Result, error)
	// ChatStream 发送流式聊天补全请求。
	//
	// 拉取式迭代器（不是 Dart 的推式 Stream）：消费方按自己的节奏取，取消通过
	// ctx 传播（T1 定论 D-T1-1）。工具调用以 JSON 分片到达，攒到流结束后由终态
	// [StreamDone.ToolCalls] 一次性给出。
	ChatStream(ctx context.Context, messages []Message, opts ...Option) iter.Seq2[StreamEvent, error]
}

// Closer 是可选接口：实现了就在释放时被关掉（如关闭复用的 HTTP 客户端）。
type Closer interface {
	Close() error
}

// Close 关闭 provider；未实现 [Closer] 时什么也不做。
func Close(provider Provider) error {
	if closer, ok := provider.(Closer); ok {
		return closer.Close()
	}
	return nil
}

// Option 定制一次请求。
type Option func(*settings)

type settings struct {
	values   map[string]any
	tools    []map[string]any
	observer func(StreamEvent)
}

// WithOptions 附加请求参数（如 temperature / max_tokens），原样并入请求体。
func WithOptions(values map[string]any) Option {
	return func(s *settings) { s.values = values }
}

// WithTools 下发工具 schema 列表（`{name, description, parameters}`）。
func WithTools(tools []map[string]any) Option {
	return func(s *settings) { s.tools = tools }
}

// WithObserver 把每个流式事件透传给宿主（实时渲染用）；只有 [StreamChatResult] 读它。
func WithObserver(observe func(StreamEvent)) Option {
	return func(s *settings) { s.observer = observe }
}

func resolveSettings(opts []Option) settings {
	settings := settings{}
	for _, opt := range opts {
		opt(&settings)
	}
	return settings
}

// StreamChatResult 以流式方式调用 provider，把增量累积成一次 [Result]。
//
// 正文、思考与工具调用都从流式事件累积，供依赖非流式签名的调用方复用——实际 API
// 请求始终走流式端点，思考过程不会丢失。[WithObserver] 可把事件透传给宿主。
//
// **一个事件都没收到时回退调用非流式 [Provider.Chat]**：只实现 Chat 的测试替身靠这条
// 工作，行为与直接调用 Chat 一致。
//
// [Result.Provider] 取 `provider.Name()`（与 Dart 一致）；[StreamDone.Provider] 记录
// 的是实际产出该流的提供商（诊断用），不覆盖前者。
func StreamChatResult(
	ctx context.Context,
	provider Provider,
	messages []Message,
	opts ...Option,
) (Result, error) {
	settings := resolveSettings(opts)
	text := &strings.Builder{}
	reasoning := &strings.Builder{}
	toolCalls := []ToolCall(nil)
	usage := map[string]any(nil)
	model := ""
	sawEvent := false

	for event, err := range provider.ChatStream(ctx, messages, opts...) {
		if err != nil {
			return Result{}, err
		}
		sawEvent = true
		if settings.observer != nil {
			settings.observer(event)
		}
		switch typed := event.(type) {
		case TextDelta:
			text.WriteString(typed.Text)
		case ReasoningDelta:
			reasoning.WriteString(typed.Text)
		case StreamDone:
			toolCalls = typed.ToolCalls
			usage = typed.Usage
			model = typed.Model
		}
	}

	if !sawEvent {
		return provider.Chat(ctx, messages, opts...)
	}
	return Result{
		Content:   text.String(),
		Provider:  provider.Name(),
		Model:     model,
		Usage:     usage,
		ToolCalls: toolCalls,
		Reasoning: reasoning.String(),
	}, nil
}

// streamRequest 是一次流式请求的输入（只被装饰器透传，不解读）。
//
// 收成一个结构是为了让 [pumpStream] 的参数不超过四个。
type streamRequest struct {
	messages []Message
	options  []Option
}

// pumpStream 跑一次流式尝试：把事件转发给消费方。
//
// 返回「是否产出过事件」「消费方是否提前停止」与错误。装饰器（[Retrying] / [Fallback]）
// 靠前者决定「还能不能重来或换一家」——已经交出去的增量收不回来；靠后者决定「是否直接
// 收工」，因为消费方都停下了，再试也只是白花配额。
func pumpStream(
	ctx context.Context,
	provider Provider,
	request streamRequest,
	yield func(StreamEvent, error) bool,
) (bool, bool, error) {
	emitted := false
	for event, err := range provider.ChatStream(ctx, request.messages, request.options...) {
		if err != nil {
			return emitted, false, err
		}
		emitted = true
		if !yield(event, nil) {
			return emitted, true, nil
		}
	}
	return emitted, false, nil
}
