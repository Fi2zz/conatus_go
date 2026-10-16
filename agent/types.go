// Package agent 是 Agent 主循环（对应 Dart 的 conatus_agent）：调模型、跑工具、回填结果，
// 直到收口。
//
// 一轮的流程：
//
//	写 user 事件 → 组装 system + 会话历史 → 取模型回复 →（有工具调用就执行并回填）→ 收口
//
// 历史不另存一份：模型看到的永远是会话事件日志的投影（[DeriveMessages]），因此「会话即
// 真相」，重启后从日志就能重建上下文。
//
// 本包目前只有最小闭环。Dart 侧还有 compaction / memory / reflection / router / plan /
// goal / telemetry / approval / sub-agent 等一堆插件，Go 侧都还没落地，所以这里既没有
// 它们的字段也没有对应分支——补齐时按同名文件加进来，别在注释里写没实现的承诺。
package agent

import (
	"context"

	"github.com/Fi2zz/conatus_go/foundation/tools"
	"github.com/Fi2zz/conatus_go/llm"
)

// SteeringSource 取回并**清空**排队的用户插话（轮内插话）；返回空列表表示没有。
//
// 出错按编程错误处理：向上传播，不吞。
type SteeringSource func(ctx context.Context) ([]string, error)

// Observer 是每轮的观察口（Observability 埋点）：`agent.round` / `agent.finished`。
type Observer func(kind string, data map[string]any)

// TurnTracker 是一轮的生命周期钩子：begin 在写入 user 事件后调用，end 在收口（或失败）
// 后调用，一轮恰好各一次。
type TurnTracker interface {
	// BeginTurn 开始一轮。
	BeginTurn(ctx context.Context, userInput string) error
	// EndTurn 结束一轮；reply 与 cause 互斥（可都为空）。它返回的错误会成为
	// [Loop.Run] 的错误——但仅当这一轮本身没出错时（真因不该被追踪器盖掉）。
	EndTurn(ctx context.Context, reply string, cause error) error
}

// Step 是一次工具调用在循环中的步骤记录。
type Step struct {
	// Call 是模型请求的调用。
	Call llm.ToolCall
	// Result 是工具执行结局。
	Result tools.ToolResult
}

// Turn 是一轮 Agent 循环的结局。
type Turn struct {
	// Reply 是最终文本回复。
	Reply string
	// Steps 是本轮内的工具调用步骤。
	Steps []Step
	// Messages 是本轮结束时的完整消息序列。
	Messages []llm.Message
	// Usage 是本轮每次模型调用的用量（按调用顺序）；无模型调用时为空。
	Usage []map[string]any
}
