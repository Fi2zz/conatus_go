package tools_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Fi2zz/conatus_go/foundation/tools"
)

// echoTool 是覆盖最小契约的工具：只实现三个必须方法。
type echoTool struct{}

func (echoTool) Name() string        { return "echo" }
func (echoTool) Description() string { return "回显输入" }

func (echoTool) Call(_ context.Context, tc *tools.ToolContext) (tools.ToolResult, error) {
	text, _ := tc.String("text")
	return tools.Success(text, tools.WithValue(text)), nil
}

// countingTool 统计执行次数；执行体可能在别的 goroutine 上跑，因此用原子量。
type countingTool struct{ runs atomic.Int64 }

func (t *countingTool) Name() string        { return "count" }
func (t *countingTool) Description() string { return "" }

func (t *countingTool) Call(_ context.Context, _ *tools.ToolContext) (tools.ToolResult, error) {
	t.runs.Add(1)
	return tools.Success("x"), nil
}

// boomTool 执行体直接失败。
type boomTool struct{}

func (boomTool) Name() string        { return "boom" }
func (boomTool) Description() string { return "" }

func (boomTool) Call(_ context.Context, _ *tools.ToolContext) (tools.ToolResult, error) {
	return tools.ToolResult{}, errors.New("炸了")
}

// greetTool 带必填参数声明。
type greetTool struct{ runs atomic.Int64 }

func (t *greetTool) Name() string        { return "greet" }
func (t *greetTool) Description() string { return "打招呼" }

func (t *greetTool) Params() []*tools.ParamSpec {
	return []*tools.ParamSpec{tools.StringParam("who", tools.Required())}
}

func (t *greetTool) Call(_ context.Context, tc *tools.ToolContext) (tools.ToolResult, error) {
	t.runs.Add(1)
	who, err := tc.RequireString("who")
	if err != nil {
		return tools.ToolResult{}, err
	}
	return tools.Success("hi " + who), nil
}

// slowTool 无视 ctx 地慢：用来验证「超时丢弃迟到结果」而不是取消工具。
type slowTool struct{}

func (slowTool) Name() string        { return "slow" }
func (slowTool) Description() string { return "" }

func (slowTool) Call(_ context.Context, _ *tools.ToolContext) (tools.ToolResult, error) {
	time.Sleep(100 * time.Millisecond)
	return tools.Success("done"), nil
}

// selfTimedTool 自声明 20ms 超时。
type selfTimedTool struct{}

func (selfTimedTool) Name() string           { return "self-timed" }
func (selfTimedTool) Description() string    { return "" }
func (selfTimedTool) Timeout() time.Duration { return 20 * time.Millisecond }

func (selfTimedTool) Call(_ context.Context, _ *tools.ToolContext) (tools.ToolResult, error) {
	time.Sleep(100 * time.Millisecond)
	return tools.Success("done"), nil
}

// recordingTool 立即返回，用来观察中间件是否被工具超时误伤。
type recordingTool struct{ runs atomic.Int64 }

func (*recordingTool) Name() string        { return "rec" }
func (*recordingTool) Description() string { return "" }

func (t *recordingTool) Call(_ context.Context, _ *tools.ToolContext) (tools.ToolResult, error) {
	t.runs.Add(1)
	return tools.Success("ran"), nil
}

func TestToolDefaultsAndSchemaWhitelist(t *testing.T) {
	var tool tools.Tool = echoTool{}

	if got := tools.RiskOf(tool); got != tools.RiskLow {
		t.Fatalf("缺省风险等级 = %v，期望 low", got)
	}
	if got := tools.GroupOf(tool); got != "" {
		t.Fatalf("缺省分组 = %q，期望空", got)
	}
	if got := tools.TimeoutOf(tool); got != 0 {
		t.Fatalf("缺省超时 = %v，期望 0", got)
	}
	if got := tools.PathParamsOf(tool); len(got) != 0 {
		t.Fatalf("缺省路径参数 = %v，期望空", got)
	}

	schema := tools.SchemaOf(tool)
	if len(schema) != 3 {
		t.Fatalf("schema 字段数 = %d，期望 3（白名单投影）：%v", len(schema), schema)
	}
	for _, key := range []string{"name", "description", "parameters"} {
		if _, found := schema[key]; !found {
			t.Fatalf("schema 缺少 %q", key)
		}
	}
	if schema["name"] != "echo" || schema["description"] != "回显输入" {
		t.Fatalf("schema 投影异常：%v", schema)
	}
	parameters, ok := schema["parameters"].(map[string]any)
	if !ok || parameters["type"] != "object" {
		t.Fatalf("parameters 投影异常：%v", schema["parameters"])
	}
}

func TestToolContextReadsTypedArguments(t *testing.T) {
	tc := tools.NewToolContext(tools.ToolCall{
		CallID: "c1",
		Name:   "echo",
		Arguments: map[string]any{
			"text": "hi", "n": 3, "ratio": 2, "flag": true,
			"items": []any{1, 2}, "nested": map[string]any{"a": 1},
		},
	})

	if tc.CallID() != "c1" {
		t.Fatalf("CallID = %q，期望 c1", tc.CallID())
	}
	if !tc.Has("text") || tc.Has("missing") {
		t.Fatal("Has 判定错误")
	}
	if tc.Value("n") != 3 {
		t.Fatalf("Value = %v，期望 3", tc.Value("n"))
	}
	if text, ok := tc.String("text"); !ok || text != "hi" {
		t.Fatalf("String = (%q, %v)", text, ok)
	}
	if number, ok := tc.Integer("n"); !ok || number != 3 {
		t.Fatalf("Integer = (%d, %v)", number, ok)
	}
	if ratio, ok := tc.Number("ratio"); !ok || ratio != 2.0 {
		t.Fatalf("Number 应把整数归一为浮点，得到 (%v, %v)", ratio, ok)
	}
	if flag, ok := tc.Boolean("flag"); !ok || !flag {
		t.Fatalf("Boolean = (%v, %v)", flag, ok)
	}
	if items, ok := tc.Array("items"); !ok || len(items) != 2 {
		t.Fatalf("Array = (%v, %v)", items, ok)
	}
	if nested, ok := tc.Object("nested"); !ok || nested["a"] != 1 {
		t.Fatalf("Object = (%v, %v)", nested, ok)
	}
	if _, ok := tc.String("missing"); ok {
		t.Fatal("缺失参数应返回 false")
	}
	if value, ok := tc.String("n"); ok || value != "" {
		t.Fatal("类型不符应返回 false 与零值")
	}
}

func TestToolContextRequireReportsFailures(t *testing.T) {
	tc := tools.NewToolContext(tools.ToolCall{
		Arguments: map[string]any{"n": "not-int", "empty": nil},
	})

	assertArgumentError(t, "缺失必填", func() error {
		_, err := tc.RequireString("missing")
		return err
	}, `缺少必填参数 "missing"`)
	assertArgumentError(t, "null 值", func() error {
		_, err := tc.RequireString("empty")
		return err
	}, `参数 "empty" 不能为 null`)
	assertArgumentError(t, "类型不符", func() error {
		_, err := tc.RequireInteger("n")
		return err
	}, `参数 "n" 期望 int，实际 string`)
}

func TestToolContextGenericHelpers(t *testing.T) {
	tc := tools.NewToolContext(tools.ToolCall{
		Arguments: map[string]any{"items": []any{"a"}, "n": 7},
	})

	items, ok := tools.Get[[]any](tc, "items")
	if !ok || len(items) != 1 {
		t.Fatalf("Get = (%v, %v)", items, ok)
	}
	number, err := tools.Require[int](tc, "n")
	if err != nil || number != 7 {
		t.Fatalf("Require[int] = (%d, %v)", number, err)
	}
	if _, err := tools.Require[string](tc, "n"); err == nil {
		t.Fatal("类型不符应报错")
	}
	if _, err := tools.Require[int](tc, "missing"); err == nil {
		t.Fatal("缺失参数应报错")
	}
}

func assertArgumentError(t *testing.T, name string, call func() error, wantMessage string) {
	t.Helper()
	err := call()
	if err == nil {
		t.Fatalf("%s：期望报错", name)
	}
	var argumentError *tools.ToolArgumentError
	if !errors.As(err, &argumentError) {
		t.Fatalf("%s：错误类型 = %T，期望 *tools.ToolArgumentError", name, err)
	}
	if argumentError.Message != wantMessage {
		t.Fatalf("%s：消息 = %q，期望 %q", name, argumentError.Message, wantMessage)
	}
	if got := err.Error(); got != fmt.Sprintf("ToolArgumentException: %s", wantMessage) {
		t.Fatalf("%s：Error() = %q", name, got)
	}
}

func assertPanics(t *testing.T, name string, body func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%s：期望 panic，但没有", name)
		}
	}()
	body()
}
