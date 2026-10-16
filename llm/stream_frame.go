package llm

// frameEvents 把一帧 JSON 映射成事件；两种形态的帧语义完全不同，按 [OpenAI] 的形态分派。
func (p *OpenAI) frameEvents(frame map[string]any, state *streamState) ([]StreamEvent, error) {
	if p.style == StyleResponses {
		return p.responsesFrame(frame, state)
	}
	return p.chatFrame(frame, state), nil
}

// chatFrame 解析 Chat Completions 的一帧：用量、结束原因、正文 / 思考增量与工具调用分片。
func (p *OpenAI) chatFrame(frame map[string]any, state *streamState) []StreamEvent {
	if usage, ok := objectOf(frame["usage"]); ok {
		state.usage = usage
	}
	choice, found := firstChoice(frame)
	if !found {
		return nil
	}
	if reason, ok := choice["finish_reason"].(string); ok {
		state.finishReason = reason
	}
	delta, _ := objectOf(choice["delta"])
	accumulateChatToolCalls(delta["tool_calls"], state)
	return textEvents(delta)
}

// firstChoice 取 `choices[0]`；用量帧的 choices 是空数组，据此判定「本帧没有增量」。
func firstChoice(frame map[string]any) (map[string]any, bool) {
	choices, _ := frame["choices"].([]any)
	if len(choices) == 0 {
		return nil, false
	}
	return objectOf(choices[0])
}

// textEvents 把 delta 里的思考与正文增量转成事件（两者可能同帧到达，思考在前）。
func textEvents(delta map[string]any) []StreamEvent {
	events := make([]StreamEvent, 0, 2)
	if reasoning, ok := nonEmptyString(delta["reasoning_content"]); ok {
		events = append(events, ReasoningDelta{Text: reasoning})
	}
	if content, ok := nonEmptyString(delta["content"]); ok {
		events = append(events, TextDelta{Text: content})
	}
	return events
}

// accumulateChatToolCalls 累积 `delta.tool_calls`：按 index 定位，id / name 一次性到达，
// arguments 是 JSON 分片。
func accumulateChatToolCalls(raw any, state *streamState) {
	calls, ok := raw.([]any)
	if !ok {
		return
	}
	for _, entry := range calls {
		state.applyChatToolCall(entry)
	}
}

// applyChatToolCall 累积一路工具调用的分片；缺 index 时按「新的一路」追加在末尾。
func (s *streamState) applyChatToolCall(entry any) {
	call, ok := objectOf(entry)
	if !ok {
		return
	}
	index, ok := integralOf(call["index"])
	if !ok {
		index = len(s.chatCalls)
	}
	builder := s.chatBuilder(index)
	builder.applyCallID(call, "id")
	function, _ := objectOf(call["function"])
	builder.applyName(function["name"])
	builder.addArguments(function["arguments"])
}

// responsesFrame 解析 Responses 的一帧。
//
// REASON: 事件类型到处理的映射表，逐条平铺比拆成多层分支更容易核对（Dart 侧同样是 switch）。
func (p *OpenAI) responsesFrame(frame map[string]any, state *streamState) ([]StreamEvent, error) {
	switch textOf(frame["type"]) {
	case "response.output_text.delta":
		return textDeltaEvent(frame), nil
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		return reasoningDeltaEvent(frame), nil
	case "response.output_item.added", "response.output_item.done":
		state.accumulateResponsesItem(frame["item"])
		return nil, nil
	case "response.function_call_arguments.delta":
		state.appendResponsesArguments(frame)
		return nil, nil
	case "response.function_call_arguments.done":
		state.setResponsesArguments(frame)
		return nil, nil
	case "response.completed", "response.incomplete":
		state.applyResponsesEnd(textOf(frame["type"]), frame)
		return nil, nil
	case "response.failed":
		return nil, failedStreamError(p.name, frame)
	}
	return nil, nil
}

// textDeltaEvent 取 `output_text` 增量；空增量不产出事件。
func textDeltaEvent(frame map[string]any) []StreamEvent {
	text, ok := nonEmptyString(frame["delta"])
	if !ok {
		return nil
	}
	return []StreamEvent{TextDelta{Text: text}}
}

// reasoningDeltaEvent 取 reasoning 增量；空增量不产出事件。
func reasoningDeltaEvent(frame map[string]any) []StreamEvent {
	text, ok := nonEmptyString(frame["delta"])
	if !ok {
		return nil
	}
	return []StreamEvent{ReasoningDelta{Text: text}}
}

// accumulateResponsesItem 累积 `function_call` 输出项。
//
// 首帧（output_item.added）与终帧（output_item.done）各调一次，终帧的 arguments 是权威完整值。
func (s *streamState) accumulateResponsesItem(raw any) {
	item, ok := objectOf(raw)
	if !ok || item["type"] != "function_call" {
		return
	}
	builder := s.responsesBuilder(textOf(item["id"]))
	builder.applyCallID(item, "call_id")
	builder.applyName(item["name"])
	builder.setArguments(item["arguments"])
}

// appendResponsesArguments 追加 arguments 分片（按 item_id 定位）。
func (s *streamState) appendResponsesArguments(frame map[string]any) {
	s.responsesBuilder(textOf(frame["item_id"])).addArguments(frame["delta"])
}

// setResponsesArguments 以完整 arguments 覆盖（按 item_id 定位）。
func (s *streamState) setResponsesArguments(frame map[string]any) {
	s.responsesBuilder(textOf(frame["item_id"])).setArguments(frame["arguments"])
}

// applyResponsesEnd 处理流结束帧：用量与结束原因；incomplete 统一记为 length。
func (s *streamState) applyResponsesEnd(eventType string, frame map[string]any) {
	response, _ := objectOf(frame["response"])
	if usage, ok := objectOf(response["usage"]); ok {
		s.usage = usage
	}
	if eventType == "response.incomplete" {
		s.finishReason = "length"
		return
	}
	s.finishReason = defaultText(textOf(response["status"]), "completed")
}

// failedStreamError 把 `response.failed` 帧转成失败：服务端在流中途报错，拿不到状态码，
// 只能按服务端错误归类（可重试）。
func failedStreamError(provider string, frame map[string]any) *Error {
	response, _ := objectOf(frame["response"])
	detail, _ := objectOf(response["error"])
	return &Error{
		Provider: provider,
		Message:  "流式响应失败：" + defaultText(textOf(detail["message"]), "unknown"),
		Kind:     KindServer,
	}
}
