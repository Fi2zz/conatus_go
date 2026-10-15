package llm_test

import (
	"context"
	"errors"
	"iter"
	"reflect"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
	"github.com/Fi2zz/conatus_go/llm"
)

// scriptedProvider 是脚本化替身：ChatStream 按脚本产出事件，Chat 记账调用次数。
type scriptedProvider struct {
	name        string
	events      []llm.StreamEvent
	streamErr   error
	chatResult  llm.Result
	chatErr     error
	chatCalls   int
	streamCalls int
	closed      bool
}

func (p *scriptedProvider) Name() string { return p.name }

func (p *scriptedProvider) Chat(
	context.Context, []llm.Message, ...llm.Option,
) (llm.Result, error) {
	p.chatCalls++
	return p.chatResult, p.chatErr
}

func (p *scriptedProvider) ChatStream(
	_ context.Context, _ []llm.Message, _ ...llm.Option,
) iter.Seq2[llm.StreamEvent, error] {
	p.streamCalls++
	return func(yield func(llm.StreamEvent, error) bool) {
		for _, event := range p.events {
			if !yield(event, nil) {
				return
			}
		}
		if p.streamErr != nil {
			yield(nil, p.streamErr)
		}
	}
}

func (p *scriptedProvider) Close() error {
	p.closed = true
	return nil
}

// chatOnlyProvider 只实现 Chat（ChatStream 是空迭代器），用来钉「没有事件就回退」。
type chatOnlyProvider struct {
	name       string
	chatResult llm.Result
	chatCalls  int
}

func (p *chatOnlyProvider) Name() string { return p.name }

func (p *chatOnlyProvider) Chat(
	context.Context, []llm.Message, ...llm.Option,
) (llm.Result, error) {
	p.chatCalls++
	return p.chatResult, nil
}

func (p *chatOnlyProvider) ChatStream(
	context.Context, []llm.Message, ...llm.Option,
) iter.Seq2[llm.StreamEvent, error] {
	return func(func(llm.StreamEvent, error) bool) {}
}

func TestStreamChatResultAccumulatesDeltas(t *testing.T) {
	provider := &scriptedProvider{
		name: "doubao",
		events: []llm.StreamEvent{
			llm.TextDelta{Text: "你"},
			llm.ReasoningDelta{Text: "想一"},
			llm.TextDelta{Text: "好"},
			llm.ReasoningDelta{Text: "想"},
			llm.StreamDone{
				Usage:        map[string]any{"total_tokens": 12},
				FinishReason: "stop",
				ToolCalls:    []llm.ToolCall{{ID: "call_1", Name: "get_time", Arguments: "{}"}},
				Provider:     "actual-provider",
				Model:        "doubao-seed",
			},
		},
	}

	result, err := llm.StreamChatResult(
		context.Background(), provider, []llm.Message{{Role: llm.RoleUser, Content: "你好"}})

	if err != nil {
		t.Fatalf("累积失败：%v", err)
	}
	if result.Content != "你好" || result.Reasoning != "想一想" {
		t.Fatalf("累积正文/思考异常：%+v", result)
	}
	if result.Model != "doubao-seed" {
		t.Fatalf("模型名 = %q，期望 doubao-seed", result.Model)
	}
	if result.Usage["total_tokens"] != 12 {
		t.Fatalf("用量未累积：%v", result.Usage)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "get_time" {
		t.Fatalf("工具调用未累积：%v", result.ToolCalls)
	}
	// Result.Provider 取 provider.Name()（与 Dart 一致）；StreamDone.Provider 记录实际
	// 提供商，只作诊断，不覆盖前者。
	if result.Provider != "doubao" {
		t.Fatalf("提供商 = %q，期望 doubao", result.Provider)
	}
	if provider.streamCalls != 1 || provider.chatCalls != 0 {
		t.Fatalf("调用次数 = (stream %d, chat %d)，期望 (1, 0)", provider.streamCalls, provider.chatCalls)
	}
}

func TestStreamChatResultFallsBackWhenNoEventArrives(t *testing.T) {
	provider := &chatOnlyProvider{
		name:       "scripted",
		chatResult: llm.Result{Content: "非流式答案", Provider: "scripted", Model: "m"},
	}

	result, err := llm.StreamChatResult(context.Background(), provider, nil)

	if err != nil {
		t.Fatalf("回退失败：%v", err)
	}
	if result.Content != "非流式答案" {
		t.Fatalf("结果 = %+v，期望走非流式回退", result)
	}
	if provider.chatCalls != 1 {
		t.Fatalf("非流式调用次数 = %d，期望 1", provider.chatCalls)
	}
}

func TestStreamChatResultPropagatesStreamErrorWithoutFallback(t *testing.T) {
	boom := errors.New("流中断")
	provider := &scriptedProvider{
		name:      "doubao",
		events:    []llm.StreamEvent{llm.TextDelta{Text: "半"}},
		streamErr: boom,
	}

	_, err := llm.StreamChatResult(context.Background(), provider, nil)

	if !errors.Is(err, boom) {
		t.Fatalf("错误 = %v，期望原样返回流错误", err)
	}
	if provider.chatCalls != 0 {
		t.Fatal("流出错后不该回退非流式（那会重复请求且掩盖失败）")
	}
}

func TestStreamChatResultNotifiesObserverInOrder(t *testing.T) {
	provider := &scriptedProvider{
		name:   "doubao",
		events: []llm.StreamEvent{llm.TextDelta{Text: "a"}, llm.StreamDone{Model: "m"}},
	}
	seen := []string{}

	_, err := llm.StreamChatResult(context.Background(), provider, nil,
		llm.WithObserver(func(event llm.StreamEvent) {
			switch typed := event.(type) {
			case llm.TextDelta:
				seen = append(seen, "text:"+typed.Text)
			case llm.StreamDone:
				seen = append(seen, "done")
			}
		}))

	if err != nil {
		t.Fatalf("累积失败：%v", err)
	}
	if want := []string{"text:a", "done"}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("透传事件 = %v，期望 %v", seen, want)
	}
}

func TestCloseOnlyActsOnProvidersImplementingCloser(t *testing.T) {
	withCloser := &scriptedProvider{name: "a"}
	if err := llm.Close(withCloser); err != nil {
		t.Fatalf("关闭失败：%v", err)
	}
	if !withCloser.closed {
		t.Fatal("实现了 Closer 的提供商应被关闭")
	}

	if err := llm.Close(&chatOnlyProvider{name: "b"}); err != nil {
		t.Fatalf("未实现 Closer 时不该报错：%v", err)
	}
}

func TestProvideRegistersAndClosesOnDispose(t *testing.T) {
	ctx := core.NewRoot("root")
	provider := &scriptedProvider{name: "doubao"}

	provided, err := llm.Provide(ctx, provider)
	if err != nil {
		t.Fatalf("provide 失败：%v", err)
	}
	if provided != llm.Provider(provider) {
		t.Fatal("Provide 应返回传入的提供商实例")
	}
	if !ctx.Has(llm.ServiceKey) {
		t.Fatal("服务应已注册")
	}
	from, err := llm.From(ctx)
	if err != nil || from != llm.Provider(provider) {
		t.Fatalf("From = (%v, %v)，期望同一实例", from, err)
	}

	ctx.Dispose()

	if !provider.closed {
		t.Fatal("上下文释放应关闭提供商")
	}
}

func TestProvideRejectsNilProviderAndFromRequiresService(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)

	if _, err := llm.Provide(ctx, nil); err == nil {
		t.Fatal("nil 提供商应返回错误")
	}
	if _, err := llm.From(ctx); err == nil {
		t.Fatal("未提供时 From 应返回错误")
	}
}
