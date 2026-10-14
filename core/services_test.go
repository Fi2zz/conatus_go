package core_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
)

func TestGetReportsTypeMismatchAsNotFound(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	if _, err := ctx.Provide("num", 42); err != nil {
		t.Fatalf("provide 失败：%v", err)
	}

	value, found := core.Get[string](ctx, "num")
	if found {
		t.Fatal("类型不符时 Get 应返回 false")
	}
	if value != "" {
		t.Fatalf("类型不符时应返回零值，得到 %q", value)
	}
	if number, ok := core.Get[int](ctx, "num"); !ok || number != 42 {
		t.Fatalf("正确类型应取到 (42, true)，得到 (%d, %v)", number, ok)
	}
}

func TestRequireDistinguishesMissingFromMismatch(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	if _, err := ctx.Provide("num", 42); err != nil {
		t.Fatalf("provide 失败：%v", err)
	}

	_, missingErr := core.Require[int](ctx, "absent")
	if missingErr == nil || !strings.Contains(missingErr.Error(), "不可用") {
		t.Fatalf("缺失服务应报「不可用」，得到 %v", missingErr)
	}

	_, mismatchErr := core.Require[string](ctx, "num")
	if mismatchErr == nil || !strings.Contains(mismatchErr.Error(), "类型不符") {
		t.Fatalf("类型不符应报「类型不符」，得到 %v", mismatchErr)
	}
}

func TestRequireFindsInheritedService(t *testing.T) {
	root := core.NewRoot("root")
	t.Cleanup(root.Dispose)
	if _, err := root.Provide("logger", "L"); err != nil {
		t.Fatalf("provide 失败：%v", err)
	}
	child, err := root.Plugin("p", func(*core.Context) error { return nil })
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	logger, requireErr := core.Require[string](child, "logger")
	if requireErr != nil {
		t.Fatalf("子上下文应能 require 到父级服务：%v", requireErr)
	}
	if logger != "L" {
		t.Fatalf("require 结果 = %q，期望 L", logger)
	}
}

func TestServiceReturnsRawValue(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	payload := map[string]int{"a": 1}
	if _, err := ctx.Provide("payload", payload); err != nil {
		t.Fatalf("provide 失败：%v", err)
	}

	value, found := ctx.Service("payload")
	if !found {
		t.Fatal("服务应可见")
	}
	if _, ok := value.(map[string]int); !ok {
		t.Fatalf("Service 应返回原始值，得到 %T", value)
	}
	if _, found := ctx.Service("absent"); found {
		t.Fatal("不存在的服务应返回 false")
	}
}

func TestDisposeReportsDisposerErrorsToSink(t *testing.T) {
	ctx := core.NewRoot("root")
	var reported []error
	ctx.SetErrorHandler(func(err error) { reported = append(reported, err) })

	boom := errors.New("boom")
	ctx.Track(func() error { return boom })
	ctx.Dispose()

	if len(reported) != 1 {
		t.Fatalf("释放错误应上报 1 条，得到 %d 条", len(reported))
	}
	if !errors.Is(reported[0], boom) {
		t.Fatalf("上报内容 = %v，期望 boom", reported[0])
	}
}

func TestDisposeStillClearsServicesAfterFailure(t *testing.T) {
	ctx := core.NewRoot("root")
	ctx.SetErrorHandler(func(error) {})
	if _, err := ctx.Provide("svc", 1); err != nil {
		t.Fatalf("provide 失败：%v", err)
	}
	ctx.Track(func() error { return errors.New("boom") })

	ctx.Dispose()

	if !ctx.Disposed() {
		t.Fatal("上下文应已释放")
	}
	if ctx.Has("svc") {
		t.Fatal("释放后服务不应残留")
	}
	if len(ctx.LocalServiceKeys()) != 0 {
		t.Fatalf("释放后直接服务应为空，得到 %v", ctx.LocalServiceKeys())
	}
}

func TestErrorSinkIsSharedWithDescendants(t *testing.T) {
	root := core.NewRoot("root")
	var reported []error
	root.SetErrorHandler(func(err error) { reported = append(reported, err) })

	child, err := root.Plugin("p", func(c *core.Context) error {
		c.Track(func() error { return errors.New("child boom") })
		return nil
	})
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	child.Dispose()

	if len(reported) != 1 {
		t.Fatalf("子上下文的错误应上报到同一接收器，得到 %d 条", len(reported))
	}
}
