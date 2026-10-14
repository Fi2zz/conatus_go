package tools_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/Fi2zz/conatus_go/foundation/tools"
)

func echoHandler(_ context.Context, tc *tools.ToolContext) (tools.ToolResult, error) {
	text, _ := tc.String("text")
	return tools.Success(text, tools.WithValue(text)), nil
}

func TestFnRegistersFunctionalTool(t *testing.T) {
	registry := tools.NewRegistry()
	off, err := registry.Fn("echo", "回显输入", echoHandler,
		tools.WithFnParams(tools.StringParam("text", tools.Required())))
	if err != nil {
		t.Fatalf("登记失败：%v", err)
	}

	result := registry.Call(context.Background(), tools.ToolCall{
		Name:      "echo",
		Arguments: map[string]any{"text": "hi"},
	})
	if result.Content != "hi" || result.Value != "hi" {
		t.Fatalf("结果 = %+v", result)
	}

	invalid := registry.Call(context.Background(), tools.ToolCall{Name: "echo"})
	if invalid.Error == nil || invalid.Error.Code != "INVALID_ARGS" {
		t.Fatalf("fn 登记的工具同样要过参数校验，得到 %+v", invalid)
	}

	schema, found := registry.DescribeOne("echo")
	if !found {
		t.Fatal("应能描述已登记的工具")
	}
	parameters, ok := schema["parameters"].(map[string]any)
	if !ok || parameters["type"] != "object" {
		t.Fatalf("parameters = %v", schema["parameters"])
	}
	required, ok := parameters["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "text" {
		t.Fatalf("required = %v，期望 [text]", parameters["required"])
	}

	if err := off(); err != nil {
		t.Fatalf("撤销失败：%v", err)
	}
	if _, found := registry.Get("echo"); found {
		t.Fatal("撤销后工具应消失")
	}
}

func TestFnCarriesMetadataWithoutLeakingIt(t *testing.T) {
	registry := tools.NewRegistry()
	off, err := registry.Fn("write", "写文件", echoHandler,
		tools.WithFnRisk(tools.RiskHigh),
		tools.WithFnGroup("fs"),
		tools.WithFnPathParams("path"))
	if err != nil {
		t.Fatalf("登记失败：%v", err)
	}
	defer func() { _ = off() }()

	tool, found := registry.Get("write")
	if !found {
		t.Fatal("工具应已登记")
	}
	if got := tools.RiskOf(tool); got != tools.RiskHigh {
		t.Fatalf("风险等级 = %v，期望 high", got)
	}
	if got := tools.GroupOf(tool); got != "fs" {
		t.Fatalf("分组 = %q，期望 fs", got)
	}
	if got := tools.PathParamsOf(tool); !reflect.DeepEqual(got, []string{"path"}) {
		t.Fatalf("路径参数 = %v，期望 [path]", got)
	}
	if got := tools.TimeoutOf(tool); got != 0 {
		t.Fatalf("未声明超时时应为 0，得到 %v", got)
	}

	schema := tools.SchemaOf(tool)
	if len(schema) != 3 {
		t.Fatalf("宿主字段不该下发模型：%v", schema)
	}
}

func TestFnRejectsMissingHandler(t *testing.T) {
	registry := tools.NewRegistry()

	if _, err := registry.Fn("x", "说明", nil); err == nil {
		t.Fatal("缺少执行体应返回错误")
	}
	if registry.Len() != 0 {
		t.Fatalf("失败的登记不该留下痕迹，得到 %d", registry.Len())
	}
}

func TestFnRejectsDuplicateName(t *testing.T) {
	registry := tools.NewRegistry()
	off, err := registry.Fn("echo", "回显", echoHandler)
	if err != nil {
		t.Fatalf("登记失败：%v", err)
	}
	defer func() { _ = off() }()

	if _, err := registry.Fn("echo", "又来了", echoHandler); err == nil {
		t.Fatal("同名重复登记应返回错误")
	}
}
