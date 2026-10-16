package agent

import "github.com/Fi2zz/conatus_go/foundation/prompt"

// BuildSystemText 组装 system 文本：prompt 段 + 动态上下文；没有注册表时退回兜底人设。
//
// 与 Dart 的差异：Dart 那一版还会拼「历史摘要」「当前计划」「相关记忆」三段，它们分别
// 依赖压缩器、plan 与记忆库，Go 侧都还没落地；那三段补齐时加在这里，因此本函数现在
// 不收 userInput（它只被「相关记忆召回」用）。
func BuildSystemText(defaultSystemPrompt string, registry *prompt.Registry) string {
	if registry == nil {
		return defaultSystemPrompt
	}
	assembly := registry.Assemble(nil)
	return joinBlocks(registry.Render(assembly), registry.RenderContexts(assembly))
}

// joinBlocks 拼接两段文本：后一段为空时不留下孤立的空行。
func joinBlocks(text, contexts string) string {
	if contexts == "" {
		return text
	}
	return text + "\n\n" + contexts
}
