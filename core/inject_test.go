package core_test

import (
	"testing"

	"github.com/Fi2zz/conatus_go/core"
)

func TestInjectActivatesWhenDependencyReady(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	mustProvide(t, ctx, "logger", "L")

	activated := false
	ctx.Inject([]string{"logger"}, func(*core.Context) error {
		activated = true
		return nil
	})

	if !activated {
		t.Fatal("依赖满足时应立即激活")
	}
}

func TestInjectDoesNotActivateWhenDependencyMissing(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)

	activated := false
	ctx.Inject([]string{"logger"}, func(*core.Context) error {
		activated = true
		return nil
	})

	if activated {
		t.Fatal("依赖不满足时不应激活")
	}
}

func TestInjectActivatesWhenDependencyArrivesLate(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)

	activated := false
	ctx.Inject([]string{"logger"}, func(*core.Context) error {
		activated = true
		return nil
	})
	if activated {
		t.Fatal("依赖未到之前不应激活")
	}

	mustProvide(t, ctx, "logger", "L")

	if !activated {
		t.Fatal("依赖后到时应激活")
	}
}

func TestInjectDeactivatesWhenDependencyDisappears(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	stop := mustProvide(t, ctx, "logger", "L")

	activated, deactivated := false, false
	ctx.Inject([]string{"logger"}, func(child *core.Context) error {
		activated = true
		child.OnDispose(core.Ignore(func() { deactivated = true }))
		return nil
	})

	if !activated {
		t.Fatal("依赖满足时应激活")
	}
	if deactivated {
		t.Fatal("停用不应提前发生")
	}

	if err := stop(); err != nil {
		t.Fatalf("撤销服务失败：%v", err)
	}

	if !deactivated {
		t.Fatal("依赖消失时应停用，且回调中的效应被撤销")
	}
}

func TestInjectReactivationCreatesFreshChildContext(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	stop := mustProvide(t, ctx, "logger", "L")

	var contexts []*core.Context
	ctx.Inject([]string{"logger"}, func(child *core.Context) error {
		contexts = append(contexts, child)
		return nil
	})

	if err := stop(); err != nil {
		t.Fatalf("撤销服务失败：%v", err)
	}
	mustProvide(t, ctx, "logger", "L2")

	if len(contexts) != 2 {
		t.Fatalf("激活次数 = %d，期望 2", len(contexts))
	}
	if contexts[0] == contexts[1] {
		t.Fatal("重新激活必须创建全新的子上下文")
	}
}

func TestInjectRequiresAllDependencies(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	mustProvide(t, ctx, "a", 1)

	activated := false
	ctx.Inject([]string{"a", "b"}, func(*core.Context) error {
		activated = true
		return nil
	})
	if activated {
		t.Fatal("多依赖未全部满足时不应激活")
	}

	mustProvide(t, ctx, "b", 2)

	if !activated {
		t.Fatal("依赖全部满足后应激活")
	}
}

func TestInjectReturnsCancelDisposer(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	mustProvide(t, ctx, "logger", "L")

	deactivated := false
	cancel := ctx.Inject([]string{"logger"}, func(child *core.Context) error {
		child.OnDispose(core.Ignore(func() { deactivated = true }))
		return nil
	})

	if err := cancel(); err != nil {
		t.Fatalf("取消注入失败：%v", err)
	}
	if !deactivated {
		t.Fatal("取消注入应停用已激活的子上下文")
	}
	if !ctx.Has("logger") {
		t.Fatal("取消注入不应影响被依赖的服务本身")
	}
	if err := cancel(); err != nil {
		t.Fatalf("重复取消应幂等且不报错：%v", err)
	}
}

func TestHostDisposeCancelsInjection(t *testing.T) {
	root := core.NewRoot("root")
	mustProvide(t, root, "logger", "L")

	deactivated := false
	plugin, err := root.Plugin("p", func(ctx *core.Context) error {
		ctx.Inject([]string{"logger"}, func(child *core.Context) error {
			child.OnDispose(core.Ignore(func() { deactivated = true }))
			return nil
		})
		return nil
	})
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	plugin.Dispose()

	if !deactivated {
		t.Fatal("宿主上下文释放应撤销所有注入")
	}
	root.Dispose()
}
