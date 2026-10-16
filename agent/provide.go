package agent

import (
	"github.com/Fi2zz/conatus_go/core"
	"github.com/Fi2zz/conatus_go/foundation/prompt"
	"github.com/Fi2zz/conatus_go/foundation/tools"
	"github.com/Fi2zz/conatus_go/llm"
)

// ServiceKey 是 Agent 主循环在上下文中的服务键（与 Dart 的 `'agentLoop'` 一致）。
const ServiceKey = "agentLoop"

// Provide 把 Agent 主循环提供到上下文（服务键 [ServiceKey]），返回该循环。
//
// 依赖上下文里的 `llm` 与 `tools` 服务（缺一即报错）；`systemPrompt` 存在时自动接入。
// 会话、步数上限、流式回调等用 [LoopOption] 显式给——显式选项排在自动接入之后，可覆盖它。
//
// 与 Dart 的差异：Dart 那里还能传现成的 loop、按「唯一打开的会话」自动选会话，并在有
// 会话日志记录器时把 recorder 挂上去。这些依赖会话存储与 sessionlog 插件（Go 侧还没
// 落地），所以这里只做「从上下文取依赖 + 装配」，不做推断与后置挂载。
func Provide(ctx *core.Context, opts ...LoopOption) (*Loop, error) {
	provider, err := llm.From(ctx)
	if err != nil {
		return nil, err
	}
	registry, err := tools.From(ctx)
	if err != nil {
		return nil, err
	}
	loop := NewLoop(provider, registry, append(automaticOptions(ctx), opts...)...)
	if _, err := ctx.Provide(ServiceKey, loop); err != nil {
		return nil, err
	}
	return loop, nil
}

// From 取当前上下文可见的 Agent 主循环；未提供时返回错误。
func From(ctx *core.Context) (*Loop, error) {
	return core.Require[*Loop](ctx, ServiceKey)
}

// automaticOptions 按上下文里已就绪的能力补装配。
func automaticOptions(ctx *core.Context) []LoopOption {
	registry, err := prompt.From(ctx)
	if err != nil {
		return nil
	}
	return []LoopOption{WithPrompt(registry)}
}
