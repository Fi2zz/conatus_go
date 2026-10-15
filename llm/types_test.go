package llm_test

import (
	"reflect"
	"testing"

	"github.com/Fi2zz/conatus_go/llm"
)

func TestToolCallJSONProjectsFunctionCall(t *testing.T) {
	call := llm.ToolCall{ID: "call_1", Name: "web_search", Arguments: `{"query":"x"}`}

	got := call.JSON()
	want := map[string]any{
		"id":   "call_1",
		"type": "function",
		"function": map[string]any{
			"name":      "web_search",
			"arguments": `{"query":"x"}`,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("投影 = %v，期望 %v", got, want)
	}
}

func TestImageDataURL(t *testing.T) {
	image := llm.Image{MIMEType: "image/png", Base64Data: "AAAA"}

	if got := image.DataURL(); got != "data:image/png;base64,AAAA" {
		t.Fatalf("data URL = %q", got)
	}
}

func TestPlainMessageJSONKeepsContentAsString(t *testing.T) {
	message := llm.Message{Role: llm.RoleUser, Content: "你好"}

	got := message.JSON()

	if len(got) != 2 {
		t.Fatalf("纯文本消息只应有 role 与 content：%v", got)
	}
	if got["role"] != llm.RoleUser || got["content"] != "你好" {
		t.Fatalf("投影 = %v", got)
	}
}

func TestMessageWithImagesProjectsContentParts(t *testing.T) {
	message := llm.Message{
		Role:    llm.RoleUser,
		Content: "看这张图",
		Images:  []llm.Image{{MIMEType: "image/png", Base64Data: "AAAA"}},
	}

	parts, ok := message.JSON()["content"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("带图消息的 content 应是分片数组：%v", message.JSON()["content"])
	}
	text, ok := parts[0].(map[string]any)
	if !ok || text["type"] != "text" || text["text"] != "看这张图" {
		t.Fatalf("首片 = %v，期望 text 分片", parts[0])
	}
	imagePart, ok := parts[1].(map[string]any)
	if !ok || imagePart["type"] != "image_url" {
		t.Fatalf("次片 = %v，期望 image_url 分片", parts[1])
	}
	nested, ok := imagePart["image_url"].(map[string]any)
	if !ok || nested["url"] != "data:image/png;base64,AAAA" {
		t.Fatalf("image_url = %v", imagePart["image_url"])
	}
}

func TestToolResultAndAssistantMessagesProjectOptionalFields(t *testing.T) {
	result := llm.Message{Role: llm.RoleTool, Content: "20 度", ToolCallID: "call_1"}
	resultJSON := result.JSON()
	if resultJSON["tool_call_id"] != "call_1" {
		t.Fatalf("工具结果消息应带 tool_call_id：%v", resultJSON)
	}
	if _, found := resultJSON["tool_calls"]; found {
		t.Fatalf("工具结果消息不该带 tool_calls：%v", resultJSON)
	}

	assistant := llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
		{ID: "call_1", Name: "get_time", Arguments: "{}"},
	}}
	assistantJSON := assistant.JSON()
	calls, ok := assistantJSON["tool_calls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("助手消息应带 tool_calls：%v", assistantJSON)
	}
	if _, found := assistantJSON["tool_call_id"]; found {
		t.Fatalf("助手消息不该带 tool_call_id：%v", assistantJSON)
	}
}

// TestCacheableFlagNeverLeavesProcess 钉住一条安全边界：cacheable 是纯本地标记，
// 任何情况下都不该出现在请求体里（非标字段可能被服务端拒绝）。
func TestCacheableFlagNeverLeavesProcess(t *testing.T) {
	message := llm.Message{Role: llm.RoleSystem, Content: "系统提示", Cacheable: true}

	if _, found := message.JSON()["cacheable"]; found {
		t.Fatalf("cacheable 不该写进请求体：%v", message.JSON())
	}
}

func TestAPIStyleNames(t *testing.T) {
	if got := llm.StyleChat.String(); got != "chat" {
		t.Fatalf("StyleChat = %q，期望 chat", got)
	}
	if got := llm.StyleResponses.String(); got != "responses" {
		t.Fatalf("StyleResponses = %q，期望 responses", got)
	}
}

// TestStreamEventsFormASealedFamily 用类型 switch 穷尽三个实现：Go 没有 sealed class，
// 这个测试是「家族成员就是这三个」的可执行说明。
func TestStreamEventsFormASealedFamily(t *testing.T) {
	events := []llm.StreamEvent{
		llm.TextDelta{Text: "增量"},
		llm.ReasoningDelta{Text: "思考"},
		llm.StreamDone{FinishReason: "stop", Provider: "doubao", Model: "x"},
	}
	seen := []string{}
	for _, event := range events {
		switch typed := event.(type) {
		case llm.TextDelta:
			seen = append(seen, "text:"+typed.Text)
		case llm.ReasoningDelta:
			seen = append(seen, "reasoning:"+typed.Text)
		case llm.StreamDone:
			seen = append(seen, "done:"+typed.Provider)
		default:
			t.Fatalf("意外的事件类型 %T", event)
		}
	}

	want := []string{"text:增量", "reasoning:思考", "done:doubao"}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("事件 = %v，期望 %v", seen, want)
	}
}
