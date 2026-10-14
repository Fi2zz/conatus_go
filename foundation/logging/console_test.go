package logging_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
	"github.com/Fi2zz/conatus_go/foundation/logging"
)

func TestConsoleExporterFormatsAndFiltersByOwnLevel(t *testing.T) {
	lines := []string{}
	service := logging.NewService()
	service.AddExporter(logging.NewConsoleExporter(
		logging.WithWriter(func(line string) { lines = append(lines, line) }),
		logging.WithExporterMinLevel(logging.LevelInfo),
	))

	service.Debug("d")
	service.Info("hi")
	service.Error("boom", logging.WithError(errors.New("x")))

	if len(lines) != 2 {
		t.Fatalf("输出行数 = %d，期望 2：%v", len(lines), lines)
	}
	if lines[0] != "[I] root  hi" {
		t.Fatalf("首行 = %q，期望 [I] root  hi", lines[0])
	}
	if !strings.HasPrefix(lines[1], "[E] root  boom") {
		t.Fatalf("次行 = %q，应以 [E] root  boom 开头", lines[1])
	}
	if !strings.HasSuffix(lines[1], "x") {
		t.Fatalf("次行 = %q，错误信息应被追加", lines[1])
	}
}

func TestConsoleExporterShowTimePrefixesISOTime(t *testing.T) {
	lines := []string{}
	service := logging.NewService()
	service.AddExporter(logging.NewConsoleExporter(
		logging.WithWriter(func(line string) { lines = append(lines, line) }),
		logging.WithShowTime(true),
	))

	service.Info("hi")

	pattern := regexp.MustCompile(`^\S+ \[I\] root  hi$`)
	if len(lines) != 1 || !pattern.MatchString(lines[0]) {
		t.Fatalf("输出 = %v，期望匹配 %s", lines, pattern)
	}
}

func TestConsoleExporterWritesStackAsItsOwnLine(t *testing.T) {
	lines := []string{}
	service := logging.NewService()
	service.AddExporter(logging.NewConsoleExporter(
		logging.WithWriter(func(line string) { lines = append(lines, line) }),
	))

	service.Error("boom", logging.WithStack([]byte("stack-line")))

	if len(lines) != 2 || lines[1] != "stack-line" {
		t.Fatalf("输出 = %v，期望堆栈另起一行", lines)
	}
}

func TestProvideRegistersServiceAndDetachesExporterOnDispose(t *testing.T) {
	ctx := core.NewRoot("root")
	service, err := logging.Provide(ctx)
	if err != nil {
		t.Fatalf("provide 失败：%v", err)
	}

	if !ctx.Has(logging.ServiceKey) {
		t.Fatal("服务应已注册")
	}
	provided, found := core.Get[*logging.Service](ctx, logging.ServiceKey)
	if !found || provided != service {
		t.Fatal("取回的服务应是同一个实例")
	}
	if len(service.Exporters()) != 1 {
		t.Fatalf("导出器数 = %d，期望 1", len(service.Exporters()))
	}

	ctx.Dispose()

	if len(service.Exporters()) != 0 {
		t.Fatalf("上下文释放后导出器应被移除，仍有 %d 个", len(service.Exporters()))
	}
}

func TestProvideWithoutConsoleLeavesNoExporter(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)

	service, err := logging.Provide(ctx, logging.WithoutConsole())
	if err != nil {
		t.Fatalf("provide 失败：%v", err)
	}

	if len(service.Exporters()) != 0 {
		t.Fatalf("导出器数 = %d，期望 0", len(service.Exporters()))
	}
}

func TestProvideUsesGivenService(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	existing := logging.NewService(logging.WithDefaultName("given"))

	service, err := logging.Provide(ctx, logging.WithLogger(existing), logging.WithoutConsole())
	if err != nil {
		t.Fatalf("provide 失败：%v", err)
	}

	if service != existing {
		t.Fatal("应提供传入的服务实例")
	}
}

func TestProvideNamesNewServiceAfterContext(t *testing.T) {
	ctx := core.NewRoot("app")
	t.Cleanup(ctx.Dispose)

	service, err := logging.Provide(ctx, logging.WithoutConsole())
	if err != nil {
		t.Fatalf("provide 失败：%v", err)
	}

	if service.DefaultName() != "app" {
		t.Fatalf("缺省名字 = %q，期望 app（取自上下文名）", service.DefaultName())
	}
}
