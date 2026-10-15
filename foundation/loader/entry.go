package loader

import "fmt"

// LoaderEntry 是 loader 配置树的一个节点。
//
// 分组节点不加载插件，只承载 [LoaderEntry.Children]。
//
// Dart 版用 `name == null` 表达分组；Go 的 string 没有 null，若把空名当分组，
// JSON 里 `"name": ""` 就会被静默当成分组，而 Dart 会报「未注册的插件 ""」。
// 因此分组改为显式字段，两种写法各归各位。
type LoaderEntry struct {
	// ID 是稳定身份；为空时由 [Loader] 自动生成（`entry-N`，子节点带父前缀）。
	ID string
	// Group 为真表示分组节点：忽略 Name，只递归加载 Children。
	Group bool
	// Name 是插件名（注册表键）。
	Name string
	// Config 原样传给插件工厂。
	Config any
	// Disabled 只禁用本节点，不影响分组语义。
	Disabled bool
	// Children 是子节点。
	Children []LoaderEntry
}

// JSON 返回与 Dart 版 `toJson` 相同的投影：空字段不出现。
//
// 与 [EntryFromJSON] 构成逐字段往返：`name` 只在非分组时出现，因此
// 「分组」这一位也能往返。
func (e LoaderEntry) JSON() map[string]any {
	out := map[string]any{}
	if e.ID != "" {
		out["id"] = e.ID
	}
	if !e.Group {
		out["name"] = e.Name
	}
	if e.Config != nil {
		out["config"] = e.Config
	}
	if e.Disabled {
		out["disabled"] = true
	}
	if len(e.Children) > 0 {
		// 用 []any 而不是 []map[string]any：这才是 `encoding/json` 解码任意 JSON 时
		// 会产出的形状，因此 [EntryFromJSON] 能吃自己的输出（Dart 里 fromJson(toJson())
		// 成立的往返，在这里也必须成立），JSON 结果也能直接交给 json.Marshal。
		children := make([]any, 0, len(e.Children))
		for _, child := range e.Children {
			children = append(children, child.JSON())
		}
		out["children"] = children
	}
	return out
}

// EntryFromJSON 从解码后的 JSON 对象还原节点。
//
// raw 是 `encoding/json` 的产物（对象 → `map[string]any`，数组 → `[]any`）；
// 缺 `name` 键即分组。字段类型不符返回错误（Dart 侧会抛 TypeError）。
func EntryFromJSON(raw map[string]any) (LoaderEntry, error) {
	entry := LoaderEntry{Group: true, Config: raw["config"]}
	if err := applyIdentity(&entry, raw); err != nil {
		return entry, err
	}
	if err := applyDisabled(&entry, raw); err != nil {
		return entry, err
	}
	return entry, applyChildren(&entry, raw)
}

func applyIdentity(entry *LoaderEntry, raw map[string]any) error {
	id, _, err := optionalField[string](raw, "id")
	if err != nil {
		return err
	}
	entry.ID = id
	name, nameFound, err := optionalField[string](raw, "name")
	if err != nil {
		return err
	}
	entry.Group = !nameFound
	entry.Name = name
	return nil
}

func applyDisabled(entry *LoaderEntry, raw map[string]any) error {
	disabled, found, err := optionalField[bool](raw, "disabled")
	if err != nil {
		return err
	}
	entry.Disabled = found && disabled
	return nil
}

func applyChildren(entry *LoaderEntry, raw map[string]any) error {
	items, found, err := optionalField[[]any](raw, "children")
	if err != nil || !found {
		return err
	}
	children := make([]LoaderEntry, 0, len(items))
	for _, item := range items {
		child, err := childFromJSON(item)
		if err != nil {
			return err
		}
		children = append(children, child)
	}
	entry.Children = children
	return nil
}

func childFromJSON(item any) (LoaderEntry, error) {
	object, ok := item.(map[string]any)
	if !ok {
		return LoaderEntry{}, fmt.Errorf("loader: children 的元素必须是对象，实际 %T", item)
	}
	return EntryFromJSON(object)
}

// optionalField 取可选字段：键缺失或值为 null 时 found 为 false；类型不符返回错误。
func optionalField[T any](raw map[string]any, key string) (T, bool, error) {
	var zero T
	value, present := raw[key]
	if !present || value == nil {
		return zero, false, nil
	}
	typed, ok := value.(T)
	if !ok {
		return zero, false, fmt.Errorf("loader: %s 必须是 %T，实际 %T", key, zero, value)
	}
	return typed, true, nil
}
