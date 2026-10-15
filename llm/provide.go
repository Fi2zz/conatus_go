package llm

import (
	"errors"

	"github.com/Fi2zz/conatus_go/core"
)

// ServiceKey 是 llm 服务在上下文中的服务键。
const ServiceKey = "llm"

// Provide 把 LLM 提供商提供到上下文（服务键 [ServiceKey]）。
//
// 具体提供商（豆包 / DeepSeek）与回退链的装配在调用方：本包只提供 `OpenAI` 兼容实现
// 与 `Fallback` / `Retrying` 两个装饰器，装配方式由宿主决定。
//
// 上下文释放时关闭提供商（实现了 [Closer] 才真正关闭）。
func Provide(ctx *core.Context, provider Provider) (Provider, error) {
	if provider == nil {
		return nil, errors.New("llm: Provide 收到 nil 提供商")
	}
	if _, err := ctx.Provide(ServiceKey, provider); err != nil {
		return nil, err
	}
	ctx.OnDispose(core.Ignore(func() { _ = Close(provider) }))
	return provider, nil
}

// From 取当前上下文可见的 LLM 提供商；未提供时返回错误。
func From(ctx *core.Context) (Provider, error) {
	return core.Require[Provider](ctx, ServiceKey)
}
