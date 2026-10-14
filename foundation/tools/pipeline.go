package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CallOption 定制一次调用。
type CallOption func(*callSettings)

type callSettings struct {
	timeout    time.Duration
	hasTimeout bool
}

// WithCallTimeout 覆盖工具自身声明的超时（后者又覆盖注册表缺省）。
func WithCallTimeout(timeout time.Duration) CallOption {
	return func(s *callSettings) {
		s.timeout = timeout
		s.hasTimeout = true
	}
}

// Call 调用一个工具：校验参数 → 守卫 → 中间件链 → 执行体 → 广播结果。
//
// 参数不合法（INVALID_ARGS）、超时（TOOL_TIMEOUT）、未知工具（UNKNOWN_TOOL）、
// 守卫拒绝（TOOL_DENIED）与执行体失败（TOOL_ERROR）都返回**失败结果**而不是
// error：调用方（Agent Loop）总是要接着往下走。
func (r *Registry) Call(ctx context.Context, call ToolCall, opts ...CallOption) ToolResult {
	settings := callSettings{}
	for _, opt := range opts {
		opt(&settings)
	}
	result := r.dispatch(ctx, call, settings)
	for _, listener := range r.results.snapshot() {
		listener(call, result)
	}
	return result
}

func (r *Registry) dispatch(ctx context.Context, call ToolCall, settings callSettings) ToolResult {
	tool, denial := r.preflight(call)
	if denial != nil {
		return *denial
	}
	result, err := r.chain(tool, call, r.effectiveTimeout(tool, settings))(ctx)
	if err != nil {
		return Failure(err.Error(), ToolError{Code: "TOOL_ERROR", Message: err.Error()})
	}
	return result
}

// preflight 依次查工具、校验参数、过守卫；任一步失败即返回失败结果。
func (r *Registry) preflight(call ToolCall) (Tool, *ToolResult) {
	tool, found := r.Get(call.Name)
	if !found {
		return nil, failurePtr(
			fmt.Sprintf("未知工具 %q", call.Name),
			"UNKNOWN_TOOL", fmt.Sprintf("unknown tool %q", call.Name))
	}
	if violations := ValidateArgs(tool, call.Arguments); len(violations) > 0 {
		return nil, failurePtr(
			"参数不合法："+strings.Join(violations, "；"),
			"INVALID_ARGS", strings.Join(violations, "; "))
	}
	if denial := r.runGuards(call); denial != "" {
		return nil, failurePtr(denial, "TOOL_DENIED", denial)
	}
	return tool, nil
}

func failurePtr(content, code, message string) *ToolResult {
	result := Failure(content, ToolError{Code: code, Message: message})
	return &result
}

func (r *Registry) runGuards(call ToolCall) string {
	for _, guard := range r.guards.snapshot() {
		if denial := guard(call); denial != "" {
			return denial
		}
	}
	return ""
}

// effectiveTimeout 解析本次调用的超时：调用参数 > 工具自声明 > 注册表缺省。
func (r *Registry) effectiveTimeout(tool Tool, settings callSettings) time.Duration {
	if settings.hasTimeout {
		return settings.timeout
	}
	if declared := TimeoutOf(tool); declared > 0 {
		return declared
	}
	return r.DefaultTimeout()
}

// chain 组装中间件链：**先登记的中间件在最外层**（与 Dart 版一致）。
func (r *Registry) chain(tool Tool, call ToolCall, timeout time.Duration) Next {
	next := r.body(tool, call, timeout)
	middlewares := r.middlewares.snapshot()
	for index := len(middlewares) - 1; index >= 0; index-- {
		middleware := middlewares[index].fn
		previous := next
		next = func(ctx context.Context) (ToolResult, error) {
			return middleware(ctx, call, previous)
		}
	}
	return next
}

// body 是链的最内层：**只有它**受工具超时约束。
//
// 超时不是「取消工具」而是「丢弃迟到结果」，与 Dart 的 `Future.timeout` 一致：工具
// 若无视 ctx 会继续在后台跑，结果被丢弃；带缓冲的结果通道保证那个 goroutine 不会
// 泄漏（写入缓冲后自行退出）。
func (r *Registry) body(tool Tool, call ToolCall, timeout time.Duration) Next {
	return func(ctx context.Context) (ToolResult, error) {
		toolContext := NewToolContext(call)
		if timeout <= 0 {
			return tool.Call(ctx, toolContext)
		}
		bounded, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		outcomes := make(chan toolOutcome, 1)
		go func() {
			result, err := tool.Call(bounded, toolContext)
			outcomes <- toolOutcome{result: result, err: err}
		}()
		select {
		case outcome := <-outcomes:
			return outcome.result, outcome.err
		case <-bounded.Done():
			return timeoutOutcome(call.Name, timeout, bounded.Err())
		}
	}
}

// timeoutOutcome 区分「超时」与「外层取消」：前者是工具预算用尽（TOOL_TIMEOUT），
// 后者是调用方主动取消，原样把 context 错误交回上方收敛成 TOOL_ERROR。
func timeoutOutcome(name string, timeout time.Duration, cause error) (ToolResult, error) {
	if errors.Is(cause, context.DeadlineExceeded) {
		return Failure(
			fmt.Sprintf("工具 %q 超时（%dms）", name, timeout.Milliseconds()),
			ToolError{Code: "TOOL_TIMEOUT", Message: name + " timed out"},
		), nil
	}
	return ToolResult{}, cause
}

type toolOutcome struct {
	result ToolResult
	err    error
}
