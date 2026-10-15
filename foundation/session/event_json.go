package session

import (
	"time"

	"github.com/Fi2zz/conatus_go/core"
)

// ToJSON 把事件序列化为 JSON 可编码的映射；负载中的凭据字段会被自动脱敏。
//
// 与 Dart 版的差别是时间的文本形态：那边是 `toIso8601String()` 的本地时间，Go 侧用
// RFC3339Nano（带时区偏移，语义更明确且能被 time.Parse 往返）。
func (e Event) ToJSON() map[string]any {
	fields := map[string]any{
		"seq":  e.Seq,
		"type": e.Type,
		"time": e.Time.Format(time.RFC3339Nano),
	}
	if e.Data != nil {
		fields["data"] = core.RedactSecrets(e.Data)
	}
	if e.ID != "" {
		fields["id"] = e.ID
	}
	if e.SessionID != "" {
		fields["sessionId"] = e.SessionID
	}
	if e.ParentEventID != "" {
		fields["parentEventId"] = e.ParentEventID
	}
	return fields
}

// EventFromJSON 从 JSON 解码出的映射还原事件。
//
// 字段缺失或形态不符时取零值（时间无法解析时为零时间），不返回错误：日志里出现坏行
// 不应该让整个会话打不开，Dart 版同样尽力还原。
func EventFromJSON(fields map[string]any) Event {
	return Event{
		Seq:           intField(fields, "seq"),
		Type:          stringField(fields, "type"),
		Time:          timeField(fields, "time"),
		Data:          fields["data"],
		ID:            stringField(fields, "id"),
		SessionID:     stringField(fields, "sessionId"),
		ParentEventID: stringField(fields, "parentEventId"),
	}
}

func stringField(fields map[string]any, key string) string {
	text, _ := fields[key].(string)
	return text
}

func intField(fields map[string]any, key string) int {
	switch typed := fields[key].(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return 0
	}
}

func timeField(fields map[string]any, key string) time.Time {
	moment, err := time.Parse(time.RFC3339Nano, stringField(fields, key))
	if err != nil {
		return time.Time{}
	}
	return moment
}
