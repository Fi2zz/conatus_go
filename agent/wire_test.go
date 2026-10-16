package agent_test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/Fi2zz/conatus_go/agent"
	"github.com/Fi2zz/conatus_go/llm"
)

// 端到端接线：Agent 循环 + 真的 OpenAI 兼容 provider（假 transport，不打网络）。
//
// 验的是「装配真的到位」：工具 schema 进了请求体、流式端点被走到、收口回复来自 SSE 增量。
func TestLoopWiresToolSchemasAndStreamingToProvider(t *testing.T) {
	arguments := []map[string]any{}
	transport := &scriptedTransport{frames: []string{
		`{"choices":[{"delta":{"content":"嗯"},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"total_tokens":7}}`,
	}}
	provider := llm.NewOpenAI(
		llm.WithName("doubao"),
		llm.WithBaseURL("https://ark.example.test/api/v3"),
		llm.WithModel("doubao-test"),
		llm.WithAPIKey("test-key"),
		llm.WithHTTPClient(&http.Client{Transport: transport}),
	)
	var streamed []string
	loop := agent.NewLoop(provider, newEchoRegistry(t, &arguments),
		agent.WithStreamObserver(func(event llm.StreamEvent) {
			if delta, ok := event.(llm.TextDelta); ok {
				streamed = append(streamed, delta.Text)
			}
		}),
	)

	turn, err := loop.Run(context.Background(), "在吗", nil)

	if err != nil {
		t.Fatalf("跑一轮失败：%v", err)
	}
	if turn.Reply != "嗯" || !slices.Equal(streamed, []string{"嗯"}) {
		t.Fatalf("回复 = %q、透传增量 = %v", turn.Reply, streamed)
	}
	if len(turn.Usage) != 1 || turn.Usage[0]["total_tokens"] != float64(7) {
		t.Fatalf("用量 = %v，期望从终态帧累积", turn.Usage)
	}

	body := lastRequest(t, transport)
	if body["stream"] != true {
		t.Fatal("装了流式回调就必须走流式端点")
	}
	schemas, ok := body["tools"].([]any)
	if !ok || len(schemas) != 1 {
		t.Fatalf("请求体里的工具 schema = %v", body["tools"])
	}
	tool, ok := schemas[0].(map[string]any)
	if !ok || tool["type"] != "function" {
		t.Fatalf("工具 schema = %v", schemas[0])
	}
	function, ok := tool["function"].(map[string]any)
	if !ok || function["name"] != "echo" {
		t.Fatalf("工具 schema 缺 function 包裹或名字不对：%v", tool)
	}
	if _, leaked := function["riskLevel"]; leaked {
		t.Fatalf("宿主字段不该下发模型：%v", function)
	}

	messages, ok := body["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("请求体里的消息 = %v，期望 system + user", body["messages"])
	}
	first, _ := messages[0].(map[string]any)
	if first["role"] != "system" {
		t.Fatalf("首条消息 = %v，期望 system", first)
	}
}

// 工具调用也要能穿过真 provider：SSE 帧给出 tool_calls → 执行 → 回填。
func TestLoopRunsToolsThroughRealProvider(t *testing.T) {
	arguments := []map[string]any{}
	transport := &scriptedTransport{frames: []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"echo","arguments":"{\"text\":\"嗨\"}"}}]},"finish_reason":"tool_calls"}]}`,
	}}
	provider := llm.NewOpenAI(
		llm.WithName("doubao"),
		llm.WithBaseURL("https://ark.example.test/api/v3"),
		llm.WithModel("doubao-test"),
		llm.WithAPIKey("test-key"),
		llm.WithHTTPClient(&http.Client{Transport: transport}),
	)
	// 假 transport 每次都回同一帧（要求调工具），所以把步数压到 1，只看「执行 + 回填」这一步。
	loop := agent.NewLoop(provider, newEchoRegistry(t, &arguments), agent.WithMaxSteps(1))

	turn, err := loop.Run(context.Background(), "试试工具", nil)

	if err != nil {
		t.Fatalf("跑一轮失败：%v", err)
	}
	if turn.Reply != "（已达到最大步数 1，未收口）" {
		t.Fatalf("回复 = %q", turn.Reply)
	}
	if len(arguments) != 1 || arguments[0]["text"] != "嗨" {
		t.Fatalf("工具参数 = %v，期望从 SSE 分片里解析出来", arguments)
	}
	if len(turn.Steps) != 1 || turn.Steps[0].Result.Content != "echo:嗨" {
		t.Fatalf("步骤 = %+v", turn.Steps)
	}
	if turn.Steps[0].Call.ID != "c1" || turn.Steps[0].Call.Name != "echo" {
		t.Fatalf("调用 = %+v，期望 id 与 name 从分片累积出来", turn.Steps[0].Call)
	}
}
