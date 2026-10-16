package agent_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Fi2zz/conatus_go/agent"
	"github.com/Fi2zz/conatus_go/foundation/session"
	"github.com/Fi2zz/conatus_go/llm"
)

func TestRunRepliesWithoutTools(t *testing.T) {
	provider := &scriptedProvider{replies: []llm.Result{textResult(" \n你好\t ")}}
	target := newSession(t, "s1")
	loop := agent.NewLoop(provider, newEchoRegistry(t, &[]map[string]any{}), agent.WithSession(target))

	turn, err := loop.Run(context.Background(), "在吗", nil)

	if err != nil {
		t.Fatalf("跑一轮失败：%v", err)
	}
	if turn.Reply != "你好" {
		t.Fatalf("回复 = %q，期望首尾空白被去掉", turn.Reply)
	}
	if len(turn.Steps) != 0 || len(turn.Usage) != 1 {
		t.Fatalf("步骤 %d 条、用量 %d 条，期望 0 / 1", len(turn.Steps), len(turn.Usage))
	}
	if len(turn.Messages) != 3 {
		t.Fatalf("消息 = %d 条，期望 system + user + assistant", len(turn.Messages))
	}
	if turn.Messages[0].Role != llm.RoleSystem || !turn.Messages[0].Cacheable {
		t.Fatalf("首条消息 = %+v，期望可缓存的 system", turn.Messages[0])
	}
	if want := []string{session.EventUserMessage, session.EventAssistantMessage}; !slices.Equal(eventTypes(target), want) {
		t.Fatalf("会话事件 = %v，期望 %v", eventTypes(target), want)
	}
	if data := eventData(t, target.Events()[0]); data["text"] != "在吗" {
		t.Fatalf("用户事件负载 = %v", data)
	}
}

func TestRunExecutesToolCallsAndCloses(t *testing.T) {
	arguments := []map[string]any{}
	registry := newEchoRegistry(t, &arguments)
	provider := &scriptedProvider{replies: []llm.Result{
		toolCallResult(toolCall("c1", "echo", `{"text":"嗨"}`)),
		textResult("收到：echo:嗨"),
	}}
	target := newSession(t, "s1")
	loop := agent.NewLoop(provider, registry, agent.WithSession(target))

	turn, err := loop.Run(context.Background(), "试试工具", nil)

	if err != nil {
		t.Fatalf("跑一轮失败：%v", err)
	}
	if turn.Reply != "收到：echo:嗨" || provider.calls != 2 {
		t.Fatalf("回复 = %q、模型调用 %d 次", turn.Reply, provider.calls)
	}
	if len(turn.Steps) != 1 || turn.Steps[0].Result.Content != "echo:嗨" {
		t.Fatalf("步骤 = %+v", turn.Steps)
	}
	if len(arguments) != 1 || arguments[0]["text"] != "嗨" {
		t.Fatalf("工具收到的参数 = %v，期望解析后的 {\"text\":\"嗨\"}", arguments)
	}
	if len(turn.Messages) != 5 {
		t.Fatalf("消息 = %d 条，期望 system / user / assistant / tool / assistant", len(turn.Messages))
	}
	if len(turn.Messages[2].ToolCalls) != 1 || turn.Messages[3].ToolCallID != "c1" {
		t.Fatalf("回填的消息 = %+v / %+v", turn.Messages[2], turn.Messages[3])
	}
	// 第二次请求模型时，工具结果必须在上下文里——否则模型没法据此收口。
	second := provider.seen[1]
	if second[len(second)-1].Role != llm.RoleTool {
		t.Fatalf("第二次请求的最后一条 = %+v，期望工具结果", second[len(second)-1])
	}
	want := []string{
		session.EventUserMessage, session.EventAssistantMessage,
		session.EventToolResult, session.EventAssistantMessage,
	}
	if !slices.Equal(eventTypes(target), want) {
		t.Fatalf("会话事件 = %v，期望 %v", eventTypes(target), want)
	}
}

func TestRunExecutesParallelCallsInOrder(t *testing.T) {
	arguments := []map[string]any{}
	registry := newEchoRegistry(t, &arguments)
	provider := &scriptedProvider{replies: []llm.Result{
		toolCallResult(
			toolCall("c1", "echo", `{"text":"甲"}`),
			toolCall("c2", "echo", `{"text":"乙"}`),
		),
		textResult("都做完了"),
	}}
	loop := agent.NewLoop(provider, registry)

	turn, err := loop.Run(context.Background(), "两个一起", nil)

	if err != nil {
		t.Fatalf("跑一轮失败：%v", err)
	}
	if len(turn.Steps) != 2 {
		t.Fatalf("步骤 = %d 条，期望 2", len(turn.Steps))
	}
	if turn.Steps[0].Call.ID != "c1" || turn.Steps[1].Call.ID != "c2" {
		t.Fatalf("步骤顺序 = %s / %s，期望按模型给的顺序", turn.Steps[0].Call.ID, turn.Steps[1].Call.ID)
	}
	if turn.Steps[0].Result.Content != "echo:甲" || turn.Steps[1].Result.Content != "echo:乙" {
		t.Fatalf("步骤结果 = %+v", turn.Steps)
	}
}

func TestRunStopsAtMaxSteps(t *testing.T) {
	arguments := []map[string]any{}
	provider := &scriptedProvider{replies: []llm.Result{
		toolCallResult(toolCall("c1", "echo", `{"text":"再"}`)),
	}}
	loop := agent.NewLoop(provider, newEchoRegistry(t, &arguments), agent.WithMaxSteps(2))

	turn, err := loop.Run(context.Background(), "停不下来", nil)

	if err != nil {
		t.Fatalf("跑一轮失败：%v", err)
	}
	if turn.Reply != "（已达到最大步数 2，未收口）" {
		t.Fatalf("回复 = %q", turn.Reply)
	}
	if provider.calls != 2 {
		t.Fatalf("模型调用 %d 次，期望被步数上限拦住", provider.calls)
	}
}

func TestNewLoopPanicsOnNonPositiveMaxSteps(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("maxSteps ≤ 0 属编程错误，应当 panic")
		}
	}()

	agent.NewLoop(&scriptedProvider{replies: []llm.Result{textResult("x")}},
		newEchoRegistry(t, &[]map[string]any{}), agent.WithMaxSteps(0))
}

func TestRunWorksWithoutSession(t *testing.T) {
	provider := &scriptedProvider{replies: []llm.Result{textResult("临时对话")}}
	loop := agent.NewLoop(provider, newEchoRegistry(t, &[]map[string]any{}))

	turn, err := loop.Run(context.Background(), "在吗", nil)

	if err != nil {
		t.Fatalf("无会话时也要能跑：%v", err)
	}
	if turn.Reply != "临时对话" {
		t.Fatalf("回复 = %q", turn.Reply)
	}
}

func TestRunAbortsOnClosedSession(t *testing.T) {
	provider := &scriptedProvider{replies: []llm.Result{textResult("不该到这里")}}
	target := newSession(t, "s1")
	target.Close()
	loop := agent.NewLoop(provider, newEchoRegistry(t, &[]map[string]any{}), agent.WithSession(target))

	_, err := loop.Run(context.Background(), "在吗", nil)

	if !errors.Is(err, agent.ErrSessionClosed) {
		t.Fatalf("错误 = %v，期望会话已关闭", err)
	}
	if provider.calls != 0 {
		t.Fatal("会话都关了，不该再花一次模型配额")
	}
}

func TestRunReturnsErrCancelledWhenContextDone(t *testing.T) {
	provider := &scriptedProvider{replies: []llm.Result{textResult("不该到这里")}}
	target := newSession(t, "s1")
	loop := agent.NewLoop(provider, newEchoRegistry(t, &[]map[string]any{}), agent.WithSession(target))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := loop.Run(ctx, "在吗", nil)

	if !errors.Is(err, agent.ErrCancelled) {
		t.Fatalf("错误 = %v，期望轮次已取消", err)
	}
	if want := []string{session.EventUserMessage}; !slices.Equal(eventTypes(target), want) {
		t.Fatalf("取消后不该写助手事件：%v", eventTypes(target))
	}
}

func TestRunInjectsSteeringBetweenSteps(t *testing.T) {
	arguments := []map[string]any{}
	pending := []string{"插一条"}
	source := func(context.Context) ([]string, error) {
		taken := pending
		pending = nil
		return taken, nil
	}
	provider := &scriptedProvider{replies: []llm.Result{
		toolCallResult(toolCall("c1", "echo", `{"text":"甲"}`)),
		textResult("收到插话"),
	}}
	target := newSession(t, "s1")
	loop := agent.NewLoop(provider, newEchoRegistry(t, &arguments),
		agent.WithSession(target), agent.WithSteering(source))

	turn, err := loop.Run(context.Background(), "先干活", nil)

	if err != nil {
		t.Fatalf("跑一轮失败：%v", err)
	}
	if turn.Reply != "收到插话" {
		t.Fatalf("回复 = %q", turn.Reply)
	}
	// 插话在「组下一次请求之前」注入：它没有排队等待，所以随第一步就发出去了。
	first := provider.seen[0]
	if first[len(first)-1].Content != "插一条" {
		t.Fatalf("第一次请求的最后一条 = %+v，期望插话", first[len(first)-1])
	}
	if data := eventData(t, target.Events()[1]); data["text"] != "插一条" {
		t.Fatalf("插话事件 = %v", data)
	}
}

func TestRunNotifiesTurnTracker(t *testing.T) {
	tracker := &recordingTracker{}
	provider := &scriptedProvider{replies: []llm.Result{textResult("答案")}}
	loop := agent.NewLoop(provider, newEchoRegistry(t, &[]map[string]any{}),
		agent.WithTurnTracker(tracker))

	if _, err := loop.Run(context.Background(), "问题", nil); err != nil {
		t.Fatalf("跑一轮失败：%v", err)
	}

	if !slices.Equal(tracker.begun, []string{"问题"}) {
		t.Fatalf("begin 收到 %v", tracker.begun)
	}
	if !slices.Equal(tracker.ended, []string{"答案"}) {
		t.Fatalf("end 收到 %v，期望最终回复", tracker.ended)
	}
}

func TestRunReportsTrackerFailureOnlyWhenRoundSucceeds(t *testing.T) {
	failing := &failingTracker{}
	provider := &scriptedProvider{replies: []llm.Result{textResult("答案")}}
	loop := agent.NewLoop(provider, newEchoRegistry(t, &[]map[string]any{}),
		agent.WithTurnTracker(failing))

	_, err := loop.Run(context.Background(), "问题", nil)

	if !errors.Is(err, errTracker) {
		t.Fatalf("错误 = %v，期望追踪器的错误浮上来", err)
	}
}

func TestRunObservesRoundsAndFinish(t *testing.T) {
	arguments := []map[string]any{}
	var kinds []string
	var payloads []map[string]any
	provider := &scriptedProvider{replies: []llm.Result{
		toolCallResult(toolCall("c1", "echo", `{"text":"甲"}`)),
		textResult("好了"),
	}}
	loop := agent.NewLoop(provider, newEchoRegistry(t, &arguments),
		agent.WithObserver(func(kind string, data map[string]any) {
			kinds = append(kinds, kind)
			payloads = append(payloads, data)
		}))

	if _, err := loop.Run(context.Background(), "干活", nil); err != nil {
		t.Fatalf("跑一轮失败：%v", err)
	}

	if want := []string{"agent.round", "agent.round", "agent.finished"}; !slices.Equal(kinds, want) {
		t.Fatalf("观察事件 = %v，期望 %v", kinds, want)
	}
	if payloads[0]["step"] != 0 || payloads[0]["toolCalls"] != 1 {
		t.Fatalf("首轮埋点 = %v", payloads[0])
	}
	if payloads[2]["replyLength"] != len("好了") || payloads[2]["steps"] != 1 {
		t.Fatalf("收口埋点 = %v", payloads[2])
	}
}

func TestRunForwardsStreamEventsWhenConfigured(t *testing.T) {
	var kinds []string
	provider := &scriptedProvider{replies: []llm.Result{textResult("流式答案")}}
	loop := agent.NewLoop(provider, newEchoRegistry(t, &[]map[string]any{}),
		agent.WithStreamObserver(func(event llm.StreamEvent) {
			switch event.(type) {
			case llm.TextDelta:
				kinds = append(kinds, "text")
			case llm.StreamDone:
				kinds = append(kinds, "done")
			}
		}))

	turn, err := loop.Run(context.Background(), "在吗", nil)

	if err != nil {
		t.Fatalf("跑一轮失败：%v", err)
	}
	if !slices.Equal(kinds, []string{"text", "done"}) {
		t.Fatalf("透传事件 = %v", kinds)
	}
	if !strings.Contains(turn.Reply, "流式答案") {
		t.Fatalf("回复 = %q", turn.Reply)
	}
}
