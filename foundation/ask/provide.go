package ask

import "github.com/Fi2zz/conatus_go/core"

// ProvideOption 定制 [Provide]。
type ProvideOption func(*provideSettings)

type provideSettings struct {
	askUser AskUser
}

// WithAskUser 提供现成提问器（缺省新建命令行提问器）。
func WithAskUser(askUser AskUser) ProvideOption {
	return func(s *provideSettings) { s.askUser = askUser }
}

// Provide 把提问器提供到上下文（服务键 [ServiceKey]）。
//
// 上下文释放时取消所有等待中的提问：等用户输入可能长达数分钟，宿主上下文被释放
// （用户中断整轮对话）时不能让提问悬挂。取消动作登记为可逆效应。
func Provide(ctx *core.Context, opts ...ProvideOption) (AskUser, error) {
	settings := provideSettings{}
	for _, opt := range opts {
		opt(&settings)
	}
	instance := settings.askUser
	if instance == nil {
		instance = NewCLIAskUser()
	}
	if _, err := ctx.Provide(ServiceKey, instance); err != nil {
		return nil, err
	}
	ctx.OnDispose(core.Ignore(instance.Cancel))
	return instance, nil
}

// From 取当前上下文可见的提问器；未提供时返回错误。
func From(ctx *core.Context) (AskUser, error) {
	return core.Require[AskUser](ctx, ServiceKey)
}
