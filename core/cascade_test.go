package core_test

import (
	"reflect"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
)

func TestUnloadingProviderPluginDeactivatesDependent(t *testing.T) {
	root := core.NewRoot("root")
	t.Cleanup(root.Dispose)
	container, err := root.Plugin("container", func(*core.Context) error { return nil })
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	provider, err := container.Plugin("provider", func(ctx *core.Context) error {
		_, provideErr := ctx.Provide("service", 42)
		return provideErr
	})
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	if container.Has("service") {
		t.Fatal("服务在 provider 上下文内提供，对 container 不可见——这是设计预期")
	}
	if !provider.Has("service") {
		t.Fatal("provider 自己应看得到服务")
	}

	deactivated := false
	provider.Inject([]string{"service"}, func(child *core.Context) error {
		child.OnDispose(core.Ignore(func() { deactivated = true }))
		return nil
	})

	provider.Dispose()

	if !deactivated {
		t.Fatal("卸载服务提供者应级联停用依赖者")
	}
}

func TestDisposingParentPluginDeactivatesChildInject(t *testing.T) {
	root := core.NewRoot("root")
	t.Cleanup(root.Dispose)

	deactivated := false
	node, err := root.Plugin("A", func(ctx *core.Context) error {
		if _, provideErr := ctx.Provide("service", 42); provideErr != nil {
			return provideErr
		}
		_, pluginErr := ctx.Plugin("B", func(child *core.Context) error {
			child.Inject([]string{"service"}, func(grand *core.Context) error {
				grand.OnDispose(core.Ignore(func() { deactivated = true }))
				return nil
			})
			return nil
		})
		return pluginErr
	})
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	node.Dispose()

	if !deactivated {
		t.Fatal("父上下文卸载应导致子上下文的 inject 停用")
	}
}

func TestTwoLevelCascadeRemovesDerivedService(t *testing.T) {
	root := core.NewRoot("root")
	t.Cleanup(root.Dispose)
	var log []string

	node, err := root.Plugin("A", func(ctx *core.Context) error {
		_, provideErr := ctx.Provide("x", 1)
		return provideErr
	})
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	if _, err := node.Plugin("B", func(ctx *core.Context) error {
		ctx.Inject([]string{"x"}, func(child *core.Context) error {
			log = append(log, "B.activate")
			if _, provideErr := child.Provide("y", 2); provideErr != nil {
				return provideErr
			}
			child.OnDispose(core.Ignore(func() { log = append(log, "B.deactivate") }))
			return nil
		})
		return nil
	}); err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	if want := []string{"B.activate"}; !reflect.DeepEqual(log, want) {
		t.Fatalf("激活日志 = %v，期望 %v", log, want)
	}

	node.Dispose()

	if want := []string{"B.activate", "B.deactivate"}; !reflect.DeepEqual(log, want) {
		t.Fatalf("级联日志 = %v，期望 %v（x 消失 → B 停用 → B 提供的 y 随之消失）", log, want)
	}
	if root.Has("x") {
		t.Fatal("x 只应存在于 A 子树内")
	}
}

func TestDependencyReappearanceReactivatesWithNewContext(t *testing.T) {
	root := core.NewRoot("root")
	t.Cleanup(root.Dispose)
	var log []string
	stop, err := root.Provide("dep", 1)
	if err != nil {
		t.Fatalf("provide 失败：%v", err)
	}

	root.Inject([]string{"dep"}, func(child *core.Context) error {
		log = append(log, "activate")
		child.OnDispose(core.Ignore(func() { log = append(log, "deactivate") }))
		return nil
	})

	if err := stop(); err != nil {
		t.Fatalf("撤销服务失败：%v", err)
	}
	if want := []string{"activate", "deactivate"}; !reflect.DeepEqual(log, want) {
		t.Fatalf("停用日志 = %v，期望 %v", log, want)
	}

	mustProvide(t, root, "dep", 2)

	if want := []string{"activate", "deactivate", "activate"}; !reflect.DeepEqual(log, want) {
		t.Fatalf("重新激活日志 = %v，期望 %v", log, want)
	}
}
