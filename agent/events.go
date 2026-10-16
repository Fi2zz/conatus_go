package agent

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Fi2zz/conatus_go/foundation/session"
	"github.com/Fi2zz/conatus_go/llm"
)

// ErrSessionClosed 表示绑定的会话已关闭：循环中止，不往下写。
var ErrSessionClosed = errors.New("会话已关闭")

// EnsureSessionOpen 检查会话还能不能写；已关闭时返回 [ErrSessionClosed]，没有会话时无事。
func EnsureSessionOpen(target *session.Session) error {
	if target == nil || !target.Closed() {
		return nil
	}
	return fmt.Errorf("%w：%s", ErrSessionClosed, target.ID())
}

// RecentEvents 取本轮要还原的历史窗口（没有会话时没有事件）。
//
// 现在是整段日志。按预算折叠开头的那段随压缩器一起顺延，届时这里多收一个窗口起点——
// 折叠掉的是日志开头的一段，窗口起点就是折叠条数，不能用「保留最近 N 条」反推。
func RecentEvents(target *session.Session) []session.Event {
	if target == nil {
		return nil
	}
	return target.Events()
}

// DeriveMessages 把会话事件还原成模型消息序列：只认三类消息事件，其余（llm/request、
// tool/call 之类的派生事件）不进模型上下文。
func DeriveMessages(events []session.Event) []llm.Message {
	messages := make([]llm.Message, 0, len(events))
	for _, event := range events {
		if message, ok := deriveMessage(event); ok {
			messages = append(messages, message)
		}
	}
	return messages
}

// deriveMessage 把一条事件还原成模型消息；不是消息事件（或负载不成形）时 ok 为 false。
func deriveMessage(event session.Event) (llm.Message, bool) {
	data := objectField(event.Data)
	if data == nil {
		return llm.Message{}, false
	}
	if event.Type == session.EventToolResult {
		return llm.Message{
			Role:       llm.RoleTool,
			Content:    textField(data, "content"),
			ToolCallID: textField(data, "callId"),
		}, true
	}
	return conversationalMessage(event.Type, data)
}

// conversationalMessage 还原 user / assistant 两类消息；其余类型 ok 为 false。
func conversationalMessage(eventType string, data map[string]any) (llm.Message, bool) {
	switch eventType {
	case session.EventUserMessage:
		return llm.Message{
			Role:    llm.RoleUser,
			Content: textField(data, "text"),
			Images:  ImagesFromJSON(data["images"]),
		}, true
	case session.EventAssistantMessage:
		return llm.Message{
			Role:      llm.RoleAssistant,
			Content:   textField(data, "text"),
			ToolCalls: ToolCallsFromJSON(data["toolCalls"]),
		}, true
	}
	return llm.Message{}, false
}

// ImagesToJSON 序列化图片列表为事件负载。
//
// 返回 `[]any` 而不是 `[]map[string]any`：后者落盘再读回来会变成前者，统一成一种形态，
// 免得「内存里的事件」与「从 JSONL 读回的事件」走两套解析。
func ImagesToJSON(images []llm.Image) []any {
	items := make([]any, 0, len(images))
	for _, image := range images {
		items = append(items, map[string]any{
			"mimeType":   image.MIMEType,
			"base64Data": image.Base64Data,
		})
	}
	return items
}

// ImagesFromJSON 还原事件负载里的 `images`；不成形的条目跳过（日志可能被手改过）。
func ImagesFromJSON(raw any) []llm.Image {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	images := make([]llm.Image, 0, len(items))
	for _, item := range items {
		entry := objectField(item)
		mime, mimeOK := entry["mimeType"].(string)
		data, dataOK := entry["base64Data"].(string)
		if mimeOK && dataOK {
			images = append(images, llm.Image{MIMEType: mime, Base64Data: data})
		}
	}
	return images
}

// ToolCallsToJSON 序列化工具调用列表为事件负载（形态理由同 [ImagesToJSON]）。
func ToolCallsToJSON(calls []llm.ToolCall) []any {
	items := make([]any, 0, len(calls))
	for _, call := range calls {
		items = append(items, map[string]any{
			"id":        call.ID,
			"name":      call.Name,
			"arguments": call.Arguments,
		})
	}
	return items
}

// ToolCallsFromJSON 还原事件负载里的 `toolCalls`；无名或缺 id 的条目跳过。
func ToolCallsFromJSON(raw any) []llm.ToolCall {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	calls := make([]llm.ToolCall, 0, len(items))
	for _, item := range items {
		entry := objectField(item)
		name, nameOK := entry["name"].(string)
		if !nameOK || name == "" {
			continue
		}
		calls = append(calls, llm.ToolCall{
			ID:        textField(entry, "id"),
			Name:      name,
			Arguments: defaultText(textField(entry, "arguments"), "{}"),
		})
	}
	return calls
}

// ParseToolArguments 解析模型给的原始 JSON 参数串；非法、非对象或 `null` 都返回空表
// （交给工具参数校验按「缺必填」处理，不在这里报错）。
func ParseToolArguments(raw string) map[string]any {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil || decoded == nil {
		return map[string]any{}
	}
	return decoded
}

// objectField 取值的对象形态；不是对象（包括负载缺失）时返回 nil。
func objectField(value any) map[string]any {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return object
}

// textField 取字符串字段，语义对齐 Dart 那版的字符串模板兜底：缺失或 null → 空串，
// 其余类型 → 它的字面形式（日志可能被手改成数字）。
func textField(data map[string]any, key string) string {
	value, found := data[key]
	if !found || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

// defaultText 空串时取缺省值。
func defaultText(text, fallback string) string {
	if text == "" {
		return fallback
	}
	return text
}
