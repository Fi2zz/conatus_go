package tools_test

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
	"github.com/Fi2zz/conatus_go/foundation/tools"
)

func mustRegister(t *testing.T, registry *tools.Registry, tool tools.Tool) core.Disposer {
	t.Helper()
	off, err := registry.Register(tool)
	if err != nil {
		t.Fatalf("注册 %q 失败：%v", tool.Name(), err)
	}
	return off
}

func TestRegisterGetNamesAndDescribe(t *testing.T) {
	registry := tools.NewRegistry()

	if registry.Len() != 0 {
		t.Fatalf("新注册表应有 0 个工具，得到 %d", registry.Len())
	}
	off := mustRegister(t, registry, echoTool{})

	if _, found := registry.Get("echo"); !found {
		t.Fatal("注册后应能取到工具")
	}
	if _, found := registry.Get("nope"); found {
		t.Fatal("未注册的工具不该取到")
	}
	if got := registry.Names(); !reflect.DeepEqual(got, []string{"echo"}) {
		t.Fatalf("Names = %v，期望 [echo]", got)
	}
	schemas := registry.Describe()
	if len(schemas) != 1 || schemas[0]["name"] != "echo" {
		t.Fatalf("Describe = %v", schemas)
	}
	if _, found := registry.DescribeOne("nope"); found {
		t.Fatal("未注册的工具不该有 schema")
	}

	if err := off(); err != nil {
		t.Fatalf("撤销失败：%v", err)
	}
	if registry.Len() != 0 {
		t.Fatalf("撤销后应为 0，得到 %d", registry.Len())
	}
}

func TestRegisterRejectsDuplicateAndNil(t *testing.T) {
	registry := tools.NewRegistry()
	mustRegister(t, registry, echoTool{})

	if _, err := registry.Register(echoTool{}); err == nil {
		t.Fatal("同名重复注册应返回错误")
	}
	if _, err := registry.Register(nil); err == nil {
		t.Fatal("nil 工具应返回错误")
	}
}

func TestRevokeIsIdempotentAndDoesNotTouchReregisteredTool(t *testing.T) {
	registry := tools.NewRegistry()
	off := mustRegister(t, registry, &countingTool{})

	if err := off(); err != nil {
		t.Fatalf("首次撤销失败：%v", err)
	}
	if err := off(); err != nil {
		t.Fatalf("重复撤销应幂等：%v", err)
	}
	mustRegister(t, registry, &countingTool{})

	if err := off(); err != nil {
		t.Fatalf("迟到撤销不应报错：%v", err)
	}
	if _, found := registry.Get("count"); !found {
		t.Fatal("旧撤销误删了之后重新登记的同名工具")
	}
}

func TestOnChangeFiresOnRegisterAndRevoke(t *testing.T) {
	registry := tools.NewRegistry()
	changes := 0
	watch := registry.OnChange(func() { changes++ })

	off := mustRegister(t, registry, echoTool{})
	if changes != 1 {
		t.Fatalf("注册后变更次数 = %d，期望 1", changes)
	}
	if err := off(); err != nil {
		t.Fatalf("撤销失败：%v", err)
	}
	if changes != 2 {
		t.Fatalf("撤销后变更次数 = %d，期望 2", changes)
	}

	if err := watch(); err != nil {
		t.Fatalf("取消监听失败：%v", err)
	}
	mustRegister(t, registry, echoTool{})
	if changes != 2 {
		t.Fatalf("取消监听后仍收到通知：%d", changes)
	}
}

func TestOnResultSeesEveryOutcome(t *testing.T) {
	registry := tools.NewRegistry()
	mustRegister(t, registry, echoTool{})
	seen := []string{}
	registry.OnResult(func(call tools.ToolCall, result tools.ToolResult) {
		seen = append(seen, call.Name)
	})

	registry.Call(context.Background(), tools.ToolCall{Name: "echo"})
	registry.Call(context.Background(), tools.ToolCall{Name: "nope"})

	if !reflect.DeepEqual(seen, []string{"echo", "nope"}) {
		t.Fatalf("结果观察 = %v，期望 [echo nope]（失败也要广播）", seen)
	}
}

func TestProvideAndFrom(t *testing.T) {
	ctx := core.NewRoot("root")
	registry, err := tools.Provide(ctx)
	if err != nil {
		t.Fatalf("provide 失败：%v", err)
	}
	if !ctx.Has(tools.ServiceKey) {
		t.Fatal("服务应已注册")
	}

	from, err := tools.From(ctx)
	if err != nil || from != registry {
		t.Fatalf("From = (%v, %v)，期望同一实例", from, err)
	}

	off := mustRegister(t, registry, echoTool{})
	ctx.Track(off)
	ctx.Dispose()

	if _, found := registry.Get("echo"); found {
		t.Fatal("上下文释放应撤销登记（登记返回的 Disposer 交给 Track）")
	}
}

func TestFromFailsWithoutRegistration(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)

	if _, err := tools.From(ctx); err == nil {
		t.Fatal("未提供注册表时 From 应返回错误")
	}
}

func TestProvideUsesGivenRegistryWithTimeout(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	given := tools.NewRegistry()

	registry, err := tools.Provide(ctx, tools.WithRegistry(given))
	if err != nil || registry != given {
		t.Fatalf("应提供传入的注册表：(%v, %v)", registry, err)
	}

	other := core.NewRoot("other")
	t.Cleanup(other.Dispose)
	built, err := tools.Provide(other, tools.WithRegistryTimeout(1500))
	if err != nil {
		t.Fatalf("provide 失败：%v", err)
	}
	if built.DefaultTimeout() != 1500 {
		t.Fatalf("缺省超时 = %v，期望 1500ns（Duration 以纳秒为单位）", built.DefaultTimeout())
	}
}

func TestRegistryIsConcurrencySafe(t *testing.T) {
	registry := tools.NewRegistry()
	var group sync.WaitGroup

	for index := 0; index < 8; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			name := fmt.Sprintf("tool-%d", index)
			off, err := registry.Fn(name, "并发登记",
				func(context.Context, *tools.ToolContext) (tools.ToolResult, error) {
					return tools.Success("ok"), nil
				})
			if err != nil {
				t.Errorf("登记 %q 失败：%v", name, err)
				return
			}
			registry.Call(context.Background(), tools.ToolCall{Name: name})
			if callErr := off(); callErr != nil {
				t.Errorf("撤销 %q 失败：%v", name, callErr)
			}
		}(index)
	}
	group.Wait()

	if registry.Len() != 0 {
		t.Fatalf("并发登记/撤销后应清空，得到 %d", registry.Len())
	}
}
