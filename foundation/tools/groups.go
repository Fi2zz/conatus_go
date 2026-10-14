package tools

import (
	"fmt"
	"sync"

	"github.com/Fi2zz/conatus_go/core"
)

// Groups 是注册表之上的分组视图与风险分级。
//
// 分组登记与注册表本体分离（与 Dart 版一致）：注册表不需要知道分组这件事；分组表
// 只对自己登记过的名字有话语权，其余回退到工具自身的 [GroupOf]。
type Groups struct {
	registry *Registry

	mu       sync.Mutex
	assigned map[string]string
	order    []string
}

// NewGroups 在一个注册表之上建立分组视图。
func NewGroups(registry *Registry) *Groups {
	return &Groups{registry: registry, assigned: map[string]string{}}
}

// Add 按领域注册一组工具，返回撤销整组的函数（幂等，按登记逆序撤销）。
//
// 组内任一个登记失败即回滚本组已登记的部分并返回错误——Dart 版是边登记边抛，
// 会把半组留在表里；Go 侧改为原子登记，避免「一半成功」的中间态。
func (g *Groups) Add(name string, tools ...Tool) (core.Disposer, error) {
	revokers := make([]core.Disposer, 0, len(tools))
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		revoke, err := g.registry.Register(tool)
		if err != nil {
			rollback(revokers, names, g)
			return nil, err
		}
		revokers = append(revokers, revoke)
		names = append(names, tool.Name())
		g.assign(tool.Name(), name)
	}

	var once sync.Once
	return func() error {
		once.Do(func() {
			rollback(revokers, names, g)
		})
		return nil
	}, nil
}

// Names 返回已登记的分组名（首次出现顺序）。
func (g *Groups) Names() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.order...)
}

// GroupOf 返回某工具的分组：注册时登记的优先，否则回退到工具的静态分组。
func (g *Groups) GroupOf(toolName string) string {
	g.mu.Lock()
	assigned, found := g.assigned[toolName]
	g.mu.Unlock()
	if found {
		return assigned
	}
	if tool, ok := g.registry.Get(toolName); ok {
		return GroupOf(tool)
	}
	return ""
}

// NamesIn 返回某分组下的工具名（注册顺序）。
func (g *Groups) NamesIn(group string) []string {
	names := []string{}
	for _, name := range g.registry.Names() {
		if g.GroupOf(name) == group {
			names = append(names, name)
		}
	}
	return names
}

// DescribeGroup 返回某分组下工具的模型 schema。
func (g *Groups) DescribeGroup(group string) []map[string]any {
	return g.describe(g.NamesIn(group))
}

// DescribeWithin 只投影风险等级不高于 maxRisk 的工具 schema。
func (g *Groups) DescribeWithin(maxRisk Risk) []map[string]any {
	names := []string{}
	for _, name := range g.registry.Names() {
		if tool, ok := g.registry.Get(name); ok && RiskOf(tool) <= maxRisk {
			names = append(names, name)
		}
	}
	return g.describe(names)
}

// GuardRisk 登记能力分级守卫：拒绝风险等级高于 maxRisk 的工具调用。
// 返回撤销函数（幂等）。
func (g *Groups) GuardRisk(maxRisk Risk) core.Disposer {
	return g.registry.Guard(func(call ToolCall) string {
		tool, found := g.registry.Get(call.Name)
		if !found {
			return ""
		}
		risk := RiskOf(tool)
		if risk <= maxRisk {
			return ""
		}
		return fmt.Sprintf("工具 %q 风险等级 %s 高于允许的 %s", call.Name, risk, maxRisk)
	})
}

func (g *Groups) assign(toolName, group string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.assigned[toolName] = group
	for _, existing := range g.order {
		if existing == group {
			return
		}
	}
	g.order = append(g.order, group)
}

func (g *Groups) unassign(toolName, group string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.assigned, toolName)
	if g.stillAssigned(group) {
		return
	}
	for index, existing := range g.order {
		if existing == group {
			g.order = append(g.order[:index], g.order[index+1:]...)
			return
		}
	}
}

// stillAssigned 报告该分组名下是否还有别的工具（调用方须持有锁）。
func (g *Groups) stillAssigned(group string) bool {
	for _, assigned := range g.assigned {
		if assigned == group {
			return true
		}
	}
	return false
}

// rollback 逆序撤销本组已登记的工具并清掉分组归属。
func rollback(revokers []core.Disposer, names []string, g *Groups) {
	for index := len(revokers) - 1; index >= 0; index-- {
		_ = revokers[index]()
		if index < len(names) {
			g.unassign(names[index], g.GroupOf(names[index]))
		}
	}
}

func (g *Groups) describe(names []string) []map[string]any {
	schemas := []map[string]any{}
	for _, name := range names {
		if schema, found := g.registry.DescribeOne(name); found {
			schemas = append(schemas, schema)
		}
	}
	return schemas
}
