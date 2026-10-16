package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"net/http"
	"strings"
	"testing"

	"github.com/Fi2zz/conatus_go/agent"
	"github.com/Fi2zz/conatus_go/core"
	"github.com/Fi2zz/conatus_go/foundation/session"
	"github.com/Fi2zz/conatus_go/foundation/tools"
	"github.com/Fi2zz/conatus_go/llm"
)

// scriptedProvider 是脚本化模型：按脚本逐次给回复，脚本用尽后停在最后一个动作上。
//
// 它记录每次请求收到的消息，用例据此断言「模型看到了什么」。
type scriptedProvider struct {
	replies []llm.Result
	// err 非空时每次调用都直接失败（用来验「非取消的错误原样上抛」）。
	err error
	// onCall 在每次调用返回前执行（用来在轮内制造「会话被关」「ctx 被取消」这类时序）。
	onCall func()
	calls  int
	seen   [][]llm.Message
}

func (p *scriptedProvider) Name() string { return "scripted" }

func (p *scriptedProvider) Chat(
	ctx context.Context, messages []llm.Message, _ ...llm.Option,
) (llm.Result, error) {
	if err := ctx.Err(); err != nil {
		// 尊重 ctx 的替身：真实 provider 在取消后就是这个行为。
		return llm.Result{}, err
	}
	p.seen = append(p.seen, append([]llm.Message(nil), messages...))
	index := p.calls
	p.calls++
	if p.onCall != nil {
		p.onCall()
	}
	if p.err != nil {
		return llm.Result{}, p.err
	}
	if index >= len(p.replies) {
		index = len(p.replies) - 1
	}
	return p.replies[index], nil
}

func (p *scriptedProvider) ChatStream(
	ctx context.Context, messages []llm.Message, opts ...llm.Option,
) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		result, err := p.Chat(ctx, messages, opts...)
		if err != nil {
			yield(nil, err)
			return
		}
		if !yield(llm.TextDelta{Text: result.Content}, nil) {
			return
		}
		yield(llm.StreamDone{
			Usage:     result.Usage,
			ToolCalls: result.ToolCalls,
			Provider:  result.Provider,
			Model:     result.Model,
		}, nil)
	}
}

// textResult 构造一次直答。
func textResult(content string) llm.Result {
	return llm.Result{
		Content:  content,
		Provider: "scripted",
		Model:    "m",
		Usage:    map[string]any{"total_tokens": 1},
	}
}

// toolCallResult 构造一次「要求调用工具」的回复。
func toolCallResult(calls ...llm.ToolCall) llm.Result {
	return llm.Result{Content: "先调工具", ToolCalls: calls, Provider: "scripted", Model: "m"}
}

// toolCall 构造一次工具调用请求。
func toolCall(id, name, arguments string) llm.ToolCall {
	return llm.ToolCall{ID: id, Name: name, Arguments: arguments}
}

// newEchoRegistry 建一个注册表并登记 echo 工具：记录每次拿到的解析后参数，并在工具体里
// 依次执行 hooks（用例用它制造「工具执行期间关会话 / 取消 ctx」这类时序）。
func newEchoRegistry(t *testing.T, arguments *[]map[string]any, hooks ...func()) *tools.Registry {
	t.Helper()
	registry := tools.NewRegistry()
	_, err := registry.Fn("echo", "回显输入",
		func(_ context.Context, tc *tools.ToolContext) (tools.ToolResult, error) {
			*arguments = append(*arguments, tc.Arguments())
			for _, hook := range hooks {
				hook()
			}
			text, _ := tc.String("text")
			return tools.Success("echo:" + text), nil
		},
		tools.WithFnParams(tools.StringParam("text", tools.Required(), tools.Describe("要回显的文本"))),
	)
	if err != nil {
		t.Fatalf("登记 echo 工具失败：%v", err)
	}
	return registry
}

// newSession 建一个不落盘的会话。
func newSession(t *testing.T, id string) *session.Session {
	t.Helper()
	target, err := session.New(id)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}
	return target
}

// eventTypes 取会话事件的类型序列。
func eventTypes(target *session.Session) []string {
	types := []string{}
	for _, event := range target.Events() {
		types = append(types, event.Type)
	}
	return types
}

// eventData 取会话事件的数据负载。
func eventData(t *testing.T, event session.Event) map[string]any {
	t.Helper()
	data, ok := event.Data.(map[string]any)
	if !ok {
		t.Fatalf("事件 %s 的负载 = %v，期望对象", event.Type, event.Data)
	}
	return data
}

// scriptedTransport 是 http.RoundTripper 替身：回一段预置 SSE 帧并记录请求体，不打真实网络。
type scriptedTransport struct {
	frames   []string
	requests []map[string]any
}

func (t *scriptedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	t.requests = append(t.requests, body)

	var sse strings.Builder
	for _, frame := range t.frames {
		sse.WriteString("data: " + frame + "\n\n")
	}
	sse.WriteString("data: [DONE]\n\n")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(sse.String())),
		Request:    request,
	}, nil
}

// lastRequest 取最后一次请求体；一次都没有时报错。
func lastRequest(t *testing.T, transport *scriptedTransport) map[string]any {
	t.Helper()
	if len(transport.requests) == 0 {
		t.Fatal("假 transport 没有收到任何请求")
	}
	return transport.requests[len(transport.requests)-1]
}

// recordingTracker 记录 begin / end 的调用。
type recordingTracker struct {
	begun []string
	ended []string
}

func (t *recordingTracker) BeginTurn(_ context.Context, userInput string) error {
	t.begun = append(t.begun, userInput)
	return nil
}

func (t *recordingTracker) EndTurn(_ context.Context, reply string, _ error) error {
	t.ended = append(t.ended, reply)
	return nil
}

// errTracker 是追踪器自身的失败。
var errTracker = errors.New("追踪器坏了")

// failingTracker 的 EndTurn 总是失败（begin 正常）。
type failingTracker struct{}

func (failingTracker) BeginTurn(context.Context, string) error { return nil }

func (failingTracker) EndTurn(context.Context, string, error) error { return errTracker }

// beginFailingTracker 的 BeginTurn 就失败。
type beginFailingTracker struct{}

func (beginFailingTracker) BeginTurn(context.Context, string) error { return errTracker }

func (beginFailingTracker) EndTurn(context.Context, string, error) error { return nil }

// provideLoopWithoutPrompt 在上下文里提供 llm 与 tools（**不提供** prompt 注册表）并装配循环。
func provideLoopWithoutPrompt(t *testing.T, provider llm.Provider) *agent.Loop {
	t.Helper()
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	if _, err := llm.Provide(ctx, provider); err != nil {
		t.Fatalf("provide llm 失败：%v", err)
	}
	if _, err := tools.Provide(ctx); err != nil {
		t.Fatalf("provide tools 失败：%v", err)
	}
	loop, err := agent.Provide(ctx)
	if err != nil {
		t.Fatalf("provide agent 失败：%v", err)
	}
	return loop
}
