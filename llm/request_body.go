package llm

// buildStreamBody 组装请求体。
//
// Go 侧请求恒为流式：Dart 的 `stream: false` 分支不可达（非流式签名也走流式端点），不移植。
// options 最后并入，与 Dart 一致——调用方可以用它覆盖任意字段（含 stream）。
func buildStreamBody(
	style APIStyle,
	model string,
	messages []Message,
	settings settings,
) map[string]any {
	body := map[string]any{"model": model, "stream": true}
	if style == StyleResponses {
		body["input"] = responsesInput(messages)
		body["store"] = false
	} else {
		body["messages"] = messageList(messages)
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	if len(settings.tools) > 0 {
		body["tools"] = projectedTools(style, settings.tools)
	}
	mergeInto(body, settings.values)
	return body
}

// messageList 把消息编译成 Chat Completions 的 messages 数组。
func messageList(messages []Message) []any {
	items := make([]any, 0, len(messages))
	for _, message := range messages {
		items = append(items, message.JSON())
	}
	return items
}

// projectedTools 按形态投影工具 schema。
func projectedTools(style APIStyle, tools []map[string]any) []any {
	items := make([]any, 0, len(tools))
	for _, tool := range tools {
		items = append(items, projectTool(style, tool))
	}
	return items
}

// toolDetails 取工具 schema 里模型可见的字段。
//
// 只有 name / description / parameters 下发——工具定义里的宿主字段（风险等级、分组、超时、
// 路径参数……）不是给模型的，混进 wire 等于把宿主实现细节透给服务端。
func toolDetails(tool map[string]any) map[string]any {
	return map[string]any{
		"name":        tool["name"],
		"description": tool["description"],
		"parameters":  tool["parameters"],
	}
}

// projectTool 投影单个工具：chat 需要 `function` 包裹，responses 直接平铺。
func projectTool(style APIStyle, tool map[string]any) map[string]any {
	if style != StyleResponses {
		return map[string]any{"type": "function", "function": toolDetails(tool)}
	}
	projection := toolDetails(tool)
	projection["type"] = "function"
	return projection
}

// mergeInto 把 options 原样并入请求体（后写覆盖先写）。
func mergeInto(body map[string]any, options map[string]any) {
	for key, value := range options {
		body[key] = value
	}
}

// responsesInput 把消息编译成 Responses 的 input 项。
func responsesInput(messages []Message) []any {
	items := make([]any, 0, len(messages))
	for _, message := range messages {
		items = append(items, responsesItems(message)...)
	}
	return items
}

// responsesItems 产出单条消息对应的 input 项：一条助手消息可能裂成「文本项 + 若干 function_call 项」。
func responsesItems(message Message) []any {
	if message.Role == RoleTool {
		return []any{map[string]any{
			"type":    "function_call_output",
			"call_id": message.ToolCallID,
			"output":  message.Content,
		}}
	}
	if len(message.ToolCalls) == 0 {
		return []any{responsesMessageItem(message)}
	}
	items := make([]any, 0, len(message.ToolCalls)+1)
	if message.Content != "" {
		items = append(items, responsesMessageItem(message))
	}
	for _, call := range message.ToolCalls {
		items = append(items, map[string]any{
			"type":      "function_call",
			"call_id":   call.ID,
			"name":      call.Name,
			"arguments": call.Arguments,
		})
	}
	return items
}

// responsesMessageItem 产出 Responses 的 message 项。
//
// Ark 的 /responses 强校验：message 项必须带 `type: message`，assistant 历史项另须 `status: completed`。
func responsesMessageItem(message Message) map[string]any {
	author := message.Role == RoleAssistant
	item := map[string]any{
		"type":    "message",
		"role":    message.Role,
		"content": messageContentParts(message, author),
	}
	if author {
		item["status"] = "completed"
	}
	return item
}

// messageContentParts 产出消息内容项：文本项按角色取 input_text / output_text，带图时追加 input_image。
func messageContentParts(message Message, author bool) []any {
	kind := "input_text"
	if author {
		kind = "output_text"
	}
	parts := make([]any, 0, len(message.Images)+1)
	parts = append(parts, map[string]any{"type": kind, "text": message.Content})
	for _, image := range message.Images {
		parts = append(parts, map[string]any{"type": "input_image", "image_url": image.DataURL()})
	}
	return parts
}
