package llm_test

import (
	"context"
	"slices"
	"testing"

	"github.com/Fi2zz/conatus_go/llm"
)

// testTools 复刻 Dart 侧 llm_tools_test 的工具 schema（只含模型可见字段）。
func testTools() []map[string]any {
	return []map[string]any{{
		"name":        "get_time",
		"description": "返回当前时间",
		"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
	}}
}

func TestOpenAIChatStreamAccumulatesToolCallFragments(t *testing.T) {
	transport := &fakeTransport{body: sseBody(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_time","arguments":""}}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"tz\":"}}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"utc\"}"}}]},"finish_reason":"tool_calls"}]}`,
		"[DONE]",
	)}
	provider := newTestProvider(t, transport)

	events := collect(t, provider.ChatStream(context.Background(), userMessage(), llm.WithTools(testTools())))
	done := doneOf(t, events)

	if done.FinishReason != "tool_calls" {
		t.Fatalf("结束原因 = %q", done.FinishReason)
	}
	if len(done.ToolCalls) != 1 {
		t.Fatalf("工具调用 = %+v，期望 1 个", done.ToolCalls)
	}
	call := done.ToolCalls[0]
	if call.ID != "call_1" || call.Name != "get_time" || call.Arguments != `{"tz":"utc"}` {
		t.Fatalf("工具调用 = %+v，期望分片拼成完整参数", call)
	}
}

func TestOpenAIChatStreamKeepsParallelCallsByIndex(t *testing.T) {
	transport := &fakeTransport{body: sseBody(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"get_time","arguments":"{}"}},{"index":1,"id":"c2","function":{"name":"echo","arguments":"{\"a\":"}}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"1}"}}]},"finish_reason":"tool_calls"}]}`,
		"[DONE]",
	)}
	provider := newTestProvider(t, transport)

	events := collect(t, provider.ChatStream(context.Background(), userMessage(), llm.WithTools(testTools())))
	calls := doneOf(t, events).ToolCalls

	if names := toolNames(calls); !slices.Equal(names, []string{"get_time", "echo"}) {
		t.Fatalf("工具调用 = %v，期望按 index 顺序", names)
	}
	if calls[1].Arguments != `{"a":1}` {
		t.Fatalf("第二路参数 = %q，期望分片补齐", calls[1].Arguments)
	}
}

func TestOpenAIChatStreamDropsNamelessPlaceholder(t *testing.T) {
	transport := &fakeTransport{body: sseBody(
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		"[DONE]",
	)}
	provider := newTestProvider(t, transport)

	events := collect(t, provider.ChatStream(context.Background(), userMessage(), llm.WithTools(testTools())))

	if calls := doneOf(t, events).ToolCalls; calls != nil {
		t.Fatalf("工具调用 = %+v，只有分片、始终无名的占位项应被丢弃", calls)
	}
}

func TestOpenAIToolCallArgumentsNormalize(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			"缺 arguments 字段退化成空对象",
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"get_time"}}]},"finish_reason":"tool_calls"}]}`,
			"{}",
		},
		{
			"非字符串分片按 JSON 编码",
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"get_time","arguments":{"tz":"utc"}}}]},"finish_reason":"tool_calls"}]}`,
			`{"tz":"utc"}`,
		},
	}
	for _, item := range cases {
		transport := &fakeTransport{body: sseBody(item.payload, "[DONE]")}
		provider := newTestProvider(t, transport)

		events := collect(t, provider.ChatStream(context.Background(), userMessage(), llm.WithTools(testTools())))
		calls := doneOf(t, events).ToolCalls

		if len(calls) != 1 || calls[0].Arguments != item.want {
			t.Fatalf("%s：工具调用 = %+v，期望参数 %q", item.name, calls, item.want)
		}
	}
}

func TestOpenAIResponsesStreamAccumulatesFunctionCalls(t *testing.T) {
	transport := &fakeTransport{body: sseBody(
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_9","name":"get_time","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"x\":"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"1}"}`,
		`{"type":"response.function_call_arguments.done","item_id":"fc_1","arguments":"{\"x\":1}"}`,
		`{"type":"response.completed","response":{"status":"completed","usage":{"total_tokens":3}}}`,
	)}
	provider := newTestProvider(t, transport, llm.WithAPIStyle(llm.StyleResponses))

	events := collect(t, provider.ChatStream(context.Background(), userMessage(), llm.WithTools(testTools())))
	calls := doneOf(t, events).ToolCalls

	if len(calls) != 1 {
		t.Fatalf("工具调用 = %+v，期望 1 个", calls)
	}
	if calls[0].ID != "call_9" || calls[0].Name != "get_time" || calls[0].Arguments != `{"x":1}` {
		t.Fatalf("工具调用 = %+v，期望 arguments.done 的完整值覆盖分片", calls[0])
	}
}

// Go 的 map 遍历无序，而工具调用顺序有语义：保序靠的是切片 + 下标，这条用例是它的守卫。
func TestOpenAIResponsesStreamKeepsToolCallOrder(t *testing.T) {
	transport := &fakeTransport{body: sseBody(
		`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"c1","name":"first","arguments":"{}"}}`,
		`{"type":"response.output_item.added","item":{"id":"fc_2","type":"function_call","call_id":"c2","name":"second","arguments":"{}"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"1"}`,
		`{"type":"response.output_item.added","item":{"id":"fc_3","type":"function_call","call_id":"c3","name":"third","arguments":"{}"}}`,
		`{"type":"response.completed","response":{"status":"completed"}}`,
	)}
	provider := newTestProvider(t, transport, llm.WithAPIStyle(llm.StyleResponses))

	events := collect(t, provider.ChatStream(context.Background(), userMessage(), llm.WithTools(testTools())))

	want := []string{"first", "second", "third"}
	if names := toolNames(doneOf(t, events).ToolCalls); !slices.Equal(names, want) {
		t.Fatalf("工具调用顺序 = %v，期望 %v", names, want)
	}
}

func TestOpenAIResponsesStreamIgnoresEmptyDoneArguments(t *testing.T) {
	transport := &fakeTransport{body: sseBody(
		`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"c1","name":"get_time","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"a\":1}"}`,
		`{"type":"response.function_call_arguments.done","item_id":"fc_1","arguments":""}`,
		`{"type":"response.completed","response":{"status":"completed"}}`,
	)}
	provider := newTestProvider(t, transport, llm.WithAPIStyle(llm.StyleResponses))

	events := collect(t, provider.ChatStream(context.Background(), userMessage(), llm.WithTools(testTools())))
	calls := doneOf(t, events).ToolCalls

	if len(calls) != 1 || calls[0].Arguments != `{"a":1}` {
		t.Fatalf("工具调用 = %+v，空 done 不该抹掉已攒分片", calls)
	}
}

// 分片里 `index` 不可用（缺失、类型不符）或条目根本不是对象时不能崩：不可用的 index
// 按「新的一路」追加在末尾，非对象条目直接跳过。
func TestOpenAIChatStreamToleratesBrokenToolCallFragments(t *testing.T) {
	transport := &fakeTransport{body: sseBody(
		`{"choices":[{"delta":{"tool_calls":[42,{"id":"c1","function":{"name":"first","arguments":"{}"}},{"index":"不合法","function":{"name":"second","arguments":"{}"}},{"id":"c2","function":{"name":"third","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		"[DONE]",
	)}
	provider := newTestProvider(t, transport)

	events := collect(t, provider.ChatStream(context.Background(), userMessage(), llm.WithTools(testTools())))

	want := []string{"first", "second", "third"}
	if names := toolNames(doneOf(t, events).ToolCalls); !slices.Equal(names, want) {
		t.Fatalf("工具调用 = %v，期望 %v", names, want)
	}
}

func TestOpenAIToolsProjectionFollowsStyle(t *testing.T) {
	chatTransport := &fakeTransport{body: sseBody("[DONE]")}
	chatProvider := newTestProvider(t, chatTransport)
	if _, err := chatProvider.Chat(context.Background(), userMessage(), llm.WithTools(testTools())); err != nil {
		t.Fatalf("chat 形态请求失败：%v", err)
	}
	chatTool := objectAt(t, arrayOf(t, onlyRequest(t, chatTransport).bodyObject(t), "tools"), 0)
	if chatTool["type"] != "function" {
		t.Fatalf("chat 工具 = %v", chatTool)
	}
	function, wrapped := chatTool["function"].(map[string]any)
	if !wrapped || function["name"] != "get_time" {
		t.Fatalf("chat 工具需要 function 包裹：%v", chatTool)
	}
	if _, leaked := chatTool["name"]; leaked {
		t.Fatalf("chat 形态不该把 name 平铺在顶层：%v", chatTool)
	}

	responseTransport := &fakeTransport{body: sseBody("[DONE]")}
	responseProvider := newTestProvider(t, responseTransport, llm.WithAPIStyle(llm.StyleResponses))
	_, err := responseProvider.Chat(context.Background(), userMessage(), llm.WithTools(testTools()))
	if err != nil {
		t.Fatalf("responses 形态请求失败：%v", err)
	}
	responseTool := objectAt(t, arrayOf(t, onlyRequest(t, responseTransport).bodyObject(t), "tools"), 0)
	if responseTool["type"] != "function" || responseTool["name"] != "get_time" {
		t.Fatalf("responses 工具 = %v", responseTool)
	}
	if _, wrapped := responseTool["function"]; wrapped {
		t.Fatalf("responses 形态不该有 function 包裹：%v", responseTool)
	}
}
