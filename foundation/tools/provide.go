package tools

import (
	"time"

	"github.com/Fi2zz/conatus_go/core"
)

// ProvideOption 定制 [Provide]。
type ProvideOption func(*provideSettings)

type provideSettings struct {
	registry *Registry
	timeout  time.Duration
}

// WithRegistry 提供现成注册表（缺省新建）。
func WithRegistry(registry *Registry) ProvideOption {
	return func(s *provideSettings) { s.registry = registry }
}

// WithRegistryTimeout 设定新建注册表的缺省超时。
func WithRegistryTimeout(timeout time.Duration) ProvideOption {
	return func(s *provideSettings) { s.timeout = timeout }
}

// Provide 把工具注册表提供到上下文（服务键 [ServiceKey]），返回该注册表。
//
// 注册的工具面向模型只暴露白名单 schema；登记本身返回 Disposer，交给
// `ctx.Track(...)` 即可随上下文卸载自动撤销。
func Provide(ctx *core.Context, opts ...ProvideOption) (*Registry, error) {
	settings := provideSettings{}
	for _, opt := range opts {
		opt(&settings)
	}
	registry := settings.registry
	if registry == nil {
		registry = NewRegistry(WithDefaultTimeout(settings.timeout))
	}
	if _, err := ctx.Provide(ServiceKey, registry); err != nil {
		return nil, err
	}
	return registry, nil
}

// From 取当前上下文可见的工具注册表；未提供时返回错误
// （对应 Dart 版 `ctx.tools` 抛 StateError）。
func From(ctx *core.Context) (*Registry, error) {
	return core.Require[*Registry](ctx, ServiceKey)
}
