package llm_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Fi2zz/conatus_go/llm"
)

func TestOpenAIEndpointFollowsStyle(t *testing.T) {
	chatTransport := &fakeTransport{body: sseBody("[DONE]")}
	chatProvider := newTestProvider(t, chatTransport)
	if _, err := chatProvider.Chat(context.Background(), userMessage()); err != nil {
		t.Fatalf("chat 形态请求失败：%v", err)
	}
	want := "https://ark.cn-beijing.volces.com/api/v3/chat/completions"
	if got := onlyRequest(t, chatTransport).url; got != want {
		t.Fatalf("端点 = %q，期望 %q", got, want)
	}

	responseTransport := &fakeTransport{body: sseBody("[DONE]")}
	responseProvider := newTestProvider(t, responseTransport, llm.WithAPIStyle(llm.StyleResponses))
	if _, err := responseProvider.Chat(context.Background(), userMessage()); err != nil {
		t.Fatalf("responses 形态请求失败：%v", err)
	}
	wantResponses := "https://ark.cn-beijing.volces.com/api/v3/responses"
	if got := onlyRequest(t, responseTransport).url; got != wantResponses {
		t.Fatalf("端点 = %q，期望 %q", got, wantResponses)
	}
}

func TestOpenAIRequestHeaders(t *testing.T) {
	transport := &fakeTransport{body: sseBody("[DONE]")}
	provider := newTestProvider(t, transport)

	if _, err := provider.Chat(context.Background(), userMessage()); err != nil {
		t.Fatalf("请求失败：%v", err)
	}

	header := onlyRequest(t, transport).header
	if got := header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := header.Get("Authorization"); got != "Bearer test-key" {
		t.Fatalf("Authorization = %q，期望带 Bearer 前缀", got)
	}
	if got := header.Get("User-Agent"); got != llm.DefaultUserAgent {
		t.Fatalf("User-Agent = %q", got)
	}
	if llm.DefaultUserAgent != "ConatusCode/0.16" {
		t.Fatalf("缺省 UA = %q，应与 Dart 侧同值", llm.DefaultUserAgent)
	}
}

func TestOpenAIUserAgentOverride(t *testing.T) {
	transport := &fakeTransport{body: sseBody("[DONE]")}
	provider := newTestProvider(t, transport, llm.WithUserAgent("dsh/0.1.2"))

	if _, err := provider.Chat(context.Background(), userMessage()); err != nil {
		t.Fatalf("请求失败：%v", err)
	}

	if got := onlyRequest(t, transport).header.Get("User-Agent"); got != "dsh/0.1.2" {
		t.Fatalf("User-Agent = %q，期望自定义值", got)
	}
}

// 注入客户端时以注入的为准：本包不该去改别人客户端的配置（超时也一样）。
func TestOpenAIUsesInjectedClient(t *testing.T) {
	transport := &fakeTransport{body: sseBody(
		`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
		"[DONE]",
	)}
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	provider := llm.NewOpenAI(
		llm.WithName("doubao"),
		llm.WithBaseURL("https://ark.cn-beijing.volces.com/api/v3"),
		llm.WithModel("m"),
		llm.WithAPIKey("k"),
		llm.WithHTTPClient(client),
		llm.WithTimeout(2*time.Second),
	)
	t.Cleanup(func() { _ = provider.Close() })

	result, err := provider.Chat(context.Background(), userMessage())

	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	if result.Content != "ok" || result.Provider != "doubao" {
		t.Fatalf("结果 = %+v", result)
	}
	if transport.callCount() != 1 {
		t.Fatalf("请求数 = %d，期望走注入的客户端", transport.callCount())
	}
}

// 不注入客户端时自有缺省：缺省 UA、缺省形态、可安全关闭（超时落在自建 transport 的
// 响应头阶段，见 WithTimeout 的注释）。
func TestOpenAINewOpenAIHasUsableDefaults(t *testing.T) {
	provider := llm.NewOpenAI(
		llm.WithName("doubao"),
		llm.WithBaseURL("https://ark.cn-beijing.volces.com/api/v3"),
		llm.WithModel("m"),
		llm.WithAPIKey("k"),
		llm.WithTimeout(2*time.Second),
	)

	if provider.Name() != "doubao" {
		t.Fatalf("名称 = %q", provider.Name())
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("关闭失败：%v", err)
	}
	if other := llm.NewOpenAI(); other.Name() != "openai" {
		t.Fatalf("缺省名称 = %q，期望 openai", other.Name())
	}
}

func TestOpenAIMissingKeyFailsBeforeSending(t *testing.T) {
	transport := &fakeTransport{body: sseBody("[DONE]")}
	provider := newTestProvider(t, transport, llm.WithAPIKey(""))

	_, err := provider.Chat(context.Background(), userMessage())

	var llmError *llm.Error
	if !errors.As(err, &llmError) {
		t.Fatalf("错误 = %v，期望 *llm.Error", err)
	}
	if llmError.ErrorKind() != llm.KindConfig {
		t.Fatalf("类别 = %v，期望配置问题", llmError.ErrorKind())
	}
	if !strings.Contains(llmError.Message, "API Key") {
		t.Fatalf("文案 = %q，期望提到 API Key", llmError.Message)
	}
	if transport.callCount() != 0 {
		t.Fatal("缺 Key 不该发请求：发了也只会被拒，还白耗一次配额")
	}
}

func TestOpenAIStatusErrorCarriesMessageAndRetryAfter(t *testing.T) {
	transport := &fakeTransport{
		status: http.StatusTooManyRequests,
		body:   `{"error":{"message":"请求过于频繁"}}`,
		header: http.Header{"Retry-After": {"7"}},
	}
	provider := newTestProvider(t, transport)

	_, err := provider.Chat(context.Background(), userMessage())

	var llmError *llm.Error
	if !errors.As(err, &llmError) {
		t.Fatalf("错误 = %v，期望 *llm.Error", err)
	}
	if llmError.StatusCode != http.StatusTooManyRequests || llmError.Message != "请求过于频繁" {
		t.Fatalf("错误 = %+v", llmError)
	}
	if llmError.RetryAfter != 7*time.Second {
		t.Fatalf("Retry-After = %v，期望 7s", llmError.RetryAfter)
	}
	if llmError.ErrorKind() != llm.KindRateLimit || !llmError.Retryable() {
		t.Fatalf("类别 = %v，期望可重试的限流", llmError.ErrorKind())
	}
}

func TestOpenAIErrorTextFallsBackToRawBody(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"顶层 message 字段", 400, `{"message":"参数不对"}`, "参数不对"},
		{"原始 HTML 原样透出", 502, "<html>bad gateway</html>", "<html>bad gateway</html>"},
		{"空 body 用状态码", 500, "", "HTTP 500"},
	}
	for _, item := range cases {
		transport := &fakeTransport{status: item.status, body: item.body}
		provider := newTestProvider(t, transport)

		_, err := provider.Chat(context.Background(), userMessage())

		var llmError *llm.Error
		if !errors.As(err, &llmError) {
			t.Fatalf("%s：错误 = %v，期望 *llm.Error", item.name, err)
		}
		if llmError.Message != item.want {
			t.Fatalf("%s：文案 = %q，期望 %q", item.name, llmError.Message, item.want)
		}
	}
}

func TestOpenAITransportFailureIsRetryable(t *testing.T) {
	cases := []struct {
		name         string
		err          error
		wantContains string
	}{
		{"超时", timeoutError{}, "请求超时"},
		{"上下文超时", context.DeadlineExceeded, "请求超时"},
		{"连接被拒", errors.New("connection refused"), "网络错误"},
	}
	for _, item := range cases {
		transport := &fakeTransport{err: item.err}
		provider := newTestProvider(t, transport)

		_, err := provider.Chat(context.Background(), userMessage())

		var llmError *llm.Error
		if !errors.As(err, &llmError) {
			t.Fatalf("%s：错误 = %v，期望 *llm.Error", item.name, err)
		}
		if llmError.ErrorKind() != llm.KindNetwork || !llmError.Retryable() {
			t.Fatalf("%s：类别 = %v，期望可重试的网络异常", item.name, llmError.ErrorKind())
		}
		if !strings.Contains(llmError.Message, item.wantContains) {
			t.Fatalf("%s：文案 = %q，期望含 %q", item.name, llmError.Message, item.wantContains)
		}
	}
}

// 请求构造阶段就失败时**不许**发请求：options 里塞了不可编码的值、端点根本拼不出来。
func TestOpenAIFailsBeforeSendingWhenRequestIsUnbuildable(t *testing.T) {
	cases := []struct {
		name         string
		providerOpts []llm.OpenAIOption
		requestOpts  []llm.Option
	}{
		{
			"options 不可编码",
			nil,
			[]llm.Option{llm.WithOptions(map[string]any{"bad": make(chan int)})},
		},
		{
			"端点不合法",
			[]llm.OpenAIOption{llm.WithBaseURL("://坏端点")},
			nil,
		},
	}
	for _, item := range cases {
		transport := &fakeTransport{body: sseBody("[DONE]")}
		provider := newTestProvider(t, transport, item.providerOpts...)

		_, err := provider.Chat(context.Background(), userMessage(), item.requestOpts...)

		var llmError *llm.Error
		if !errors.As(err, &llmError) {
			t.Fatalf("%s：错误 = %v，期望 *llm.Error", item.name, err)
		}
		if llmError.ErrorKind() != llm.KindConfig {
			t.Fatalf("%s：类别 = %v，期望配置问题", item.name, llmError.ErrorKind())
		}
		if transport.callCount() != 0 {
			t.Fatalf("%s：不该发出半截请求", item.name)
		}
	}
}

func TestOpenAIChatUsesStreamingEndpoint(t *testing.T) {
	transport := &fakeTransport{body: sseBody(
		`{"choices":[{"delta":{"content":"流式拿到的"},"finish_reason":"stop"}]}`,
		"[DONE]",
	)}
	provider := newTestProvider(t, transport)

	result, err := provider.Chat(context.Background(), userMessage())

	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	if result.Content != "流式拿到的" || result.Provider != "doubao" || result.Model != "doubao-seed-1-8-251228" {
		t.Fatalf("结果 = %+v", result)
	}
	if body := onlyRequest(t, transport).bodyObject(t); body["stream"] != true {
		t.Fatal("非流式签名也必须走流式端点（这样思考过程不会丢）")
	}
}
