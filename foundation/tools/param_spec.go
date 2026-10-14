package tools

// ParamType 是参数类型。
type ParamType int

const (
	// ParamString 字符串。
	ParamString ParamType = iota
	// ParamInteger 整数。
	ParamInteger
	// ParamNumber 数字（整数或小数）。
	ParamNumber
	// ParamBoolean 布尔。
	ParamBoolean
	// ParamEnumeration 字符串枚举。
	ParamEnumeration
	// ParamArray 数组（元素类型由 Items 声明）。
	ParamArray
	// ParamObject 对象（嵌套字段由 Properties 声明）。
	ParamObject
)

var paramTypeNames = [...]string{
	"string", "integer", "number", "boolean", "string", "array", "object",
}

// schemaTypeName 返回该类型在 JSON Schema 里的 type 名（枚举投影成 string）。
func (t ParamType) schemaTypeName() string {
	if int(t) < 0 || int(t) >= len(paramTypeNames) {
		return "string"
	}
	return paramTypeNames[t]
}

// ParamSpec 是一个参数的声明。
//
// 字段对外只读：构造请走 [StringParam] / [IntegerParam] / [NumberParam] /
// [BooleanParam] / [EnumParam] / [ArrayParam] / [ObjectParam]，
// 它们会校验「枚举取值不能为空」「数组/对象必须给出元素或字段声明」。
type ParamSpec struct {
	Name        string
	Type        ParamType
	Description string
	Required    bool
	EnumValues  []string
	Items       *ParamSpec
	Properties  map[string]*ParamSpec
	Default     any
}

// ToJSON 把声明编译为单个 JSON Schema 片段。
//
// 嵌套对象的 required 由其子声明的 Required 聚合；Default 为 nil 时不输出。
func (p *ParamSpec) ToJSON() map[string]any {
	fields := map[string]any{"type": p.Type.schemaTypeName()}
	if p.Type == ParamEnumeration {
		fields["enum"] = p.EnumValues
	}
	if p.Type == ParamArray && p.Items != nil {
		fields["items"] = p.Items.ToJSON()
	}
	if p.Type == ParamObject {
		properties := make(map[string]any, len(p.Properties))
		for name, property := range p.Properties {
			properties[name] = property.ToJSON()
		}
		fields["properties"] = properties
	}
	if required := p.requiredProperties(); len(required) > 0 {
		fields["required"] = required
	}
	if p.Description != "" {
		fields["description"] = p.Description
	}
	if p.Default != nil {
		fields["default"] = p.Default
	}
	return fields
}

func (p *ParamSpec) requiredProperties() []string {
	required := make([]string, 0, len(p.Properties))
	for _, property := range p.Properties {
		if property.Required {
			required = append(required, property.Name)
		}
	}
	return required
}
