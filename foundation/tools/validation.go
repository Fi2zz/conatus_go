package tools

import (
	"fmt"
	"strings"
)

// ValidateArgs 校验 args 是否满足 tool 的参数声明，返回违规说明列表；空列表表示通过。
//
// 校验覆盖必填、类型与枚举取值，并递归检查数组元素与嵌套对象字段。违规文案与 Dart
// 版逐字对齐（中文、路径形如 `a.b[0]`），对等测试据此断言。
func ValidateArgs(tool Tool, args map[string]any) []string {
	violations := []string{}
	for _, spec := range ParamsOf(tool) {
		if issue := checkProperty(spec, args, spec.Name); issue != "" {
			violations = append(violations, issue)
		}
	}
	return violations
}

func checkProperty(spec *ParamSpec, container map[string]any, path string) string {
	value, present := container[spec.Name]
	if !present {
		return requiredIssue(spec, fmt.Sprintf("缺少必填参数 %q", path))
	}
	if value == nil {
		return requiredIssue(spec, fmt.Sprintf("参数 %q 不能为 null", path))
	}
	return checkValue(spec, value, path)
}

// requiredIssue 只在参数必填时才把问题报出来。
func requiredIssue(spec *ParamSpec, issue string) string {
	if spec.Required {
		return issue
	}
	return ""
}

// checkValue 按声明类型校验取值。
// REASON: 七种参数类型的一一映射表，拆开反而更难与 Dart 版逐条对照。
func checkValue(spec *ParamSpec, value any, path string) string {
	switch spec.Type {
	case ParamString:
		if _, ok := asString(value); !ok {
			return expectType(path, "string")
		}
		return ""
	case ParamInteger:
		if _, ok := asInteger(value); !ok {
			return expectType(path, "integer")
		}
		return ""
	case ParamNumber:
		if _, ok := asNumber(value); !ok {
			return expectType(path, "number")
		}
		return ""
	case ParamBoolean:
		if _, ok := asBoolean(value); !ok {
			return expectType(path, "boolean")
		}
		return ""
	case ParamEnumeration:
		return checkEnum(spec, value, path)
	case ParamArray:
		return checkArray(spec, value, path)
	default:
		return checkObject(spec, value, path)
	}
}

func expectType(path, expected string) string {
	return fmt.Sprintf("参数 %q 期望 %s", path, expected)
}

func checkEnum(spec *ParamSpec, value any, path string) string {
	text, ok := asString(value)
	if !ok {
		return fmt.Sprintf("参数 %q 期望 string 枚举", path)
	}
	for _, allowed := range spec.EnumValues {
		if text == allowed {
			return ""
		}
	}
	return fmt.Sprintf("参数 %q 取值不在 [%s] 内", path, strings.Join(spec.EnumValues, ", "))
}

func checkArray(spec *ParamSpec, value any, path string) string {
	items, ok := value.([]any)
	if !ok {
		return fmt.Sprintf("参数 %q 期望 array", path)
	}
	if spec.Items == nil {
		return ""
	}
	for index, item := range items {
		itemPath := fmt.Sprintf("%s[%d]", path, index)
		if item == nil {
			if spec.Items.Required {
				return fmt.Sprintf("参数 %q 不能为 null", itemPath)
			}
			continue
		}
		if issue := checkValue(spec.Items, item, itemPath); issue != "" {
			return issue
		}
	}
	return ""
}

func checkObject(spec *ParamSpec, value any, path string) string {
	fields, ok := value.(map[string]any)
	if !ok {
		return fmt.Sprintf("参数 %q 期望 object", path)
	}
	for _, child := range spec.Properties {
		if issue := checkProperty(child, fields, path+"."+child.Name); issue != "" {
			return issue
		}
	}
	return ""
}
