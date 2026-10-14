package tools_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Fi2zz/conatus_go/foundation/tools"
)

func TestCallReportsUnknownTool(t *testing.T) {
	registry := tools.NewRegistry()

	result := registry.Call(context.Background(), tools.ToolCall{Name: "nope"})

	if !result.IsError || result.Error.Code != "UNKNOWN_TOOL" {
		t.Fatalf("结果 = %+v，期望 UNKNOWN_TOOL", result)
	}
	if result.Content != `未知工具 "nope"` {
		t.Fatalf("文案 = %q", result.Content)
	}
}

func TestGuardDenialSkipsExecution(t *testing.T) {
	counter := &countingTool{}
	registry := tools.NewRegistry()
	mustRegister(t, registry, counter)
	registry.Guard(func(call tools.ToolCall) string {
		if call.Name == "count" {
			return "不允许"
		}
		return ""
	})

	result := registry.Call(context.Background(), tools.ToolCall{Name: "count"})

	if counter.runs.Load() != 0 {
		t.Fatal("守卫拒绝时执行体不应运行")
	}
	if result.Error.Code != "TOOL_DENIED" || result.Content != "不允许" {
		t.Fatalf("结果 = %+v，期望 TOOL_DENIED / 不允许", result)
	}
}

func TestCallSucceedsAndBroadcastsValue(t *testing.T) {
	registry := tools.NewRegistry()
	mustRegister(t, registry, echoTool{})

	result := registry.Call(context.Background(), tools.ToolCall{
		Name:      "echo",
		Arguments: map[string]any{"text": "hi"},
	})

	if result.IsError {
		t.Fatalf("不应失败：%+v", result)
	}
	if result.Content != "hi" || result.Value != "hi" {
		t.Fatalf("结果 = %+v，期望 content/value 都是 hi", result)
	}
}

func TestMiddlewareNestsInRegistrationOrder(t *testing.T) {
	registry := tools.NewRegistry()
	mustRegister(t, registry, echoTool{})
	order := []string{}

	registry.Use(func(ctx context.Context, call tools.ToolCall, next tools.Next) (tools.ToolResult, error) {
		order = append(order, "outer-before")
		inner, err := next(ctx)
		order = append(order, "outer-after")
		return tools.Success("[" + inner.Content + "]"), err
	}, "")
	registry.Use(func(ctx context.Context, call tools.ToolCall, next tools.Next) (tools.ToolResult, error) {
		order = append(order, "inner")
		return next(ctx)
	}, "")

	result := registry.Call(context.Background(), tools.ToolCall{
		Name:      "echo",
		Arguments: map[string]any{"text": "hi"},
	})

	if want := []string{"outer-before", "inner", "outer-after"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("顺序 = %v，期望 %v（先登记的在外层）", order, want)
	}
	if result.Content != "[hi]" {
		t.Fatalf("内容 = %q，期望 [hi]（外层可改写结果）", result.Content)
	}
}

func TestBodyErrorBecomesToolError(t *testing.T) {
	registry := tools.NewRegistry()
	mustRegister(t, registry, boomTool{})

	result := registry.Call(context.Background(), tools.ToolCall{Name: "boom"})

	if result.Error.Code != "TOOL_ERROR" {
		t.Fatalf("结果 = %+v，期望 TOOL_ERROR", result)
	}
}

func TestMiddlewareErrorBecomesToolError(t *testing.T) {
	registry := tools.NewRegistry()
	mustRegister(t, registry, echoTool{})
	registry.Use(func(ctx context.Context, call tools.ToolCall, next tools.Next) (tools.ToolResult, error) {
		return tools.ToolResult{}, context.Canceled
	}, "")

	result := registry.Call(context.Background(), tools.ToolCall{Name: "echo"})

	if result.Error.Code != "TOOL_ERROR" || result.Content != "context canceled" {
		t.Fatalf("结果 = %+v，期望中间件异常被收敛为 TOOL_ERROR", result)
	}
}

func TestInvalidArgsSkipExecution(t *testing.T) {
	greet := &greetTool{}
	registry := tools.NewRegistry()
	mustRegister(t, registry, greet)

	result := registry.Call(context.Background(), tools.ToolCall{Name: "greet"})

	if greet.runs.Load() != 0 {
		t.Fatal("参数校验失败时执行体不应运行")
	}
	if result.Error.Code != "INVALID_ARGS" {
		t.Fatalf("结果 = %+v，期望 INVALID_ARGS", result)
	}

	ok := registry.Call(context.Background(), tools.ToolCall{
		Name:      "greet",
		Arguments: map[string]any{"who": "助手"},
	})
	if ok.Content != "hi 助手" {
		t.Fatalf("合法参数应执行，得到 %+v", ok)
	}
}

func TestTimeoutReturnsToolTimeout(t *testing.T) {
	registry := tools.NewRegistry()
	mustRegister(t, registry, slowTool{})

	result := registry.Call(context.Background(), tools.ToolCall{Name: "slow"},
		tools.WithCallTimeout(20*time.Millisecond))

	if result.Error == nil || result.Error.Code != "TOOL_TIMEOUT" {
		t.Fatalf("结果 = %+v，期望 TOOL_TIMEOUT", result)
	}
}

func TestDefaultTimeoutApplies(t *testing.T) {
	registry := tools.NewRegistry(tools.WithDefaultTimeout(20 * time.Millisecond))
	mustRegister(t, registry, slowTool{})

	result := registry.Call(context.Background(), tools.ToolCall{Name: "slow"})

	if result.Error == nil || result.Error.Code != "TOOL_TIMEOUT" {
		t.Fatalf("结果 = %+v，期望缺省超时生效", result)
	}
}

func TestTimeoutPrecedence(t *testing.T) {
	registry := tools.NewRegistry(tools.WithDefaultTimeout(500 * time.Millisecond))
	mustRegister(t, registry, selfTimedTool{})

	if got := registry.Call(context.Background(), tools.ToolCall{Name: "self-timed"}); got.Error == nil ||
		got.Error.Code != "TOOL_TIMEOUT" {
		t.Fatalf("工具自声明超时应覆盖缺省，得到 %+v", got)
	}
	if got := registry.Call(context.Background(), tools.ToolCall{Name: "self-timed"},
		tools.WithCallTimeout(5*time.Second)); got.IsError {
		t.Fatalf("调用参数应覆盖工具自声明，得到 %+v", got)
	}
}

// TestTimeoutOnlyWrapsToolBody 是本包最要紧的一条语义：超时不套住等用户的中间件。
//
// 混在一处计时时小的那个静默获胜：用户批准后工具照样执行，但模型已被告知失败，
// 写操作会变成「以为失败而重试，实际写了两遍」。
func TestTimeoutOnlyWrapsToolBody(t *testing.T) {
	registry := tools.NewRegistry(tools.WithDefaultTimeout(30 * time.Millisecond))
	mustRegister(t, registry, &recordingTool{})
	registry.Use(func(ctx context.Context, call tools.ToolCall, next tools.Next) (tools.ToolResult, error) {
		time.Sleep(120 * time.Millisecond) // 模拟等用户
		return next(ctx)
	}, "")

	result := registry.Call(context.Background(), tools.ToolCall{Name: "rec"})

	if result.Error != nil && result.Error.Code == "TOOL_TIMEOUT" {
		t.Fatal("工具超时不该套住等用户的中间件")
	}
	if result.Content != "ran" {
		t.Fatalf("结果 = %+v，期望工具正常执行", result)
	}
}

func TestCallTimeoutCancellationIsNotReportedAsTimeout(t *testing.T) {
	registry := tools.NewRegistry()
	mustRegister(t, registry, slowTool{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := registry.Call(ctx, tools.ToolCall{Name: "slow"}, tools.WithCallTimeout(time.Second))

	if result.Error == nil || result.Error.Code != "TOOL_ERROR" {
		t.Fatalf("外层取消应报 TOOL_ERROR 而不是 TOOL_TIMEOUT，得到 %+v", result)
	}
}

func TestCopyPipelineCopiesGuardsAndMiddleware(t *testing.T) {
	parent := tools.NewRegistry()
	mustRegister(t, parent, echoTool{})
	order := []string{}
	parent.Use(func(ctx context.Context, call tools.ToolCall, next tools.Next) (tools.ToolResult, error) {
		order = append(order, call.Name)
		return next(ctx)
	}, "")

	child := tools.NewRegistry()
	mustRegister(t, child, echoTool{})
	parent.CopyPipelineTo(child)
	child.Call(context.Background(), tools.ToolCall{Name: "echo"})

	if got := child.Names(); !reflect.DeepEqual(got, []string{"echo"}) {
		t.Fatalf("工具不随管线复制：%v", got)
	}
	if !reflect.DeepEqual(order, []string{"echo"}) {
		t.Fatalf("中间件应被复制：%v", order)
	}

	parent.Guard(func(tools.ToolCall) string { return "不允许" })
	guarded := tools.NewRegistry()
	mustRegister(t, guarded, echoTool{})
	parent.CopyPipelineTo(guarded)
	result := guarded.Call(context.Background(), tools.ToolCall{Name: "echo"})
	if result.Error == nil || result.Error.Code != "TOOL_DENIED" {
		t.Fatalf("守卫应被复制，得到 %+v", result)
	}
}

func TestCopyPipelineHonoursExcludeTags(t *testing.T) {
	parent := tools.NewRegistry()
	order := []string{}
	parent.Use(func(ctx context.Context, call tools.ToolCall, next tools.Next) (tools.ToolResult, error) {
		order = append(order, "keep")
		return next(ctx)
	}, "keep")
	parent.Use(func(ctx context.Context, call tools.ToolCall, next tools.Next) (tools.ToolResult, error) {
		order = append(order, "skip")
		return next(ctx)
	}, "skip")

	child := tools.NewRegistry()
	mustRegister(t, child, echoTool{})
	parent.CopyPipelineTo(child, "skip")
	child.Call(context.Background(), tools.ToolCall{Name: "echo"})

	if !reflect.DeepEqual(order, []string{"keep"}) {
		t.Fatalf("排除 tag 未生效：%v", order)
	}
}

func TestCallHonoursToolPathParamsDeclaration(t *testing.T) {
	registry := tools.NewRegistry()
	off, err := registry.Fn("write", "写文件",
		func(context.Context, *tools.ToolContext) (tools.ToolResult, error) {
			return tools.Success("ok"), nil
		},
		tools.WithFnParams(tools.StringParam("path", tools.Required())),
		tools.WithFnPathParams("path"),
	)
	if err != nil {
		t.Fatalf("登记失败：%v", err)
	}
	defer func() { _ = off() }()

	tool, found := registry.Get("write")
	if !found {
		t.Fatal("工具应已登记")
	}
	if got := tools.PathParamsOf(tool); !reflect.DeepEqual(got, []string{"path"}) {
		t.Fatalf("路径参数 = %v，期望 [path]", got)
	}
}
