package tools_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/Fi2zz/conatus_go/foundation/tools"
)

func TestParamSpecProjectsToJSONSchema(t *testing.T) {
	spec := tools.StringParam("query",
		tools.Required(),
		tools.Describe("搜索词"),
		tools.WithDefault("x"),
	)

	got := spec.ToJSON()
	if got["type"] != "string" {
		t.Fatalf("type = %v，期望 string", got["type"])
	}
	if got["description"] != "搜索词" {
		t.Fatalf("description = %v", got["description"])
	}
	if got["default"] != "x" {
		t.Fatalf("default 未投影：%v", got)
	}
	// 顶层参数自身不输出 required（那是容器层的聚合字段）。
	if _, found := got["required"]; found {
		t.Fatalf("单个参数不应输出 required：%v", got)
	}
}

func TestParamSpecOmitsEmptyAnnotations(t *testing.T) {
	got := tools.StringParam("query").ToJSON()
	if len(got) != 1 || got["type"] != "string" {
		t.Fatalf("无注解参数只应投影 type：%v", got)
	}
}

func TestParamSpecTypesProjectTypeNames(t *testing.T) {
	cases := []struct {
		spec *tools.ParamSpec
		want string
	}{
		{tools.StringParam("a"), "string"},
		{tools.IntegerParam("b"), "integer"},
		{tools.NumberParam("c"), "number"},
		{tools.BooleanParam("d"), "boolean"},
		{tools.EnumParam("e", []string{"x", "y"}), "string"},
		{tools.ArrayParam("f", tools.StringParam("item")), "array"},
		{tools.ObjectParam("g", map[string]*tools.ParamSpec{"k": tools.StringParam("k")}), "object"},
	}
	for _, item := range cases {
		if got := item.spec.ToJSON()["type"]; got != item.want {
			t.Fatalf("%s 的 type = %v，期望 %s", item.spec.Name, got, item.want)
		}
	}
}

func TestParamSpecEnumAndNestedStructures(t *testing.T) {
	enum := tools.EnumParam("mode", []string{"fast", "slow"})
	if got := enum.ToJSON()["enum"]; !reflect.DeepEqual(got, []string{"fast", "slow"}) {
		t.Fatalf("enum = %v", got)
	}

	array := tools.ArrayParam("tags", tools.StringParam("tag"))
	items, ok := array.ToJSON()["items"].(map[string]any)
	if !ok || items["type"] != "string" {
		t.Fatalf("items 投影 = %v", array.ToJSON()["items"])
	}

	nested := tools.ObjectParam("filter", map[string]*tools.ParamSpec{
		"limit": tools.IntegerParam("limit", tools.Required()),
		"name":  tools.StringParam("name"),
	})
	projected := nested.ToJSON()
	properties, ok := projected["properties"].(map[string]any)
	if !ok || len(properties) != 2 {
		t.Fatalf("properties 投影 = %v", projected["properties"])
	}
	required, ok := projected["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "limit" {
		t.Fatalf("嵌套 required 聚合 = %v，期望 [limit]", projected["required"])
	}
}

func TestParamBuildersRejectEmptyDeclarations(t *testing.T) {
	assertPanics(t, "空枚举", func() { tools.EnumParam("mode", nil) })
	assertPanics(t, "数组缺少元素声明", func() { tools.ArrayParam("tags", nil) })
	assertPanics(t, "对象缺少字段声明", func() { tools.ObjectParam("filter", nil) })
}

func TestParameterSchemaAggregatesTopLevelRequired(t *testing.T) {
	schema := tools.ParameterSchema([]*tools.ParamSpec{
		tools.StringParam("query", tools.Required()),
		tools.IntegerParam("limit"),
	})

	if schema["type"] != "object" {
		t.Fatalf("type = %v，期望 object", schema["type"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) != 2 {
		t.Fatalf("properties = %v", schema["properties"])
	}
	required, ok := schema["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "query" {
		t.Fatalf("required = %v，期望 [query]", schema["required"])
	}
}

func TestParameterSchemaOmitsRequiredWhenAllOptional(t *testing.T) {
	schema := tools.ParameterSchema([]*tools.ParamSpec{tools.StringParam("query")})
	if _, found := schema["required"]; found {
		t.Fatalf("全可选时不应输出 required：%v", schema)
	}
	if parameters := tools.ParameterSchema(nil); parameters["type"] != "object" {
		t.Fatalf("无参时仍应是合法的 object schema：%v", parameters)
	}
}

func TestValidateArgsReportsViolations(t *testing.T) {
	tool := &greetTool{}
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"缺失必填", map[string]any{}, `缺少必填参数 "who"`},
		{"显式 null", map[string]any{"who": nil}, `参数 "who" 不能为 null`},
		{"类型不符", map[string]any{"who": 1}, `参数 "who" 期望 string`},
	}
	for _, item := range cases {
		violations := tools.ValidateArgs(tool, item.args)
		if len(violations) != 1 || violations[0] != item.want {
			t.Fatalf("%s：违规 = %v，期望 [%s]", item.name, violations, item.want)
		}
	}
	if violations := tools.ValidateArgs(tool, map[string]any{"who": "助手"}); len(violations) != 0 {
		t.Fatalf("合法参数不应有违规：%v", violations)
	}
}

func TestValidateArgsRecursesIntoNestedStructures(t *testing.T) {
	tool := &deepTool{}

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"嵌套对象字段类型不符", map[string]any{"filter": map[string]any{"limit": "x"}},
			`参数 "filter.limit" 期望 integer`},
		{"数组元素类型不符", map[string]any{"tags": []any{"ok", 2}},
			`参数 "tags[1]" 期望 string`},
		{"枚举取值非法", map[string]any{"mode": "quick"},
			`参数 "mode" 取值不在 [fast, slow] 内`},
		{"对象字段为 null 且必填", map[string]any{"filter": map[string]any{"limit": nil}},
			`参数 "filter.limit" 不能为 null`},
	}
	for _, item := range cases {
		violations := tools.ValidateArgs(tool, item.args)
		if len(violations) != 1 || violations[0] != item.want {
			t.Fatalf("%s：违规 = %v，期望 [%s]", item.name, violations, item.want)
		}
	}
}

// deepTool 声明嵌套对象、数组与枚举，用来钉住递归校验的路径文案。
type deepTool struct{}

func (deepTool) Name() string        { return "deep" }
func (deepTool) Description() string { return "" }

func (deepTool) Params() []*tools.ParamSpec {
	return []*tools.ParamSpec{
		tools.ObjectParam("filter", map[string]*tools.ParamSpec{
			"limit": tools.IntegerParam("limit", tools.Required()),
		}),
		tools.ArrayParam("tags", tools.StringParam("tag")),
		tools.EnumParam("mode", []string{"fast", "slow"}),
	}
}

func (deepTool) Call(_ context.Context, _ *tools.ToolContext) (tools.ToolResult, error) {
	return tools.Success("ok"), nil
}
