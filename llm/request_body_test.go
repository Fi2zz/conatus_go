package llm_test

import (
	"context"
	"testing"

	"github.com/Fi2zz/conatus_go/llm"
)

func TestRequestChatBodyShape(t *testing.T) {
	transport := &fakeTransport{body: sseBody("[DONE]")}
	provider := newTestProvider(t, transport)

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "hi"},
	}
	options := map[string]any{"temperature": 0.2}
	if _, err := provider.Chat(context.Background(), messages, llm.WithOptions(options)); err != nil {
		t.Fatalf("请求失败：%v", err)
	}

	body := onlyRequest(t, transport).bodyObject(t)
	if body["model"] != "doubao-seed-1-8-251228" || body["stream"] != true {
		t.Fatalf("请求体 = %v", body)
	}
	if body["temperature"] != 0.2 {
		t.Fatalf("options 应原样并入请求体：%v", body)
	}
	if _, found := body["input"]; found {
		t.Fatalf("chat 形态不该有 input：%v", body)
	}
	streamOptions, ok := body["stream_options"].(map[string]any)
	if !ok || streamOptions["include_usage"] != true {
		t.Fatalf("stream_options = %v，期望 include_usage", body["stream_options"])
	}
	items := arrayOf(t, body, "messages")
	if len(items) != 2 || objectAt(t, items, 0)["role"] != "system" {
		t.Fatalf("messages = %v", items)
	}
}

func TestRequestResponsesBodyShape(t *testing.T) {
	transport := &fakeTransport{body: sseBody("[DONE]")}
	provider := newTestProvider(t, transport, llm.WithAPIStyle(llm.StyleResponses))

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleAssistant, Content: "prev"},
		{Role: llm.RoleUser, Content: "hi"},
	}
	if _, err := provider.Chat(context.Background(), messages); err != nil {
		t.Fatalf("请求失败：%v", err)
	}

	body := onlyRequest(t, transport).bodyObject(t)
	if body["store"] != false {
		t.Fatal("responses 形态必须显式 store=false：避免敏感原文留存在服务端")
	}
	if _, found := body["messages"]; found {
		t.Fatalf("responses 形态不该有 messages：%v", body)
	}
	if _, found := body["stream_options"]; found {
		t.Fatalf("responses 形态不该带 stream_options：%v", body)
	}

	items := arrayOf(t, body, "input")
	if len(items) != 3 {
		t.Fatalf("input = %v，期望 3 项", items)
	}
	author := objectAt(t, items, 1)
	if author["type"] != "message" || author["status"] != "completed" {
		t.Fatalf("助手历史项 = %v，期望 type=message 且 status=completed", author)
	}
	parts := arrayOf(t, author, "content")
	if objectAt(t, parts, 0)["type"] != "output_text" {
		t.Fatalf("助手历史项内容 = %v，期望 output_text", parts)
	}
}

func TestRequestResponsesSerializesToolHistory(t *testing.T) {
	transport := &fakeTransport{body: sseBody("[DONE]")}
	provider := newTestProvider(t, transport, llm.WithAPIStyle(llm.StyleResponses))

	messages := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c1", Name: "get_time", Arguments: "{}"}}},
		{Role: llm.RoleTool, Content: "12:00", ToolCallID: "c1"},
	}
	if _, err := provider.Chat(context.Background(), messages); err != nil {
		t.Fatalf("请求失败：%v", err)
	}

	items := arrayOf(t, onlyRequest(t, transport).bodyObject(t), "input")
	if len(items) != 2 {
		t.Fatalf("input = %v，无正文的助手消息不该产出 message 项", items)
	}
	call := objectAt(t, items, 0)
	if call["type"] != "function_call" || call["call_id"] != "c1" || call["name"] != "get_time" {
		t.Fatalf("工具调用项 = %v", call)
	}
	output := objectAt(t, items, 1)
	if output["type"] != "function_call_output" || output["call_id"] != "c1" || output["output"] != "12:00" {
		t.Fatalf("工具结果项 = %v", output)
	}
}

// 助手消息既有正文又有工具调用：先产出 message 项，再产出 function_call 项。
func TestRequestResponsesKeepsAssistantTextWithToolCalls(t *testing.T) {
	transport := &fakeTransport{body: sseBody("[DONE]")}
	provider := newTestProvider(t, transport, llm.WithAPIStyle(llm.StyleResponses))

	messages := []llm.Message{{
		Role:      llm.RoleAssistant,
		Content:   "先说明再调用",
		ToolCalls: []llm.ToolCall{{ID: "c1", Name: "get_time", Arguments: "{}"}},
	}}
	if _, err := provider.Chat(context.Background(), messages); err != nil {
		t.Fatalf("请求失败：%v", err)
	}

	items := arrayOf(t, onlyRequest(t, transport).bodyObject(t), "input")
	if len(items) != 2 {
		t.Fatalf("input = %v，期望文本项 + 工具调用项", items)
	}
	if objectAt(t, items, 0)["type"] != "message" || objectAt(t, items, 1)["type"] != "function_call" {
		t.Fatalf("input = %v", items)
	}
}

func TestRequestImagesShapeFollowsStyle(t *testing.T) {
	image := llm.Image{MIMEType: "image/png", Base64Data: "aGVsbG8="}
	messages := []llm.Message{{Role: llm.RoleUser, Content: "看这张图", Images: []llm.Image{image}}}

	chatTransport := &fakeTransport{body: sseBody("[DONE]")}
	chatProvider := newTestProvider(t, chatTransport)
	if _, err := chatProvider.Chat(context.Background(), messages); err != nil {
		t.Fatalf("chat 形态请求失败：%v", err)
	}
	chatParts := arrayOf(t, objectAt(t, arrayOf(t, onlyRequest(t, chatTransport).bodyObject(t), "messages"), 0), "content")
	if objectAt(t, chatParts, 0)["type"] != "text" {
		t.Fatalf("chat 内容分片 = %v", chatParts)
	}
	chatImage := objectAt(t, chatParts, 1)
	if chatImage["type"] != "image_url" {
		t.Fatalf("chat 图片分片 = %v", chatImage)
	}
	inner, ok := chatImage["image_url"].(map[string]any)
	if !ok || inner["url"] != image.DataURL() {
		t.Fatalf("chat 图片 = %v，期望嵌套 data URL", chatImage)
	}

	responseTransport := &fakeTransport{body: sseBody("[DONE]")}
	responseProvider := newTestProvider(t, responseTransport, llm.WithAPIStyle(llm.StyleResponses))
	if _, err := responseProvider.Chat(context.Background(), messages); err != nil {
		t.Fatalf("responses 形态请求失败：%v", err)
	}
	responseParts := arrayOf(t, objectAt(t, arrayOf(t, onlyRequest(t, responseTransport).bodyObject(t), "input"), 0), "content")
	if objectAt(t, responseParts, 0)["type"] != "input_text" {
		t.Fatalf("responses 内容分片 = %v", responseParts)
	}
	responseImage := objectAt(t, responseParts, 1)
	if responseImage["type"] != "input_image" || responseImage["image_url"] != image.DataURL() {
		t.Fatalf("responses 图片分片 = %v，期望 image_url 直挂 data URL", responseImage)
	}
}
