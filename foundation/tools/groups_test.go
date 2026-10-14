package tools_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/Fi2zz/conatus_go/foundation/tools"
)

// labeledTool 是可命名、可声明分组的工具，供分组用例复用。
type labeledTool struct {
	name  string
	group string
}

func (t *labeledTool) Name() string        { return t.name }
func (t *labeledTool) Description() string { return "" }
func (t *labeledTool) GroupName() string   { return t.group }

func (t *labeledTool) Call(_ context.Context, _ *tools.ToolContext) (tools.ToolResult, error) {
	return tools.Success("ok"), nil
}

func TestGroupsAddRegistersBatchAndRevokesWholeGroup(t *testing.T) {
	registry := tools.NewRegistry()
	groups := tools.NewGroups(registry)

	off, err := groups.Add("web", &labeledTool{name: "search"}, &labeledTool{name: "fetch"})
	if err != nil {
		t.Fatalf("分组登记失败：%v", err)
	}

	if got := registry.Names(); !reflect.DeepEqual(got, []string{"search", "fetch"}) {
		t.Fatalf("Names = %v", got)
	}
	if got := groups.Names(); !reflect.DeepEqual(got, []string{"web"}) {
		t.Fatalf("分组名 = %v，期望 [web]", got)
	}
	if got := groups.NamesIn("web"); !reflect.DeepEqual(got, []string{"search", "fetch"}) {
		t.Fatalf("组内工具 = %v", got)
	}

	if err := off(); err != nil {
		t.Fatalf("撤销失败：%v", err)
	}
	if registry.Len() != 0 {
		t.Fatalf("撤销整组后应为空，得到 %d", registry.Len())
	}
	if len(groups.Names()) != 0 {
		t.Fatalf("撤销后分组名应为空：%v", groups.Names())
	}
	if err := off(); err != nil {
		t.Fatalf("重复撤销应幂等：%v", err)
	}
}

func TestGroupsAddRollsBackOnDuplicate(t *testing.T) {
	registry := tools.NewRegistry()
	mustRegister(t, registry, &labeledTool{name: "fetch"})
	groups := tools.NewGroups(registry)

	_, err := groups.Add("web", &labeledTool{name: "search"}, &labeledTool{name: "fetch"})

	if err == nil {
		t.Fatal("组内重名应返回错误")
	}
	if _, found := registry.Get("search"); found {
		t.Fatal("失败的整组登记应回滚，不留半组")
	}
	if len(groups.Names()) != 0 {
		t.Fatalf("回滚后不该留下分组名：%v", groups.Names())
	}
}

func TestGroupsGroupOfPrefersAssignmentThenStatic(t *testing.T) {
	registry := tools.NewRegistry()
	groups := tools.NewGroups(registry)
	off, err := groups.Add("web", &labeledTool{name: "search"})
	if err != nil {
		t.Fatalf("分组登记失败：%v", err)
	}
	defer func() { _ = off() }()
	mustRegister(t, registry, &labeledTool{name: "static", group: "fs"})

	if got := groups.GroupOf("search"); got != "web" {
		t.Fatalf("分组登记应优先：%q", got)
	}
	if got := groups.GroupOf("static"); got != "fs" {
		t.Fatalf("无分组登记时应回退到工具自声明：%q", got)
	}
	if got := groups.GroupOf("absent"); got != "" {
		t.Fatalf("不存在的工具应返回空串：%q", got)
	}
}

func TestGroupsDescribeGroupAndWithin(t *testing.T) {
	registry := tools.NewRegistry()
	groups := tools.NewGroups(registry)
	off, err := groups.Add("web", &labeledTool{name: "search"}, &labeledTool{name: "fetch"})
	if err != nil {
		t.Fatalf("分组登记失败：%v", err)
	}
	defer func() { _ = off() }()
	high, err := registry.Fn("danger", "高危", echoHandler, tools.WithFnRisk(tools.RiskHigh))
	if err != nil {
		t.Fatalf("登记失败：%v", err)
	}
	defer func() { _ = high() }()

	if got := groups.DescribeGroup("web"); len(got) != 2 {
		t.Fatalf("组内 schema 数 = %d，期望 2", len(got))
	}
	if got := groups.DescribeGroup("absent"); len(got) != 0 {
		t.Fatalf("不存在的分组应为空：%v", got)
	}
	within := groups.DescribeWithin(tools.RiskLow)
	if len(within) != 2 {
		t.Fatalf("低风险投影 = %d 个，期望 2（danger 是 high）", len(within))
	}
}

func TestGroupsGuardRiskDeniesHigherRisk(t *testing.T) {
	registry := tools.NewRegistry()
	groups := tools.NewGroups(registry)
	low, err := registry.Fn("read", "只读", echoHandler)
	if err != nil {
		t.Fatalf("登记失败：%v", err)
	}
	defer func() { _ = low() }()
	high, err := registry.Fn("danger", "高危", echoHandler, tools.WithFnRisk(tools.RiskHigh))
	if err != nil {
		t.Fatalf("登记失败：%v", err)
	}
	defer func() { _ = high() }()
	off := groups.GuardRisk(tools.RiskLow)
	defer func() { _ = off() }()

	allowed := registry.Call(context.Background(), tools.ToolCall{Name: "read"})
	if allowed.IsError {
		t.Fatalf("低风险工具应放行：%+v", allowed)
	}

	denied := registry.Call(context.Background(), tools.ToolCall{Name: "danger"})
	if denied.Error == nil || denied.Error.Code != "TOOL_DENIED" {
		t.Fatalf("越级工具应被拒绝：%+v", denied)
	}
	want := `工具 "danger" 风险等级 high 高于允许的 low`
	if denied.Content != want {
		t.Fatalf("拒绝文案 = %q，期望 %q", denied.Content, want)
	}
}
