package tools

import (
	"context"
	"time"
)

// Tool 是工具的最小契约：名字、说明、执行体。
//
// 其余元数据（参数声明、风险等级、分组、超时、路径参数）通过**可选接口**声明，
// 未实现即取缺省值——这样简单工具只需要三个方法：
//
//	type EchoTool struct{}
//
//	func (EchoTool) Name() string        { return "echo" }
//	func (EchoTool) Description() string { return "回显输入" }
//	func (EchoTool) Call(ctx context.Context, tc *tools.ToolContext) (tools.ToolResult, error) {
//		text, _ := tc.String("text")
//		return tools.Success(text, tools.WithValue(text)), nil
//	}
//
// 需要参数声明与风险等级时，要么实现下面这些可选接口，要么直接用 [Registry.Fn]
// 一行登记（内部走同一条注册与执行管线）。
type Tool interface {
	Name() string
	Description() string
	Call(ctx context.Context, tc *ToolContext) (ToolResult, error)
}

// 可选接口：实现了就用，没实现取缺省。
type paramDeclarer interface{ Params() []*ParamSpec }
type riskDeclarer interface{ RiskLevel() Risk }
type groupDeclarer interface{ GroupName() string }
type timeoutDeclarer interface{ Timeout() time.Duration }
type pathDeclarer interface{ PathParams() []string }

// ParamsOf 返回工具的参数声明；未实现该可选接口时为空。
func ParamsOf(tool Tool) []*ParamSpec {
	declarer, ok := tool.(paramDeclarer)
	if !ok {
		return nil
	}
	return declarer.Params()
}

// RiskOf 返回工具风险等级；未声明时为 [RiskLow]。
func RiskOf(tool Tool) Risk {
	declarer, ok := tool.(riskDeclarer)
	if !ok {
		return RiskLow
	}
	return declarer.RiskLevel()
}

// GroupOf 返回工具的静态分组；未声明时为空串。
func GroupOf(tool Tool) string {
	declarer, ok := tool.(groupDeclarer)
	if !ok {
		return ""
	}
	return declarer.GroupName()
}

// TimeoutOf 返回工具自声明的超时；未声明时为 0（沿用注册表缺省）。
func TimeoutOf(tool Tool) time.Duration {
	declarer, ok := tool.(timeoutDeclarer)
	if !ok {
		return 0
	}
	return declarer.Timeout()
}

// PathParamsOf 返回工具声明的文件系统路径参数（取值形如 `"path"`，可含 `a.b` 嵌套
// 路径），未声明时为空。
//
// 审批中间件据此把「工具 + 参数路径」作为信任粒度：落在已信任目录内的调用可直接
// 放行，无需再问用户。
func PathParamsOf(tool Tool) []string {
	declarer, ok := tool.(pathDeclarer)
	if !ok {
		return nil
	}
	return declarer.PathParams()
}

// SchemaOf 是面向模型的白名单投影：只含 name / description / parameters。
//
// 宿主字段（风险等级、分组、超时、路径参数、执行体）永不下发模型。
func SchemaOf(tool Tool) map[string]any {
	return map[string]any{
		"name":        tool.Name(),
		"description": tool.Description(),
		"parameters":  ParameterSchema(ParamsOf(tool)),
	}
}
