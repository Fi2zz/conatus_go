package agent_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/Fi2zz/conatus_go/agent"
	"github.com/Fi2zz/conatus_go/core"
	"github.com/Fi2zz/conatus_go/foundation/prompt"
	"github.com/Fi2zz/conatus_go/foundation/session"
	"github.com/Fi2zz/conatus_go/foundation/tools"
	"github.com/Fi2zz/conatus_go/llm"
)

// newPromptRegistry 建一个带一段人设与一份动态上下文的注册表。
func newPromptRegistry(t *testing.T) *prompt.Registry {
	t.Helper()
	registry := prompt.NewRegistry()
	_, err := registry.Section(prompt.Section{
		Name: "人设", Order: 1, Text: func() string { return "你是测试助手。" },
	})
	if err != nil {
		t.Fatalf("注册 prompt 段失败：%v", err)
	}
	_, err = registry.Context(prompt.Context{
		Name: "环境", Order: 1, Text: func() string { return "今天很热" },
	})
	if err != nil {
		t.Fatalf("注册动态上下文失败：%v", err)
	}
	return registry
}

func TestDeriveMessagesRestoresThreeKindsOfMessages(t *testing.T) {
	target := newSession(t, "s1")
	appends := []struct {
		eventType string
		data      map[string]any
	}{
		{session.EventUserMessage, map[string]any{
			"text":   "看这张图",
			"images": []any{map[string]any{"mimeType": "image/png", "base64Data": "aGk="}},
		}},
		{session.EventAssistantMessage, map[string]any{
			"text":      "我调用工具",
			"toolCalls": []any{map[string]any{"id": "c1", "name": "echo", "arguments": `{"text":"嗨"}`}},
		}},
		{session.EventToolResult, map[string]any{
			"callId": "c1", "name": "echo", "content": "echo:嗨", "isError": false,
		}},
		{"llm/request", map[string]any{"note": "派生事件不进模型上下文"}},
	}
	for _, item := range appends {
		if _, err := target.Append(item.eventType, session.WithEventData(item.data)); err != nil {
			t.Fatalf("写事件失败：%v", err)
		}
	}

	messages := agent.DeriveMessages(target.Events())

	if len(messages) != 3 {
		t.Fatalf("消息 = %d 条，期望 3（派生事件被忽略）", len(messages))
	}
	if messages[0].Role != llm.RoleUser || messages[0].Content != "看这张图" || len(messages[0].Images) != 1 {
		t.Fatalf("用户消息 = %+v", messages[0])
	}
	if messages[1].Role != llm.RoleAssistant || len(messages[1].ToolCalls) != 1 {
		t.Fatalf("助手消息 = %+v", messages[1])
	}
	if messages[1].ToolCalls[0].Name != "echo" || messages[1].ToolCalls[0].Arguments != `{"text":"嗨"}` {
		t.Fatalf("工具调用 = %+v", messages[1].ToolCalls[0])
	}
	if messages[2].Role != llm.RoleTool || messages[2].ToolCallID != "c1" || messages[2].Content != "echo:嗨" {
		t.Fatalf("工具结果消息 = %+v", messages[2])
	}
}

func TestDeriveMessagesSkipsMalformedPayloads(t *testing.T) {
	events := []session.Event{
		{Type: session.EventUserMessage, Data: "不是对象"},
		{Type: session.EventUserMessage},
		{Type: session.EventUserMessage, Data: map[string]any{"text": 42}},
	}

	messages := agent.DeriveMessages(events)

	if len(messages) != 1 {
		t.Fatalf("消息 = %d 条，期望只留下唯一成形的那条", len(messages))
	}
	if messages[0].Content != "42" {
		t.Fatalf("负载 = %+v，期望数字被按字面形式兜底", messages[0])
	}
}

// 事件负载要能落盘再读回来：序列化用的形态必须和还原时认的形态一致（都是 []any）。
func TestImagesAndToolCallsRoundTripThroughJSON(t *testing.T) {
	images := []llm.Image{{MIMEType: "image/png", Base64Data: "aGk="}}
	calls := []llm.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"a":1}`}}

	rawImages, err := json.Marshal(agent.ImagesToJSON(images))
	if err != nil {
		t.Fatalf("序列化图片失败：%v", err)
	}
	rawCalls, err := json.Marshal(agent.ToolCallsToJSON(calls))
	if err != nil {
		t.Fatalf("序列化工具调用失败：%v", err)
	}
	var backImages, backCalls any
	if err := json.Unmarshal(rawImages, &backImages); err != nil {
		t.Fatalf("读回图片失败：%v", err)
	}
	if err := json.Unmarshal(rawCalls, &backCalls); err != nil {
		t.Fatalf("读回工具调用失败：%v", err)
	}

	if got := agent.ImagesFromJSON(backImages); len(got) != 1 || got[0] != images[0] {
		t.Fatalf("图片往返 = %+v", got)
	}
	if got := agent.ToolCallsFromJSON(backCalls); len(got) != 1 || got[0] != calls[0] {
		t.Fatalf("工具调用往返 = %+v", got)
	}
	// 缺字段的条目跳过：日志可能被手改过。
	if got := agent.ImagesFromJSON([]any{map[string]any{"mimeType": "image/png"}}); len(got) != 0 {
		t.Fatalf("缺 base64Data 的图片应被跳过，得到 %+v", got)
	}
	if got := agent.ToolCallsFromJSON([]any{map[string]any{"id": "c1"}}); len(got) != 0 {
		t.Fatalf("缺 name 的调用应被跳过，得到 %+v", got)
	}
	if got := agent.ImagesFromJSON("不是数组"); len(got) != 0 {
		t.Fatalf("非数组负载应返回空，得到 %+v", got)
	}
}

func TestParseToolArgumentsToleratesBrokenInput(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want map[string]any
	}{
		{"合法对象", `{"text":"嗨","count":2}`, map[string]any{"text": "嗨", "count": float64(2)}},
		{"空串", "", map[string]any{}},
		{"非法 JSON", `{"text":`, map[string]any{}},
		{"不是对象", `[1,2]`, map[string]any{}},
		{"null", `null`, map[string]any{}},
	}
	for _, item := range cases {
		got := agent.ParseToolArguments(item.raw)
		if len(got) != len(item.want) {
			t.Fatalf("%s：参数 = %v，期望 %v", item.name, got, item.want)
		}
		for key, value := range item.want {
			if got[key] != value {
				t.Fatalf("%s：参数[%q] = %v，期望 %v", item.name, key, got[key], value)
			}
		}
	}
}

func TestBuildSystemText(t *testing.T) {
	if got := agent.BuildSystemText("兜底人设", nil); got != "兜底人设" {
		t.Fatalf("无注册表时 = %q，期望兜底人设", got)
	}

	got := agent.BuildSystemText("兜底人设", newPromptRegistry(t))

	if got != "你是测试助手。\n\n今天很热" {
		t.Fatalf("有注册表时 = %q，期望段 + 空行 + 动态上下文", got)
	}
}

func TestProvideResolvesDependenciesFromContext(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	target := newSession(t, "s1")
	provider := &scriptedProvider{replies: []llm.Result{textResult("答")}}
	if _, err := llm.Provide(ctx, provider); err != nil {
		t.Fatalf("provide llm 失败：%v", err)
	}
	if _, err := tools.Provide(ctx); err != nil {
		t.Fatalf("provide tools 失败：%v", err)
	}
	if _, err := prompt.Provide(ctx, prompt.WithRegistry(newPromptRegistry(t))); err != nil {
		t.Fatalf("provide prompt 失败：%v", err)
	}

	loop, err := agent.Provide(ctx, agent.WithSession(target))
	if err != nil {
		t.Fatalf("provide agent 失败：%v", err)
	}
	from, err := agent.From(ctx)
	if err != nil || from != loop {
		t.Fatalf("From = (%v, %v)，期望同一实例", from, err)
	}

	if _, err := loop.Run(context.Background(), "在吗", nil); err != nil {
		t.Fatalf("跑一轮失败：%v", err)
	}
	// prompt 注册表是自动接入的：system 消息里应当能看到段与动态上下文。
	system := provider.seen[0][0].Content
	if system != "你是测试助手。\n\n今天很热" {
		t.Fatalf("system = %q，期望自动接入 prompt 注册表", system)
	}
}

func TestProvideRequiresLlmAndTools(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)

	if _, err := agent.Provide(ctx); err == nil {
		t.Fatal("缺 llm 服务时应报错")
	}
	if _, err := agent.From(ctx); err == nil {
		t.Fatal("未提供时 From 应报错")
	}

	if _, err := llm.Provide(ctx, &scriptedProvider{replies: []llm.Result{textResult("x")}}); err != nil {
		t.Fatalf("provide llm 失败：%v", err)
	}
	if _, err := agent.Provide(ctx); err == nil {
		t.Fatal("缺 tools 服务时应报错")
	}
}

func TestEnsureSessionOpenAndRecentEvents(t *testing.T) {
	if err := agent.EnsureSessionOpen(nil); err != nil {
		t.Fatalf("没有会话时不该报错：%v", err)
	}
	if got := agent.RecentEvents(nil); got != nil {
		t.Fatalf("没有会话时不该有历史：%v", got)
	}

	target := newSession(t, "s1")
	if _, err := target.Append(session.EventUserMessage, session.WithEventData(map[string]any{"text": "x"})); err != nil {
		t.Fatalf("写事件失败：%v", err)
	}
	if got := agent.RecentEvents(target); len(got) != 1 {
		t.Fatalf("历史 = %d 条，期望 1", len(got))
	}
	target.Close()
	if err := agent.EnsureSessionOpen(target); err == nil {
		t.Fatal("会话关闭后应报错")
	}
}

// 事件日志是唯一的真相：第二轮要能把第一轮的事件还原成上下文。
func TestSecondRunSeesPreviousEvents(t *testing.T) {
	provider := &scriptedProvider{replies: []llm.Result{textResult("第一答"), textResult("第二答")}}
	target := newSession(t, "s1")
	loop := agent.NewLoop(provider, newEchoRegistry(t, &[]map[string]any{}), agent.WithSession(target))

	if _, err := loop.Run(context.Background(), "第一问", nil); err != nil {
		t.Fatalf("第一轮失败：%v", err)
	}
	if _, err := loop.Run(context.Background(), "第二问", nil); err != nil {
		t.Fatalf("第二轮失败：%v", err)
	}

	second := provider.seen[1]
	contents := make([]string, 0, len(second))
	for _, message := range second {
		contents = append(contents, message.Content)
	}
	for _, text := range []string{"第一问", "第一答", "第二问"} {
		if !slices.Contains(contents, text) {
			t.Fatalf("第二轮上下文 = %v，期望带上第一轮的历史（%q）", contents, text)
		}
	}
	// 本轮输入只该出现一次：历史是在写用户事件之前抄的，不许重复派生。
	appearances := 0
	for _, content := range contents {
		if content == "第二问" {
			appearances++
		}
	}
	if appearances != 1 {
		t.Fatalf("本轮输入出现 %d 次，期望 1", appearances)
	}
}
