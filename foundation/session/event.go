// Package session 是会话事件日志：append-only、可 fork、可重放、可落盘
// （对应 Dart 的 session / session-persistence / session-store 三个插件）。
//
// Go 侧的两个关键取舍（也写进 AGENTS.md）：
//
//   - **落盘改为同步**：Append 返回即已持久化，因此没有 Dart 的「写入链 + Flush」，
//     也就不存在「忘了 flush 导致丢事件」的坑。落盘失败无法从 Append 返回
//     （事件监听器没有错误通道），交给 store 的错误接收器。
//   - **事件日志用不可变快照 + 原子发布**：读路径（Events / Read / LastEventID）
//     不加锁，因此监听器里可以安全地读会话（Dart 的 model-visible 不变式检查就要
//     这么做）；写路径（Append / Close / Fork）在会话锁内串行，事件顺序即 seq 顺序。
//     监听器在追加锁内被调用，**不得重入 Append / Close / Fork**（会死锁）。
package session

import (
	"fmt"
	"sync/atomic"
	"time"
)

// 事件类型常量（与 Dart 版逐字一致：这些字符串是会话日志的对外契约）。
const (
	// EventUserMessage 用户消息。
	EventUserMessage = "user/message"
	// EventAssistantMessage 助手消息（可携带工具调用）。
	EventAssistantMessage = "assistant/message"
	// EventToolResult 工具结果。
	EventToolResult = "tool/result"
)

// Event 是会话日志中的一条事件。
//
// Seq 由所属会话在追加时分配（从 0 起单调递增），是事件在日志中的稳定位置；
// ID 是事件唯一标识，用于跨会话引用与 fork；SessionID 标明归属会话；
// ParentEventID 表达因果关系（如工具结果指向触发它的助手消息）。
type Event struct {
	Seq           int
	Type          string
	Time          time.Time
	Data          any
	ID            string
	SessionID     string
	ParentEventID string
}

// EventOption 定制一条事件。
type EventOption func(*Event)

// WithEventData 携带可 JSON 序列化的负载。
func WithEventData(data any) EventOption {
	return func(e *Event) { e.Data = data }
}

// WithEventTime 指定事件时间（缺省取当前时间）。
func WithEventTime(moment time.Time) EventOption {
	return func(e *Event) { e.Time = moment }
}

// WithEventID 指定事件 id（缺省自动生成）。
func WithEventID(id string) EventOption {
	return func(e *Event) { e.ID = id }
}

// WithParentEventID 指定父事件 id。
func WithParentEventID(id string) EventOption {
	return func(e *Event) { e.ParentEventID = id }
}

// NewEvent 构造一条事件；未给出的字段取缺省（当前时间 + 自动生成 id）。
func NewEvent(sessionID, eventType string, seq int, opts ...EventOption) Event {
	event := Event{SessionID: sessionID, Type: eventType, Seq: seq, Time: time.Now()}
	for _, opt := range opts {
		opt(&event)
	}
	if event.ID == "" {
		event.ID = NextEventID()
	}
	return event
}

var eventSeq atomic.Uint64

// NextEventID 生成事件唯一 id：进程内单调、跨会话唯一。
func NextEventID() string {
	return fmt.Sprintf("evt-%d-%d", time.Now().UnixMicro(), eventSeq.Add(1)-1)
}
