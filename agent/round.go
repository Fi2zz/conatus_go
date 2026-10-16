package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/Fi2zz/conatus_go/foundation/session"
	"github.com/Fi2zz/conatus_go/foundation/tools"
	"github.com/Fi2zz/conatus_go/llm"
)

// round 是一轮的进行态：消息序列、已记步骤与用量，外加这一轮的 ctx 与用户输入。
//
// 收成结构是为了让每个函数都在参数上限内，并且这一轮的状态只沿一条路径流动。
type round struct {
	loop      *Loop
	ctx       context.Context
	userInput string
	messages  []llm.Message
	steps     []Step
	usage     []map[string]any
}

// stepThrough 逐步推进到收口；步数用尽时以「未收口」文案收尾。
func (r *round) stepThrough() (Turn, error) {
	for index := 0; index < r.loop.maxSteps; index++ {
		reply, closed, err := r.step(index)
		if err != nil {
			return Turn{}, err
		}
		if closed {
			return r.finish(reply)
		}
	}
	return r.finish(fmt.Sprintf("（已达到最大步数 %d，未收口）", r.loop.maxSteps))
}

// step 走一步：取一次模型回复；没有工具调用就算收口，否则执行工具并回填。
func (r *round) step(index int) (string, bool, error) {
	if err := r.prepareStep(); err != nil {
		return "", false, err
	}
	result, err := r.askModel()
	if err != nil {
		return "", false, err
	}
	r.usage = append(r.usage, result.Usage)
	r.loop.observe("agent.round", map[string]any{
		"step":          index,
		"toolCalls":     len(result.ToolCalls),
		"contentLength": len(result.Content),
	})
	if len(result.ToolCalls) == 0 {
		return strings.TrimSpace(result.Content), true, nil
	}
	return "", false, r.appendToolRound(result)
}

// prepareStep 每步开场：会话还开着，且把排队的用户插话注入消息序列。
func (r *round) prepareStep() error {
	if err := EnsureSessionOpen(r.loop.session); err != nil {
		return err
	}
	return r.injectSteering()
}

// injectSteering 取回排队的用户插话：逐条写会话并进消息序列，本轮模型立刻可见。
//
// 取消时已经注入的消息留在会话里——说出去的话不收回。
func (r *round) injectSteering() error {
	source := r.loop.steering
	if source == nil {
		return nil
	}
	texts, err := source(r.ctx)
	if err != nil {
		return err
	}
	for _, text := range texts {
		if err := r.loop.appendEvent(session.EventUserMessage, map[string]any{"text": text}); err != nil {
			return err
		}
		r.messages = append(r.messages, llm.Message{Role: llm.RoleUser, Content: text})
	}
	return nil
}

// askModel 取一次模型回复：装了流式回调就走流式端点（宿主实时渲染），否则走非流式
// ——只实现 Chat 的测试替身靠这条工作，行为与纯非流式一致。
func (r *round) askModel() (llm.Result, error) {
	options := []llm.Option{llm.WithTools(r.loop.tools.Describe())}
	observe := r.loop.streamObserver
	if observe == nil {
		return modelCall(r.ctx, func() (llm.Result, error) {
			return r.loop.llm.Chat(r.ctx, r.messages, options...)
		})
	}
	return modelCall(r.ctx, func() (llm.Result, error) {
		return llm.StreamChatResult(r.ctx, r.loop.llm, r.messages,
			append(options, llm.WithObserver(observe))...)
	})
}

// appendToolRound 把这一步的工具调用与结果写进消息序列与会话。
func (r *round) appendToolRound(result llm.Result) error {
	r.messages = append(r.messages, llm.Message{
		Role:      llm.RoleAssistant,
		Content:   result.Content,
		ToolCalls: result.ToolCalls,
	})
	if err := r.loop.appendEvent(session.EventAssistantMessage, map[string]any{
		"text":      result.Content,
		"reasoning": result.Reasoning,
		"toolCalls": ToolCallsToJSON(result.ToolCalls),
	}); err != nil {
		return err
	}
	return r.runCalls(result.ToolCalls)
}

// runCalls 依次执行这一步请求的全部工具调用。
func (r *round) runCalls(calls []llm.ToolCall) error {
	for _, call := range calls {
		if err := r.runCall(call); err != nil {
			return err
		}
	}
	return nil
}

// runCall 执行一次工具调用并回填结果；工具失败也照常回填，交给模型自己看。
//
// 取消后结果一律丢弃：既不回填也不写事件——否则模型会看到「工具失败：context canceled」，
// 把一次取消误当成工具坏掉。
func (r *round) runCall(call llm.ToolCall) error {
	outcome := r.loop.tools.Call(r.ctx, tools.ToolCall{
		CallID:    call.ID,
		Name:      call.Name,
		Arguments: ParseToolArguments(call.Arguments),
	})
	if r.ctx.Err() != nil {
		return ErrCancelled
	}
	r.messages = append(r.messages, llm.Message{
		Role:       llm.RoleTool,
		Content:    outcome.Content,
		ToolCallID: call.ID,
	})
	r.steps = append(r.steps, Step{Call: call, Result: outcome})
	return r.loop.appendEvent(session.EventToolResult, map[string]any{
		"callId":  call.ID,
		"name":    call.Name,
		"content": outcome.Content,
		"isError": outcome.IsError,
	})
}

// finish 收口：通报、把回复写进消息与会话，给出本轮结局。
func (r *round) finish(reply string) (Turn, error) {
	r.loop.observe("agent.finished", map[string]any{
		"replyLength": len(reply),
		"steps":       len(r.steps),
	})
	r.messages = append(r.messages, llm.Message{Role: llm.RoleAssistant, Content: reply})
	if err := r.loop.appendEvent(session.EventAssistantMessage, map[string]any{"text": reply}); err != nil {
		return Turn{}, err
	}
	return Turn{Reply: reply, Steps: r.steps, Messages: r.messages, Usage: r.usage}, nil
}
