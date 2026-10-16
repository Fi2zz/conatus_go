package llm

import (
	"encoding/json"
	"strings"
)

// streamState 是流式累积状态：用量与结束原因可能晚于增量到达，工具调用分片要攒到收尾才成形。
type streamState struct {
	usage        map[string]any
	finishReason string

	// chatCalls 按 `delta.tool_calls[].index` 累积（Chat Completions）。
	chatCalls []*toolCallBuilder

	// responseCalls 按输出项 id 累积（Responses）；responseIndex 定位下标。
	//
	// 与 Dart 的差异：Dart 用的是插入有序的 LinkedHashMap，而 Go 的 map 遍历无序，
	// 直接遍历会让并行工具调用**乱序**（顺序对模型侧有语义）。故用「切片保序 + map 定位」
	// 复刻插入顺序。
	responseCalls []*toolCallBuilder
	responseIndex map[string]int
}

func newStreamState() *streamState {
	return &streamState{responseIndex: map[string]int{}}
}

// chatBuilder 取第 index 路工具调用的累积器（中间跳号会补占位，收尾时按名字过滤掉）。
func (s *streamState) chatBuilder(index int) *toolCallBuilder {
	for len(s.chatCalls) <= index {
		s.chatCalls = append(s.chatCalls, &toolCallBuilder{})
	}
	return s.chatCalls[index]
}

// responsesBuilder 取某个输出项的累积器；首次出现时登记，保证产出顺序等于到达顺序。
func (s *streamState) responsesBuilder(itemID string) *toolCallBuilder {
	if index, found := s.responseIndex[itemID]; found {
		return s.responseCalls[index]
	}
	s.responseIndex[itemID] = len(s.responseCalls)
	s.responseCalls = append(s.responseCalls, &toolCallBuilder{})
	return s.responseCalls[len(s.responseCalls)-1]
}

// buildToolCalls 汇总已完成的工具调用；丢弃只收到分片、始终无名的占位项。
//
// 没有任何工具调用时返回 nil（仓内对「没有」一律用 nil 切片，避免调用方区分 nil 与空切片）。
func (s *streamState) buildToolCalls() []ToolCall {
	var calls []ToolCall
	calls = appendBuilt(calls, s.chatCalls)
	return appendBuilt(calls, s.responseCalls)
}

// appendBuilt 把成形的工具调用追加到 calls（无名的占位项被跳过）。
func appendBuilt(calls []ToolCall, builders []*toolCallBuilder) []ToolCall {
	for _, builder := range builders {
		if call, ok := builder.build(); ok {
			calls = append(calls, call)
		}
	}
	return calls
}

// toolCallBuilder 累积一次工具调用的分片：id 与 name 通常一次性到达，arguments 是 JSON 分片。
type toolCallBuilder struct {
	id        string
	name      string
	arguments strings.Builder
}

// addArguments 追加 arguments 分片；非字符串（如已解析的对象）按 JSON 编码后追加。
func (b *toolCallBuilder) addArguments(chunk any) {
	text, ok := chunk.(string)
	if ok {
		b.appendText(text)
		return
	}
	b.appendEncoded(chunk)
}

// appendText 追加字符串分片；空串忽略（分片缺省时不影响已攒内容）。
func (b *toolCallBuilder) appendText(text string) {
	if text != "" {
		b.arguments.WriteString(text)
	}
}

// appendEncoded 把非字符串分片编码后追加；nil 忽略。
func (b *toolCallBuilder) appendEncoded(chunk any) {
	if chunk == nil {
		return
	}
	encoded, err := json.Marshal(chunk)
	if err != nil {
		// 分片来自本包自己的 JSON 解码结果，编码不会失败；真失败就丢弃这一片。
		return
	}
	b.arguments.WriteString(string(encoded))
}

// setArguments 以完整 arguments 覆盖；空串忽略（终帧缺省时不抹掉已累积分片）。
func (b *toolCallBuilder) setArguments(complete any) {
	text, ok := complete.(string)
	if !ok || text == "" {
		return
	}
	b.arguments.Reset()
	b.arguments.WriteString(text)
}

// applyCallID 吸收调用 id（分片里的空 id 不覆盖已有值）。
func (b *toolCallBuilder) applyCallID(source map[string]any, key string) {
	if id, ok := nonEmptyString(source[key]); ok {
		b.id = id
	}
}

// applyName 吸收工具名（同样忽略空串）。
func (b *toolCallBuilder) applyName(value any) {
	if name, ok := nonEmptyString(value); ok {
		b.name = name
	}
}

// build 产出最终调用；始终无名的占位项被丢弃（ok 为 false）。
func (b *toolCallBuilder) build() (ToolCall, bool) {
	if b.name == "" {
		return ToolCall{}, false
	}
	arguments := b.arguments.String()
	if arguments == "" {
		arguments = "{}"
	}
	return ToolCall{ID: b.id, Name: b.name, Arguments: arguments}, true
}
