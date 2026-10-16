package llm_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Fi2zz/conatus_go/llm"
)

func TestOpenAIChatStreamYieldsReasoningThenContent(t *testing.T) {
	transport := &fakeTransport{body: sseBody(
		`{"choices":[{"delta":{"reasoning_content":"先想"}}]}`,
		`{"choices":[{"delta":{"reasoning_content":"再想"}}]}`,
		`{"choices":[{"delta":{"content":"回答"},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
		"[DONE]",
	)}
	provider := newTestProvider(t, transport)

	events := collect(t, provider.ChatStream(context.Background(), userMessage()))

	want := []llm.StreamEvent{
		llm.ReasoningDelta{Text: "先想"},
		llm.ReasoningDelta{Text: "再想"},
		llm.TextDelta{Text: "回答"},
		llm.StreamDone{
			Usage: map[string]any{
				"prompt_tokens":     float64(1),
				"completion_tokens": float64(2),
				"total_tokens":      float64(3),
			},
			FinishReason: "stop",
			Provider:     "doubao",
			Model:        "doubao-seed-1-8-251228",
		},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("事件 = %#v，期望 %#v", events, want)
	}
}

func TestOpenAIChatStreamSkipsUnusablePayloads(t *testing.T) {
	transport := &fakeTransport{body: sseBody(
		": 心跳",
		"不是 JSON",
		`[1,2,3]`,
		`null`,
		`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
		"[DONE]",
	)}
	provider := newTestProvider(t, transport)

	events := collect(t, provider.ChatStream(context.Background(), userMessage()))

	if got := joinText(events); got != "ok" {
		t.Fatalf("正文 = %q，期望跳过不可用载荷后仍拼出 ok", got)
	}
	if done := doneOf(t, events); done.FinishReason != "stop" {
		t.Fatalf("结束原因 = %q", done.FinishReason)
	}
}

func TestOpenAIChatStreamEmitsDoneEvenWithoutFrames(t *testing.T) {
	transport := &fakeTransport{body: sseBody("[DONE]")}
	provider := newTestProvider(t, transport)

	events := collect(t, provider.ChatStream(context.Background(), userMessage()))

	if len(events) != 1 {
		t.Fatalf("事件数 = %d，期望只有终态一件", len(events))
	}
	done := doneOf(t, events)
	if done.FinishReason != "" || done.Usage != nil || done.ToolCalls != nil {
		t.Fatalf("空流的终态 = %+v", done)
	}
}

func TestOpenAIChatStreamClosesResponseBody(t *testing.T) {
	spy := &closeSpy{}
	transport := &fakeTransport{
		spy:  spy,
		body: sseBody(`{"choices":[{"delta":{"content":"a"}}]}`, "[DONE]"),
	}
	provider := newTestProvider(t, transport)

	collect(t, provider.ChatStream(context.Background(), userMessage()))

	if !spy.closed {
		t.Fatal("流结束后必须关掉响应体，否则连接回收不了")
	}
}

func TestOpenAIChatStreamClosesBodyWhenConsumerStops(t *testing.T) {
	spy := &closeSpy{}
	transport := &fakeTransport{
		spy: spy,
		body: sseBody(
			`{"choices":[{"delta":{"content":"a"}}]}`,
			`{"choices":[{"delta":{"content":"b"}}]}`,
			"[DONE]",
		),
	}
	provider := newTestProvider(t, transport)

	for event, err := range provider.ChatStream(context.Background(), userMessage()) {
		if err != nil {
			t.Fatalf("流式迭代出错：%v", err)
		}
		if _, ok := event.(llm.TextDelta); ok {
			break
		}
	}

	if !spy.closed {
		t.Fatal("消费方提前停止时也必须关掉响应体")
	}
}

// 流中途断线：断线前拿到的增量照常产出，读错误原样上报（不许吞掉，也不许当成正常收尾）。
func TestOpenAIChatStreamReportsReadFailure(t *testing.T) {
	boom := errors.New("连接中断")
	transport := &fakeTransport{
		readErr: boom,
		body:    sseBody(`{"choices":[{"delta":{"content":"半"}}]}`),
	}
	provider := newTestProvider(t, transport)

	events := []llm.StreamEvent{}
	var failure error
	for event, err := range provider.ChatStream(context.Background(), userMessage()) {
		if err != nil {
			failure = err
			break
		}
		events = append(events, event)
	}

	if got := joinText(events); got != "半" {
		t.Fatalf("断线前的正文 = %q，期望保留", got)
	}
	if !errors.Is(failure, boom) {
		t.Fatalf("错误 = %v，期望原样上报读失败", failure)
	}
	if len(events) != 1 {
		t.Fatalf("断线时不该产出终态事件：%+v", events)
	}
}

func TestOpenAIResponsesStreamParsesEvents(t *testing.T) {
	transport := &fakeTransport{body: sseBody(
		`{"type":"response.output_text.delta","delta":"你"}`,
		`{"type":"response.reasoning_summary_text.delta","delta":"想"}`,
		`{"type":"response.reasoning_text.delta","delta":"法"}`,
		`{"type":"response.reasoning_text.delta","delta":""}`,
		`{"type":"response.output_text.delta","delta":"好"}`,
		`{"type":"response.output_text.delta","delta":""}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message"}}`,
		`{"type":"response.unknown.event","delta":"忽略"}`,
		`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`,
	)}
	provider := newTestProvider(t, transport, llm.WithAPIStyle(llm.StyleResponses))

	events := collect(t, provider.ChatStream(context.Background(), userMessage()))

	if got := joinText(events); got != "你好" {
		t.Fatalf("正文 = %q，期望 你好", got)
	}
	if got := joinReasoning(events); got != "想法" {
		t.Fatalf("思考 = %q，期望 想法", got)
	}
	done := doneOf(t, events)
	if done.FinishReason != "completed" {
		t.Fatalf("结束原因 = %q，期望 completed", done.FinishReason)
	}
	if got := usageNumber(t, done.Usage, "total_tokens"); got != 3 {
		t.Fatalf("用量 = %v，期望 3", got)
	}
}

func TestOpenAIResponsesStreamMapsIncompleteToLength(t *testing.T) {
	transport := &fakeTransport{body: sseBody(
		`{"type":"response.output_text.delta","delta":"半"}`,
		`{"type":"response.incomplete","response":{"status":"incomplete"}}`,
	)}
	provider := newTestProvider(t, transport, llm.WithAPIStyle(llm.StyleResponses))

	events := collect(t, provider.ChatStream(context.Background(), userMessage()))

	if got := doneOf(t, events).FinishReason; got != "length" {
		t.Fatalf("结束原因 = %q，期望 length", got)
	}
}

func TestOpenAIResponsesStreamReportsFailure(t *testing.T) {
	transport := &fakeTransport{body: sseBody(
		`{"type":"response.output_text.delta","delta":"半"}`,
		`{"type":"response.failed","response":{"error":{"message":"上游崩了"}}}`,
	)}
	provider := newTestProvider(t, transport, llm.WithAPIStyle(llm.StyleResponses))

	var failure error
	for _, err := range provider.ChatStream(context.Background(), userMessage()) {
		if err != nil {
			failure = err
		}
	}

	var llmError *llm.Error
	if !errors.As(failure, &llmError) {
		t.Fatalf("错误 = %v，期望 *llm.Error", failure)
	}
	if llmError.ErrorKind() != llm.KindServer || !llmError.Retryable() {
		t.Fatalf("类别 = %v，期望可重试的服务端错误", llmError.ErrorKind())
	}
	if !strings.Contains(llmError.Message, "上游崩了") {
		t.Fatalf("文案 = %q，期望带上服务端原因", llmError.Message)
	}
	if llmError.StatusCode != 0 {
		t.Fatalf("状态码 = %d，流中途失败时没有状态码", llmError.StatusCode)
	}
}
