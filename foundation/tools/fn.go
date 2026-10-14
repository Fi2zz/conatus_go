package tools

import (
	"context"
	"fmt"

	"github.com/Fi2zz/conatus_go/core"
)

// Handler 是函数式工具的执行体。
type Handler func(ctx context.Context, tc *ToolContext) (ToolResult, error)

// FnOption 定制 [Registry.Fn] 登记的工具。
type FnOption func(*fnTool)

// WithFnParams 声明参数（自动生成 JSON Schema 并参与校验）。
func WithFnParams(params ...*ParamSpec) FnOption {
	return func(t *fnTool) { t.params = params }
}

// WithFnRisk 声明风险等级（缺省 [RiskLow]）。
func WithFnRisk(risk Risk) FnOption {
	return func(t *fnTool) { t.risk = risk }
}

// WithFnGroup 声明静态分组（缺省不分组）。
func WithFnGroup(name string) FnOption {
	return func(t *fnTool) { t.group = name }
}

// WithFnPathParams 声明文件系统路径参数（供审批方按「工具 + 目录」授权）。
func WithFnPathParams(names ...string) FnOption {
	return func(t *fnTool) { t.paths = names }
}

// Fn 一行登记一个由 handler 驱动的工具，返回撤销函数（幂等）。
//
// 内部仍以 [Tool] 走同一条注册与执行管线，因此参数校验、守卫、中间件、超时与
// 结果广播全部生效。
func (r *Registry) Fn(name, description string, handler Handler, opts ...FnOption) (core.Disposer, error) {
	if handler == nil {
		return nil, fmt.Errorf("tools: 工具 %q 缺少执行体", name)
	}
	tool := &fnTool{name: name, description: description, handler: handler}
	for _, opt := range opts {
		opt(tool)
	}
	return r.Register(tool)
}

// fnTool 是 [Registry.Fn] 背后的工具实现。
type fnTool struct {
	name        string
	description string
	params      []*ParamSpec
	risk        Risk
	group       string
	paths       []string
	handler     Handler
}

func (t *fnTool) Name() string { return t.name }

func (t *fnTool) Description() string { return t.description }

func (t *fnTool) Params() []*ParamSpec { return t.params }

func (t *fnTool) RiskLevel() Risk { return t.risk }

func (t *fnTool) GroupName() string { return t.group }

func (t *fnTool) PathParams() []string { return t.paths }

func (t *fnTool) Call(ctx context.Context, tc *ToolContext) (ToolResult, error) {
	return t.handler(ctx, tc)
}
