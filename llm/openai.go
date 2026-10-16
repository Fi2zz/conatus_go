package llm

import (
	"context"
	"iter"
	"net/http"
	"time"
)

// DefaultUserAgent 是 LLM 请求默认的 User-Agent（与 Dart 侧同值）。
const DefaultUserAgent = "ConatusCode/0.16"

// DefaultTimeout 是等待响应头的缺省上限。
const DefaultTimeout = 60 * time.Second

// OpenAI 是 OpenAI 兼容 provider：Chat Completions / Responses 两种形态的 wire 层。
//
// 字段在构造后只读，因此天然并发安全（无锁，也就没有「持锁调外部代码」的风险）。
// 具体提供商（豆包 / DeepSeek）的默认端点与模型名不在这里：本包只做通用 wire 层，
// 装配（含凭据）由宿主决定。
//
// 零值不可用，请用 [NewOpenAI] 构造。
type OpenAI struct {
	name      string
	baseURL   string
	model     string
	apiKey    string
	userAgent string
	style     APIStyle
	client    *http.Client
	timeout   time.Duration
}

// OpenAIOption 定制 [OpenAI]。
type OpenAIOption func(*OpenAI)

// NewOpenAI 构造 provider：缺省名 `openai`、缺省 UA [DefaultUserAgent]、缺省 chat 形态。
//
// 超时只能落在**自建**的客户端上：注入 [WithHTTPClient] 后 [WithTimeout] 不再生效
// （Go 没有「只限响应头阶段」的 http.Client 开关），本包也不会去改别人给的客户端。
func NewOpenAI(opts ...OpenAIOption) *OpenAI {
	provider := &OpenAI{
		name:      "openai",
		userAgent: DefaultUserAgent,
		style:     StyleChat,
		timeout:   DefaultTimeout,
	}
	for _, opt := range opts {
		opt(provider)
	}
	provider.client = buildClient(provider.client, provider.timeout)
	return provider
}

// WithName 设置提供商名（出现在 [Result.Provider] 与错误信息里）。
func WithName(name string) OpenAIOption { return func(p *OpenAI) { p.name = name } }

// WithBaseURL 设置端点前缀。按 Dart 语义原样拼接，**不要**带尾斜杠。
func WithBaseURL(baseURL string) OpenAIOption { return func(p *OpenAI) { p.baseURL = baseURL } }

// WithModel 设置模型名。
func WithModel(model string) OpenAIOption { return func(p *OpenAI) { p.model = model } }

// WithAPIKey 设置 API Key。
//
// 本包**不读环境变量**（与 Dart 一致）：需要环境变量时由调用方取值后显式传入。
func WithAPIKey(apiKey string) OpenAIOption { return func(p *OpenAI) { p.apiKey = apiKey } }

// WithAPIStyle 选择请求形态（[StyleChat] 或 [StyleResponses]）。
func WithAPIStyle(style APIStyle) OpenAIOption { return func(p *OpenAI) { p.style = style } }

// WithUserAgent 覆盖 User-Agent（可伪装成其它客户端）。
func WithUserAgent(userAgent string) OpenAIOption { return func(p *OpenAI) { p.userAgent = userAgent } }

// WithHTTPClient 注入 HTTP 客户端（测试替身 / 代理）；见 [NewOpenAI] 关于超时的说明。
func WithHTTPClient(client *http.Client) OpenAIOption { return func(p *OpenAI) { p.client = client } }

// WithTimeout 设置等待响应头的上限（缺省 [DefaultTimeout]）。
func WithTimeout(timeout time.Duration) OpenAIOption { return func(p *OpenAI) { p.timeout = timeout } }

// Name 返回提供商名。
func (p *OpenAI) Name() string { return p.name }

// Chat 走流式端点，再把增量累积成一次 [Result]。
//
// 与 Dart 一致：非流式签名只是流式端点的壳，实际请求始终走流式，这样思考过程不会丢。
func (p *OpenAI) Chat(ctx context.Context, messages []Message, opts ...Option) (Result, error) {
	return StreamChatResult(ctx, p, messages, opts...)
}

// ChatStream 发送流式请求。
//
// 恒定产出至少一个事件：成功必产 [StreamDone]，失败先产 (nil, err)。因此
// [StreamChatResult] 的「零事件回退 Chat」不会在这里成环。
func (p *OpenAI) ChatStream(
	ctx context.Context,
	messages []Message,
	opts ...Option,
) iter.Seq2[StreamEvent, error] {
	settings := resolveSettings(opts)
	return func(yield func(StreamEvent, error) bool) {
		stream, err := p.openStream(ctx, messages, settings)
		if err != nil {
			yield(nil, err)
			return
		}
		defer stream.Close()
		state := newStreamState()
		if !p.pumpFrames(stream, state, yield) {
			return
		}
		yield(p.doneEvent(state), nil)
	}
}

// Close 回收空闲连接。
//
// Go 的 http.Client 没有 close（Dart 的 `client.close()` 没有等价物）：这里不废掉 provider，
// 只回收连接，之后仍可继续请求。
func (p *OpenAI) Close() error {
	p.client.CloseIdleConnections()
	return nil
}

// requireKey 校验 API Key：缺失属配置问题（[KindConfig]），重试无意义。
func (p *OpenAI) requireKey() error {
	if p.apiKey != "" {
		return nil
	}
	return &Error{Provider: p.name, Message: "缺少 API Key", Kind: KindConfig}
}

// endpoint 按形态拼端点。
func (p *OpenAI) endpoint() string {
	suffix := "chat/completions"
	if p.style == StyleResponses {
		suffix = "responses"
	}
	return p.baseURL + "/" + suffix
}
