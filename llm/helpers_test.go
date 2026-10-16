package llm_test

import (
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/Fi2zz/conatus_go/llm"
)

// recordedRequest 是一次被假 transport 接住的请求（请求体已读出：它是一次性的流）。
type recordedRequest struct {
	method  string
	url     string
	header  http.Header
	rawBody string
}

// bodyObject 把请求体解成 JSON 对象。
func (r recordedRequest) bodyObject(t *testing.T) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(r.rawBody), &decoded); err != nil {
		t.Fatalf("请求体不是 JSON 对象：%v（原文 %q）", err, r.rawBody)
	}
	return decoded
}

// fakeTransport 是 http.RoundTripper 替身：按脚本回一个预置响应，**不打真实网络**。
//
// 只在测试自己的 goroutine 里被同步调用（迭代器是拉取式的），故不加锁。
type fakeTransport struct {
	status   int
	body     string
	header   http.Header
	err      error
	readErr  error
	spy      *closeSpy
	requests []recordedRequest
}

func (t *fakeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	t.requests = append(t.requests, recordedRequest{
		method:  request.Method,
		url:     request.URL.String(),
		header:  request.Header,
		rawBody: string(raw),
	})
	if t.err != nil {
		return nil, t.err
	}
	return &http.Response{
		StatusCode: t.statusCode(),
		Header:     t.responseHeader(),
		Body:       t.responseBody(),
		Request:    request,
	}, nil
}

func (t *fakeTransport) statusCode() int {
	if t.status == 0 {
		return http.StatusOK
	}
	return t.status
}

func (t *fakeTransport) responseHeader() http.Header {
	if t.header != nil {
		return t.header
	}
	return http.Header{"Content-Type": {"text/event-stream; charset=utf-8"}}
}

func (t *fakeTransport) responseBody() io.ReadCloser {
	if t.readErr != nil {
		// 先给一段好数据再报错：钉「断线前已拿到的增量不许丢」。
		partial := io.MultiReader(strings.NewReader(t.body), iotest.ErrReader(t.readErr))
		return io.NopCloser(partial)
	}
	reader := io.Reader(strings.NewReader(t.body))
	if t.spy != nil {
		t.spy.inner = reader
		return t.spy
	}
	return io.NopCloser(reader)
}

// lastRequest 返回最后一次请求；一次都没有时 ok 为 false。
func (t *fakeTransport) lastRequest() (recordedRequest, bool) {
	if len(t.requests) == 0 {
		return recordedRequest{}, false
	}
	return t.requests[len(t.requests)-1], true
}

// callCount 返回收到的请求数。
func (t *fakeTransport) callCount() int { return len(t.requests) }

// onlyRequest 取出唯一一次请求；一次都没有时报错（用例靠它断言请求确实发出过）。
func onlyRequest(t *testing.T, transport *fakeTransport) recordedRequest {
	t.Helper()
	request, ok := transport.lastRequest()
	if !ok {
		t.Fatal("假 transport 没有收到任何请求")
	}
	return request
}

// closeSpy 记录响应体有没有被关闭：流式路径必须回收连接，不许漏。
type closeSpy struct {
	inner  io.Reader
	closed bool
}

func (s *closeSpy) Read(buffer []byte) (int, error) { return s.inner.Read(buffer) }

func (s *closeSpy) Close() error {
	s.closed = true
	return nil
}

// timeoutError 是 net.Error 的超时形态（假 transport 用它模拟「等响应头超时」）。
type timeoutError struct{}

func (timeoutError) Error() string { return "i/o timeout" }

func (timeoutError) Timeout() bool { return true }

// newTestProvider 复刻 Dart 侧 doubaoProviderForTest 的名称 / 端点 / 模型。
//
// 具体提供商的默认端点与模型名属宿主装配，本包不预置；测 wire 层时用这里的常量对齐 Dart。
func newTestProvider(t *testing.T, transport http.RoundTripper, opts ...llm.OpenAIOption) *llm.OpenAI {
	t.Helper()
	base := []llm.OpenAIOption{
		llm.WithName("doubao"),
		llm.WithBaseURL("https://ark.cn-beijing.volces.com/api/v3"),
		llm.WithModel("doubao-seed-1-8-251228"),
		llm.WithAPIKey("test-key"),
		llm.WithHTTPClient(&http.Client{Transport: transport}),
	}
	provider := llm.NewOpenAI(append(base, opts...)...)
	t.Cleanup(func() { _ = provider.Close() })
	return provider
}

// userMessage 是一条最简用户消息。
func userMessage() []llm.Message {
	return []llm.Message{{Role: llm.RoleUser, Content: "hi"}}
}

// sseBody 把若干载荷拼成 SSE 文本（`data:` 帧 + 空行分隔）。
func sseBody(payloads ...string) string {
	var builder strings.Builder
	for _, payload := range payloads {
		builder.WriteString("data: " + payload + "\n\n")
	}
	return builder.String()
}

// collect 取出流里的全部事件；出错即失败。
func collect(t *testing.T, events iter.Seq2[llm.StreamEvent, error]) []llm.StreamEvent {
	t.Helper()
	gathered := []llm.StreamEvent{}
	for event, err := range events {
		if err != nil {
			t.Fatalf("流式迭代出错：%v", err)
		}
		gathered = append(gathered, event)
	}
	return gathered
}

// joinText 拼出全部正文增量。
func joinText(events []llm.StreamEvent) string {
	var builder strings.Builder
	for _, event := range events {
		if delta, ok := event.(llm.TextDelta); ok {
			builder.WriteString(delta.Text)
		}
	}
	return builder.String()
}

// joinReasoning 拼出全部思考增量。
func joinReasoning(events []llm.StreamEvent) string {
	var builder strings.Builder
	for _, event := range events {
		if delta, ok := event.(llm.ReasoningDelta); ok {
			builder.WriteString(delta.Text)
		}
	}
	return builder.String()
}

// doneOf 取唯一的终态事件。
func doneOf(t *testing.T, events []llm.StreamEvent) llm.StreamDone {
	t.Helper()
	for _, event := range events {
		if done, ok := event.(llm.StreamDone); ok {
			return done
		}
	}
	t.Fatal("流里没有终态事件")
	return llm.StreamDone{}
}

// usageNumber 取用量字段的数值：JSON 解出的数字一律是 float64（Dart 侧是 int）。
func usageNumber(t *testing.T, usage map[string]any, key string) float64 {
	t.Helper()
	value, ok := usage[key].(float64)
	if !ok {
		t.Fatalf("用量字段 %q = %v，期望 JSON 数字", key, usage[key])
	}
	return value
}

// toolNames 取出工具调用名（顺序敏感，用于钉累积顺序）。
func toolNames(calls []llm.ToolCall) []string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, call.Name)
	}
	return names
}

// arrayOf 取出对象里的数组字段。
func arrayOf(t *testing.T, source map[string]any, key string) []any {
	t.Helper()
	items, ok := source[key].([]any)
	if !ok {
		t.Fatalf("字段 %q = %v，期望数组", key, source[key])
	}
	return items
}

// objectAt 取出数组里的对象元素。
func objectAt(t *testing.T, items []any, index int) map[string]any {
	t.Helper()
	item, ok := items[index].(map[string]any)
	if !ok {
		t.Fatalf("元素 %d = %v，期望对象", index, items[index])
	}
	return item
}
