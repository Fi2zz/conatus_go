package loader_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
	"github.com/Fi2zz/conatus_go/foundation/loader"
)

// recorder 是记录型插件工厂：把「加载 / 卸载」写进日志，用来钉住时序与级联顺序。
func recorder(log *[]string, name string) loader.PluginFactory {
	return func(ctx *core.Context, _ any) error {
		*log = append(*log, name)
		ctx.OnDispose(core.Ignore(func() { *log = append(*log, name+":off") }))
		return nil
	}
}

func noopFactory(*core.Context, any) error { return nil }

func mustProvide(t *testing.T, ctx *core.Context, plugins map[string]loader.PluginFactory) *loader.Loader {
	t.Helper()
	instance, err := loader.Provide(ctx, loader.WithPlugins(plugins))
	if err != nil {
		t.Fatalf("provide 失败：%v", err)
	}
	return instance
}

func TestLoadRunsPluginInChildContextAndRemoveRevokes(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	log := []string{}
	instance := mustProvide(t, ctx, map[string]loader.PluginFactory{
		"a": func(child *core.Context, config any) error {
			log = append(log, "a:"+fmt.Sprint(config))
			child.OnDispose(core.Ignore(func() { log = append(log, "a:off") }))
			return nil
		},
	})

	id, err := instance.Load(loader.LoaderEntry{ID: "a1", Name: "a", Config: "x"})
	if err != nil {
		t.Fatalf("加载失败：%v", err)
	}
	if id != "a1" {
		t.Fatalf("返回 id = %q，期望 a1", id)
	}
	if !reflect.DeepEqual(log, []string{"a:x"}) {
		t.Fatalf("加载日志 = %v，期望 [a:x]", log)
	}
	if ids := instance.IDs(); !reflect.DeepEqual(ids, []string{"a1"}) {
		t.Fatalf("已加载 = %v，期望 [a1]", ids)
	}
	if _, found := instance.ContextOf("a1"); !found {
		t.Fatal("加载后应有子上下文")
	}

	if err := instance.Remove("a1"); err != nil {
		t.Fatalf("卸载失败：%v", err)
	}
	if !reflect.DeepEqual(log, []string{"a:x", "a:off"}) {
		t.Fatalf("卸载日志 = %v，期望 [a:x a:off]", log)
	}
	if len(instance.IDs()) != 0 {
		t.Fatalf("卸载后应为空：%v", instance.IDs())
	}
	if _, found := instance.ContextOf("a1"); found {
		t.Fatal("卸载后不该还有子上下文")
	}
}

func TestLoadUnknownPluginLeavesNoResidue(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	instance := mustProvide(t, ctx, nil)

	_, err := instance.Load(loader.LoaderEntry{ID: "x", Name: "nope"})

	if !errors.Is(err, loader.ErrUnknownPlugin) {
		t.Fatalf("错误 = %v，期望 ErrUnknownPlugin", err)
	}
	if len(instance.IDs()) != 0 {
		t.Fatalf("失败的加载不该留残迹：%v", instance.IDs())
	}
}

func TestLoadRejectsDuplicateID(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	instance := mustProvide(t, ctx, map[string]loader.PluginFactory{"a": noopFactory})

	if _, err := instance.Load(loader.LoaderEntry{ID: "a1", Name: "a"}); err != nil {
		t.Fatalf("首次加载失败：%v", err)
	}
	if _, err := instance.Load(loader.LoaderEntry{ID: "a1", Name: "a"}); !errors.Is(err, loader.ErrDuplicateEntry) {
		t.Fatalf("重复 id 错误 = %v，期望 ErrDuplicateEntry", err)
	}
	if len(instance.IDs()) != 1 {
		t.Fatalf("重复加载后已加载数 = %d，期望 1", len(instance.IDs()))
	}
}

func TestLoadRollsBackChildContextOnFactoryError(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	rolledBack := false
	instance := mustProvide(t, ctx, map[string]loader.PluginFactory{
		"bad": func(child *core.Context, _ any) error {
			child.OnDispose(core.Ignore(func() { rolledBack = true }))
			return errors.New("装不上")
		},
	})

	_, err := instance.Load(loader.LoaderEntry{ID: "b1", Name: "bad"})

	if err == nil {
		t.Fatal("工厂报错应让加载失败")
	}
	if !rolledBack {
		t.Fatal("工厂报错时子上下文应被回滚（效应撤销）")
	}
	if len(instance.IDs()) != 0 {
		t.Fatalf("失败的加载不该留残迹：%v", instance.IDs())
	}
}

func TestDisabledEntryIsRegisteredButNotLoaded(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	loaded := false
	instance := mustProvide(t, ctx, map[string]loader.PluginFactory{
		"a": func(*core.Context, any) error {
			loaded = true
			return nil
		},
	})

	if _, err := instance.Load(loader.LoaderEntry{ID: "a1", Name: "a", Disabled: true}); err != nil {
		t.Fatalf("加载失败：%v", err)
	}

	if loaded {
		t.Fatal("disabled 的 entry 不该加载")
	}
	if _, found := instance.EntryOf("a1"); !found {
		t.Fatal("disabled 的 entry 仍应登记在配置里")
	}
	if _, found := instance.ContextOf("a1"); found {
		t.Fatal("disabled 的 entry 不该有子上下文")
	}
}

func TestRegisterHasNamesUnregister(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	instance := mustProvide(t, ctx, nil)

	if instance.Has("a") {
		t.Fatal("未注册时 Has 应为 false")
	}
	if err := instance.Register("a", noopFactory); err != nil {
		t.Fatalf("注册失败：%v", err)
	}
	if !instance.Has("a") {
		t.Fatal("注册后 Has 应为 true")
	}
	if names := instance.Names(); !reflect.DeepEqual(names, []string{"a"}) {
		t.Fatalf("已注册 = %v，期望 [a]", names)
	}
	if !instance.Unregister("a") {
		t.Fatal("注销已注册的插件应返回 true")
	}
	if instance.Unregister("a") {
		t.Fatal("重复注销应返回 false")
	}
	if instance.Has("a") {
		t.Fatal("注销后 Has 应为 false")
	}
}
