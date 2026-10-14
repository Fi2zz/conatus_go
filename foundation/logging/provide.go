package logging

import "github.com/Fi2zz/conatus_go/core"

type provideSettings struct {
	service  *Service
	minLevel Level
	console  bool
	writer   Writer
	showTime bool
}

// ProvideOption 定制 [Provide]。
type ProvideOption func(*provideSettings)

// WithLogger 提供现成服务（缺省按 ctx 名新建）。
func WithLogger(service *Service) ProvideOption {
	return func(s *provideSettings) { s.service = service }
}

// WithLoggerMinLevel 设定新建服务的最小级别（对现成服务无效）。
func WithLoggerMinLevel(level Level) ProvideOption {
	return func(s *provideSettings) { s.minLevel = level }
}

// WithoutConsole 不挂控制台导出器。
func WithoutConsole() ProvideOption {
	return func(s *provideSettings) { s.console = false }
}

// WithConsoleWriter 指定控制台写入口。
func WithConsoleWriter(writer Writer) ProvideOption {
	return func(s *provideSettings) { s.writer = writer }
}

// WithConsoleTime 在控制台行首输出 ISO 时间。
func WithConsoleTime(show bool) ProvideOption {
	return func(s *provideSettings) { s.showTime = show }
}

// Provide 把日志服务提供到上下文，并按需挂上控制台导出器。
//
// 返回该服务，便于直接取得命名 logger。上下文释放时移除本插件添加的导出器。
func Provide(ctx *core.Context, opts ...ProvideOption) (*Service, error) {
	settings := provideSettings{minLevel: LevelInfo, console: true}
	for _, opt := range opts {
		opt(&settings)
	}
	service := settings.service
	if service == nil {
		service = NewService(WithDefaultName(ctx.Name()), WithMinLevel(settings.minLevel))
	}
	if _, err := ctx.Provide(ServiceKey, service); err != nil {
		return nil, err
	}
	if settings.console {
		attachConsole(ctx, service, settings)
	}
	return service, nil
}

func attachConsole(ctx *core.Context, service *Service, settings provideSettings) {
	options := []ConsoleOption{WithShowTime(settings.showTime)}
	if settings.writer != nil {
		options = append(options, WithWriter(settings.writer))
	}
	exporter := NewConsoleExporter(options...)
	service.AddExporter(exporter)
	ctx.OnDispose(core.Ignore(func() { service.RemoveExporter(exporter) }))
}
