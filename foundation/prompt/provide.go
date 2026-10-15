package prompt

import "github.com/Fi2zz/conatus_go/core"

// ServiceKey 是 prompt 注册表在上下文中的服务键。
const ServiceKey = "systemPrompt"

// ProvideOption 定制 [Provide]。
type ProvideOption func(*provideSettings)

type provideSettings struct {
	registry *Registry
}

// WithRegistry 提供现成注册表（缺省新建）。
func WithRegistry(registry *Registry) ProvideOption {
	return func(s *provideSettings) { s.registry = registry }
}

// Provide 把 prompt 注册表提供到上下文（服务键 [ServiceKey]），返回该注册表。
func Provide(ctx *core.Context, opts ...ProvideOption) (*Registry, error) {
	settings := provideSettings{}
	for _, opt := range opts {
		opt(&settings)
	}
	registry := settings.registry
	if registry == nil {
		registry = NewRegistry()
	}
	if _, err := ctx.Provide(ServiceKey, registry); err != nil {
		return nil, err
	}
	return registry, nil
}

// From 取当前上下文可见的 prompt 注册表；未提供时返回错误。
func From(ctx *core.Context) (*Registry, error) {
	return core.Require[*Registry](ctx, ServiceKey)
}
