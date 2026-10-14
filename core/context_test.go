package core_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
)

func TestContextTrackRegistersDisposer(t *testing.T) {
	ctx := core.NewRoot("root")
	disposed := false
	ctx.Track(core.Ignore(func() { disposed = true }))

	if disposed {
		t.Fatal("释放前不应执行撤销")
	}
	ctx.Dispose()
	if !disposed {
		t.Fatal("释放后撤销应已执行")
	}
}

func TestContextOnDisposeIsAliasOfTrack(t *testing.T) {
	ctx := core.NewRoot("root")
	disposed := false
	ctx.OnDispose(core.Ignore(func() { disposed = true }))

	ctx.Dispose()
	if !disposed {
		t.Fatal("onDispose 应与 track 等价")
	}
}

func TestContextEffectTracksReturnedDisposer(t *testing.T) {
	ctx := core.NewRoot("root")
	disposed := false
	ctx.Effect(func() core.Disposer {
		return core.Ignore(func() { disposed = true })
	})

	ctx.Dispose()
	if !disposed {
		t.Fatal("effect 应自动登记返回值")
	}
}

func TestContextEffectAfterDisposeRunsImmediately(t *testing.T) {
	ctx := core.NewRoot("root")
	ctx.Dispose()

	disposed := false
	ctx.Effect(func() core.Disposer {
		return core.Ignore(func() { disposed = true })
	})

	if !disposed {
		t.Fatal("释放后 effect 的撤销函数应被立即执行")
	}
}

func TestPluginReturnsChildContext(t *testing.T) {
	root := core.NewRoot("root")
	t.Cleanup(root.Dispose)

	child, err := root.Plugin("a", func(*core.Context) error { return nil })
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	if child.Parent() != root {
		t.Fatal("子上下文的父指针应指向宿主")
	}
	if !strings.Contains(child.Name(), "a") {
		t.Fatalf("子上下文名 %q 应含插件名 a", child.Name())
	}
	if child.Name() != "Context(root)/a" {
		t.Fatalf("子上下文名 = %q，期望 Context(root)/a（与 Dart 版逐字节一致）", child.Name())
	}
	if child.String() != "Context(Context(root)/a)" {
		t.Fatalf("子上下文 String = %q，期望 Context(Context(root)/a)", child.String())
	}
}

func TestUnloadingChildLeavesParentIntact(t *testing.T) {
	root := core.NewRoot("root")
	t.Cleanup(root.Dispose)
	if _, err := root.Provide("root-service", 1); err != nil {
		t.Fatalf("provide 失败：%v", err)
	}

	child, err := root.Plugin("child", func(ctx *core.Context) error {
		_, provideErr := ctx.Provide("child-service", 2)
		return provideErr
	})
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	child.Dispose()

	if !child.Disposed() {
		t.Fatal("子上下文应已释放")
	}
	if root.Disposed() {
		t.Fatal("父上下文不应被连带释放")
	}
	if !root.Has("root-service") {
		t.Fatal("父上下文服务应仍在")
	}
	if root.Has("child-service") {
		t.Fatal("子上下文服务不应出现在父级")
	}
}

func TestDisposingParentCascadesToChild(t *testing.T) {
	root := core.NewRoot("root")
	child, err := root.Plugin("child", func(*core.Context) error { return nil })
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	root.Dispose()

	if !child.Disposed() {
		t.Fatal("父上下文释放应级联释放子上下文")
	}
}

func TestPluginRollsBackOnInstallError(t *testing.T) {
	root := core.NewRoot("root")
	t.Cleanup(root.Dispose)
	boom := errors.New("install failed")

	child, err := root.Plugin("bad", func(ctx *core.Context) error {
		if _, provideErr := ctx.Provide("temp", 1); provideErr != nil {
			return provideErr
		}
		return boom
	})

	if !errors.Is(err, boom) {
		t.Fatalf("install 错误应原样返回，得到 %v", err)
	}
	if child != nil {
		t.Fatal("失败的 plugin 不应返回子上下文")
	}
	if root.Has("temp") {
		t.Fatal("install 失败后子上下文应被回滚")
	}
}

func TestDisposingParentCascadesAllDescendantDisposers(t *testing.T) {
	root := core.NewRoot("root")
	var log []string

	if _, err := root.Plugin("a", func(a *core.Context) error {
		a.OnDispose(core.Ignore(func() { log = append(log, "a") }))
		_, err := a.Plugin("b", func(b *core.Context) error {
			b.OnDispose(core.Ignore(func() { log = append(log, "b") }))
			_, pluginErr := b.Plugin("c", func(c *core.Context) error {
				c.OnDispose(core.Ignore(func() { log = append(log, "c") }))
				return nil
			})
			return pluginErr
		})
		return err
	}); err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	root.Dispose()

	for _, name := range []string{"a", "b", "c"} {
		if !contains(log, name) {
			t.Fatalf("级联撤销缺 %q：%v", name, log)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}
