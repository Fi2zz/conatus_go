package llm

import (
	"context"
	"io"
)

// openStream 发请求并交出响应体。
//
// 缺 Key 在发请求前就失败：不发注定被拒的请求，也不白耗一次配额。
func (p *OpenAI) openStream(
	ctx context.Context,
	messages []Message,
	settings settings,
) (io.ReadCloser, error) {
	if err := p.requireKey(); err != nil {
		return nil, err
	}
	response, err := p.send(ctx, messages, settings)
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

// pumpFrames 逐帧拉取并产出；返回 false 表示不该再产终态事件（出错或消费方提前停止）。
func (p *OpenAI) pumpFrames(
	body io.Reader,
	state *streamState,
	yield func(StreamEvent, error) bool,
) bool {
	for payload, err := range SSEPayloads(body) {
		if err != nil {
			yield(nil, err)
			return false
		}
		if !p.emitFrame(payload, state, yield) {
			return false
		}
	}
	return true
}

// emitFrame 解析一帧并产出它的事件；载荷不是 JSON 对象时静默跳过（服务端可能插心跳）。
func (p *OpenAI) emitFrame(
	payload string,
	state *streamState,
	yield func(StreamEvent, error) bool,
) bool {
	frame, ok := decodeFrame(payload)
	if !ok {
		return true
	}
	events, err := p.frameEvents(frame, state)
	if err != nil {
		yield(nil, err)
		return false
	}
	return emitEvents(events, yield)
}

// emitEvents 依次产出事件；消费方停止时返回 false。
func emitEvents(events []StreamEvent, yield func(StreamEvent, error) bool) bool {
	for _, event := range events {
		if !yield(event, nil) {
			return false
		}
	}
	return true
}

// doneEvent 组装终态事件：用量、结束原因与累积完成的工具调用。
func (p *OpenAI) doneEvent(state *streamState) StreamDone {
	return StreamDone{
		Usage:        state.usage,
		FinishReason: state.finishReason,
		ToolCalls:    state.buildToolCalls(),
		Provider:     p.name,
		Model:        p.model,
	}
}
