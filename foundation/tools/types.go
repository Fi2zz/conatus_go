// Package tools 是工具系统：声明式参数（ParamSpec → JSON Schema）、类型安全的参数
// 取值、注册表与受控执行管线（校验 → 守卫 → 中间件 → 执行体 → 结果广播）。
//
// 这是 Dart 版 conatus_foundation 里 tools 插件的对等移植，三条不变式必须保住：
//
//   - 面向模型只暴露白名单投影（name / description / parameters），宿主字段
//     （风险等级、分组、超时、路径参数、执行体）永不下发；
//   - 未知工具、参数不合法、守卫拒绝、超时、执行体失败一律收敛为**失败结果**，
//     不向外返回 error；
//   - **超时只包住工具体**，不套住中间件——审批之类的中间件在等用户，可以长达
//     数分钟。两者混在一处计时时小的那个静默获胜：用户批准后工具照样执行，但
//     模型已被告知失败，写操作会变成「以为失败而重试，实际写了两遍」。
//
// 与 Dart 版的形态差异：没有 Context 扩展方法（Go 不能给已有类型加方法），
// 因此 `ctx.tools.fn(...)` 对应 [Registry.Fn]、`ctx.tools.group(...)` 对应
// [Groups]、`ctx.tools` 对应 [From]。
package tools

import (
	"context"
	"fmt"
)

// Risk 是工具风险等级：[RiskLow] 只读、[RiskMedium] 有副作用但可回退、
// [RiskHigh] 破坏性或需确认。能力分级与确认门控读取该字段。
type Risk int

const (
	// RiskLow 只读。
	RiskLow Risk = iota
	// RiskMedium 有副作用但可回退。
	RiskMedium
	// RiskHigh 破坏性或需确认。
	RiskHigh
)

var riskNames = [...]string{"low", "medium", "high"}

func (r Risk) String() string {
	if int(r) < 0 || int(r) >= len(riskNames) {
		return fmt.Sprintf("Risk(%d)", int(r))
	}
	return riskNames[r]
}

// ToolCall 是一次工具调用的请求。
//
// Arguments 是解析后的参数对象；CallID 由调用方提供，用于回喂与关联。
type ToolCall struct {
	CallID    string
	Name      string
	Arguments map[string]any
}

// ToolError 是工具失败的结构化信息：Code 是稳定的机器可读错误码，
// Message 是面向模型/用户的可读消息。
type ToolError struct {
	Code    string
	Message string
}

func (e ToolError) Error() string { return e.Code + ": " + e.Message }

// ToolResult 是一次工具调用的结局：成功携带规范值 Value，失败携带 Error。
type ToolResult struct {
	IsError bool
	Content string
	Value   any
	Error   *ToolError
}

// ResultOption 定制成功结果。
type ResultOption func(*ToolResult)

// WithValue 附加规范值（回喂给模型之外的结构化结果）。
func WithValue(value any) ResultOption {
	return func(r *ToolResult) { r.Value = value }
}

// Success 构造成功结果。
func Success(content string, opts ...ResultOption) ToolResult {
	result := ToolResult{Content: content}
	for _, opt := range opts {
		opt(&result)
	}
	return result
}

// Failure 构造失败结果。
func Failure(content string, toolErr ToolError) ToolResult {
	return ToolResult{IsError: true, Content: content, Error: &toolErr}
}

// Guard 是单调守卫：返回非空理由即拒绝该次调用，返回空串放行。
type Guard func(call ToolCall) string

// Next 是中间件链的下一环（含工具执行体）。
type Next func(ctx context.Context) (ToolResult, error)

// Middleware 是环绕执行的中间件。
type Middleware func(ctx context.Context, call ToolCall, next Next) (ToolResult, error)

// ResultListener 是结果观察者。
type ResultListener func(call ToolCall, result ToolResult)
