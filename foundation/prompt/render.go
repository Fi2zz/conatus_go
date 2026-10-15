package prompt

import (
	"regexp"
	"strings"
)

// DefaultSeparator 是拼接段与上下文时的缺省分隔符。
const DefaultSeparator = "\n\n"

// RenderOption 定制 [Registry.Render] 与 [Registry.RenderContexts]。
type RenderOption func(*renderSettings)

type renderSettings struct{ separator string }

// WithSeparator 指定拼接分隔符（缺省 [DefaultSeparator]）。
func WithSeparator(separator string) RenderOption {
	return func(s *renderSettings) { s.separator = separator }
}

// Render 把 prompt 段渲染成一段文本，并插值 `{{variable}}`。
func (r *Registry) Render(assembly Assembly, opts ...RenderOption) string {
	settings := renderSettings{separator: DefaultSeparator}
	for _, opt := range opts {
		opt(&settings)
	}
	texts := make([]string, 0, len(assembly.Sections))
	for _, section := range assembly.Sections {
		texts = append(texts, Interpolate(section.Text, assembly.Variables))
	}
	return strings.Join(texts, settings.separator)
}

// RenderContexts 把动态上下文渲染成一段文本，并插值 `{{variable}}`；
// 求值后为空的上下文不贡献内容（也不会产生多余分隔符）。
func (r *Registry) RenderContexts(assembly Assembly, opts ...RenderOption) string {
	settings := renderSettings{separator: DefaultSeparator}
	for _, opt := range opts {
		opt(&settings)
	}
	texts := make([]string, 0, len(assembly.Contexts))
	for _, context := range assembly.Contexts {
		text := Interpolate(context.Text, assembly.Variables)
		if text == "" {
			continue
		}
		texts = append(texts, text)
	}
	return strings.Join(texts, settings.separator)
}

var placeholder = regexp.MustCompile(`\{\{(\w+)\}\}`)

// Interpolate 把 `{{name}}` 替换为 variables 中的值；未知占位符原样保留。
func Interpolate(text string, variables map[string]string) string {
	if len(variables) == 0 {
		return text
	}
	return placeholder.ReplaceAllStringFunc(text, func(match string) string {
		key := placeholder.FindStringSubmatch(match)[1]
		value, found := variables[key]
		if !found {
			return match
		}
		return value
	})
}
