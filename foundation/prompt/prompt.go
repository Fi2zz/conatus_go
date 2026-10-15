// Package prompt 是 system prompt 的段与动态上下文注册表（对应 Dart 的
// system-prompt 与 time-context 两个插件）。
//
// 各插件在各自的上下文里注册段或上下文（都返回 [core.Disposer]，可交给
// `ctx.Track(...)` 随插件卸载自动撤销）；[Registry.Assemble] 按 order 升序求值，
// [Registry.Render] / [Registry.RenderContexts] 分别拼接段与动态上下文，并做
// `{{variable}}` 插值。
//
// 命名说明：[Context] 是「注入给模型的一段动态上下文」，与本仓的 [core.Context]
// （依赖注入上下文）无关。
package prompt

// Section 是一段可注册的 system prompt 段。
//
// Order 升序拼接，同序按 Name 的码位序；Text 是每次装配都重新求值的 provider
// （可引用 `{{variable}}` 占位符，由渲染阶段插值）。
type Section struct {
	Name  string
	Order int
	Text  func() string
}

// Context 是一份动态模型上下文，装配为独立贡献项。
//
// 名字重复注册会被拒绝；Text 返回空串时不贡献内容。
type Context struct {
	Name  string
	Order int
	Text  func() string
}

// AssembledSection 是已解析（尚未插值）的一段 prompt 段。
type AssembledSection struct {
	Name string
	Text string
}

// AssembledContext 是已解析（尚未插值）的一份动态上下文。
type AssembledContext struct {
	Name string
	Text string
}

// Assembly 是一次装配的完整结果：已排序的段、上下文与插值变量。
type Assembly struct {
	Sections  []AssembledSection
	Contexts  []AssembledContext
	Variables map[string]string
}
