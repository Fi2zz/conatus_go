package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Fi2zz/conatus_go/agent"
	"github.com/Fi2zz/conatus_go/core"
	"github.com/Fi2zz/conatus_go/foundation/prompt"
	"github.com/Fi2zz/conatus_go/foundation/session"
	"github.com/Fi2zz/conatus_go/foundation/tools"
	"github.com/Fi2zz/conatus_go/llm"
)

func TestRunUsesCustomDefaultSystemPrompt(t *testing.T) {
	provider := &scriptedProvider{replies: []llm.Result{textResult("好")}}
	loop := agent.NewLoop(provider, newEchoRegistry(t, &[]map[string]any{}),
		agent.WithDefaultSystemPrompt("只说重点。"))

	if _, err := loop.Run(context.Background(), "在吗", nil); err != nil {
		t.Fatalf("跑一轮失败：%v", err)
	}

	if got := provider.seen[0][0].Content; got != "只说重点。" {
		t.Fatalf("system = %q，期望自定义兜底人设", got)
	}
}

// 动态上下文为空时不该留下孤立的空行（空段拼完就没有下文了）。
func TestBuildSystemTextSkipsEmptyContexts(t *testing.T) {
	registry := prompt.NewRegistry()
	if _, err := registry.Section(prompt.Section{
		Name: "人设", Order: 1, Text: func() string { return "只有段" },
	}); err != nil {
		t.Fatalf("注册 prompt 段失败：%v", err)
	}

	if got := agent.BuildSystemText("兜底", registry); got != "只有段" {
		t.Fatalf("system = %q，期望没有多余空行", got)
	}
}

func TestProvideWithoutPromptRegistryFallsBackToDefault(t *testing.T) {
	provider := &scriptedProvider{replies: []llm.Result{textResult("答")}}
	loop := provideLoopWithoutPrompt(t, provider)

	if _, err := loop.Run(context.Background(), "在吗", nil); err != nil {
		t.Fatalf("跑一轮失败：%v", err)
	}

	if got := provider.seen[0][0].Content; got != agent.DefaultSystemPrompt {
		t.Fatalf("system = %q，期望兜底人设", got)
	}
}

func TestRunCarriesImagesIntoEventAndRequest(t *testing.T) {
	provider := &scriptedProvider{replies: []llm.Result{textResult("看到了")}}
	target := newSession(t, "s1")
	loop := agent.NewLoop(provider, newEchoRegistry(t, &[]map[string]any{}), agent.WithSession(target))
	images := []llm.Image{{MIMEType: "image/png", Base64Data: "aGk="}}

	if _, err := loop.Run(context.Background(), "看这张图", images); err != nil {
		t.Fatalf("跑一轮失败：%v", err)
	}

	data := eventData(t, target.Events()[0])
	if _, found := data["images"]; !found {
		t.Fatalf("用户事件应带上图片：%v", data)
	}
	seen := provider.seen[0]
	last := seen[len(seen)-1]
	if len(last.Images) != 1 || last.Images[0] != images[0] {
		t.Fatalf("模型收到的图片 = %+v", last.Images)
	}
}

// 事件里缺 arguments 的工具调用要退化成空对象，别把 nil 传给工具参数校验。
func TestDeriveMessagesDefaultsMissingArguments(t *testing.T) {
	events := []session.Event{{
		Type: session.EventAssistantMessage,
		Data: map[string]any{
			"text":      "",
			"toolCalls": []any{map[string]any{"id": "c1", "name": "echo"}},
		},
	}}

	messages := agent.DeriveMessages(events)

	if len(messages) != 1 || len(messages[0].ToolCalls) != 1 {
		t.Fatalf("消息 = %+v", messages)
	}
	if messages[0].ToolCalls[0].Arguments != "{}" {
		t.Fatalf("参数 = %q，期望退化成空对象", messages[0].ToolCalls[0].Arguments)
	}
}

// 缺字段的文本按「没来」处理，别把整条消息丢掉。
func TestDeriveMessagesTreatsMissingTextAsEmpty(t *testing.T) {
	messages := agent.DeriveMessages([]session.Event{
		{Type: session.EventUserMessage, Data: map[string]any{}},
	})

	if len(messages) != 1 || messages[0].Content != "" || messages[0].Role != llm.RoleUser {
		t.Fatalf("消息 = %+v，期望留下一条空正文的用户消息", messages)
	}
}

func TestRunPropagatesModelFailure(t *testing.T) {
	boom := errors.New("上游 500")
	provider := &scriptedProvider{replies: []llm.Result{textResult("x")}, err: boom}
	loop := agent.NewLoop(provider, newEchoRegistry(t, &[]map[string]any{}))

	_, err := loop.Run(context.Background(), "在吗", nil)

	if !errors.Is(err, boom) {
		t.Fatalf("错误 = %v，期望原样上抛（不是取消）", err)
	}
}

func TestRunPropagatesSteeringFailure(t *testing.T) {
	boom := errors.New("插话队列坏了")
	provider := &scriptedProvider{replies: []llm.Result{textResult("x")}}
	loop := agent.NewLoop(provider, newEchoRegistry(t, &[]map[string]any{}),
		agent.WithSteering(func(context.Context) ([]string, error) { return nil, boom }))

	_, err := loop.Run(context.Background(), "在吗", nil)

	if !errors.Is(err, boom) {
		t.Fatalf("错误 = %v，期望插话来源的失败向上传播", err)
	}
	if provider.calls != 0 {
		t.Fatal("插话都取不到，不该再花一次模型配额")
	}
}

func TestRunAbortsWhenSessionClosedMidRound(t *testing.T) {
	arguments := []map[string]any{}
	target := newSession(t, "s1")
	// 工具执行期间把会话关掉：下一次组请求前的检查必须立刻收手。
	provider := &scriptedProvider{replies: []llm.Result{
		toolCallResult(toolCall("c1", "echo", `{"text":"甲"}`)),
		textResult("不该到这里"),
	}}
	loop := agent.NewLoop(provider, newEchoRegistry(t, &arguments, target.Close),
		agent.WithSession(target))

	_, err := loop.Run(context.Background(), "干活", nil)

	if !errors.Is(err, agent.ErrSessionClosed) {
		t.Fatalf("错误 = %v，期望会话已关闭", err)
	}
	if provider.calls != 1 {
		t.Fatalf("模型调用 %d 次，会话关了就不要再试", provider.calls)
	}
}

func TestRunReturnsErrCancelledWhenCancelledDuringTool(t *testing.T) {
	arguments := []map[string]any{}
	ctx, cancel := context.WithCancel(context.Background())
	provider := &scriptedProvider{replies: []llm.Result{
		toolCallResult(toolCall("c1", "echo", `{"text":"甲"}`)),
		textResult("不该到这里"),
	}}
	loop := agent.NewLoop(provider, newEchoRegistry(t, &arguments, cancel))

	turn, err := loop.Run(ctx, "干活", nil)

	if !errors.Is(err, agent.ErrCancelled) {
		t.Fatalf("错误 = %v，期望轮次已取消", err)
	}
	if len(turn.Steps) != 0 {
		t.Fatalf("取消后工具结果应当丢弃，得到 %+v", turn.Steps)
	}
}

func TestRunAbortsWhenSessionClosedByModelCall(t *testing.T) {
	target := newSession(t, "s1")
	provider := &scriptedProvider{
		replies: []llm.Result{toolCallResult(toolCall("c1", "echo", `{"text":"甲"}`))},
		onCall:  target.Close,
	}
	loop := agent.NewLoop(provider, newEchoRegistry(t, &[]map[string]any{}), agent.WithSession(target))

	_, err := loop.Run(context.Background(), "干活", nil)

	if !errors.Is(err, agent.ErrSessionClosed) {
		t.Fatalf("错误 = %v，期望写事件时发现会话已关", err)
	}
}

func TestRunPropagatesTrackerBeginFailure(t *testing.T) {
	provider := &scriptedProvider{replies: []llm.Result{textResult("x")}}
	loop := agent.NewLoop(provider, newEchoRegistry(t, &[]map[string]any{}),
		agent.WithTurnTracker(beginFailingTracker{}))

	_, err := loop.Run(context.Background(), "在吗", nil)

	if !errors.Is(err, errTracker) {
		t.Fatalf("错误 = %v，期望追踪器的失败直接返回", err)
	}
	if provider.calls != 0 {
		t.Fatal("begin 就失败时不该开跑")
	}
}

func TestRunKeepsRoundFailureWhenTrackerAlsoFails(t *testing.T) {
	provider := &scriptedProvider{replies: []llm.Result{textResult("x")}}
	loop := agent.NewLoop(provider, newEchoRegistry(t, &[]map[string]any{}),
		agent.WithTurnTracker(failingTracker{}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := loop.Run(ctx, "在吗", nil)

	if !errors.Is(err, agent.ErrCancelled) {
		t.Fatalf("错误 = %v，期望轮次本身的失败压过追踪器的失败", err)
	}
}

// 收口时才发现会话被关（模型这一轮没有工具调用）：仍然报「会话已关闭」。
func TestRunAbortsWhenSessionClosedBeforeFinish(t *testing.T) {
	target := newSession(t, "s1")
	provider := &scriptedProvider{replies: []llm.Result{textResult("答")}, onCall: target.Close}
	loop := agent.NewLoop(provider, newEchoRegistry(t, &[]map[string]any{}), agent.WithSession(target))

	_, err := loop.Run(context.Background(), "在吗", nil)

	if !errors.Is(err, agent.ErrSessionClosed) {
		t.Fatalf("错误 = %v，期望收口写事件时发现会话已关", err)
	}
}

// 同一个上下文里重复装配要报错（服务键已被占用），不能悄悄换掉在用的循环。
func TestProvideRejectsDuplicateRegistration(t *testing.T) {
	provider := &scriptedProvider{replies: []llm.Result{textResult("答")}}
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	if _, err := llm.Provide(ctx, provider); err != nil {
		t.Fatalf("provide llm 失败：%v", err)
	}
	if _, err := tools.Provide(ctx); err != nil {
		t.Fatalf("provide tools 失败：%v", err)
	}
	if _, err := agent.Provide(ctx); err != nil {
		t.Fatalf("首次 provide agent 失败：%v", err)
	}

	if _, err := agent.Provide(ctx); err == nil {
		t.Fatal("重复装配应当报错")
	}
}
