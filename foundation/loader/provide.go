package loader

import "github.com/Fi2zz/conatus_go/core"

// ProvideOption 定制 [Provide]。
type ProvideOption func(*provideSettings)

type provideSettings struct {
	plugins map[string]PluginFactory
	config  []LoaderEntry
}

// WithPlugins 预注册插件工厂（nil 表示空表）。
func WithPlugins(plugins map[string]PluginFactory) ProvideOption {
	return func(s *provideSettings) { s.plugins = plugins }
}

// WithConfig 提供后立即施加配置树。
func WithConfig(entries []LoaderEntry) ProvideOption {
	return func(s *provideSettings) { s.config = entries }
}

// Provide 把装载器提供到上下文（服务键 [ServiceKey]），可选预注册插件并立即施加配置。
//
// 返回装载器便于直接操作。配置加载失败时返回已经提供到上下文的那一个：Dart 侧同样
// 会留下半装载的 loader，调用方要么 [Loader.Remove] 收拾，要么释放上下文兜底。
func Provide(ctx *core.Context, opts ...ProvideOption) (*Loader, error) {
	settings := provideSettings{}
	for _, opt := range opts {
		opt(&settings)
	}
	instance := NewLoader(ctx, settings.plugins)
	if _, err := ctx.Provide(ServiceKey, instance); err != nil {
		return nil, err
	}
	if settings.config != nil {
		if err := instance.Apply(settings.config); err != nil {
			return instance, err
		}
	}
	return instance, nil
}
