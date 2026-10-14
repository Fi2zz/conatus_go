package tools

// ParamOption 定制参数声明。
type ParamOption func(*ParamSpec)

// Required 把参数标记为必填。
func Required() ParamOption {
	return func(p *ParamSpec) { p.Required = true }
}

// Describe 给出面向模型的说明。
func Describe(text string) ParamOption {
	return func(p *ParamSpec) { p.Description = text }
}

// WithDefault 声明默认值（作为 schema 注解，不参与校验）。
func WithDefault(value any) ParamOption {
	return func(p *ParamSpec) { p.Default = value }
}

// StringParam 声明字符串参数。
func StringParam(name string, opts ...ParamOption) *ParamSpec {
	return newParam(name, ParamString, opts)
}

// IntegerParam 声明整数参数。
func IntegerParam(name string, opts ...ParamOption) *ParamSpec {
	return newParam(name, ParamInteger, opts)
}

// NumberParam 声明数字参数（整数或小数）。
func NumberParam(name string, opts ...ParamOption) *ParamSpec {
	return newParam(name, ParamNumber, opts)
}

// BooleanParam 声明布尔参数。
func BooleanParam(name string, opts ...ParamOption) *ParamSpec {
	return newParam(name, ParamBoolean, opts)
}

// EnumParam 声明字符串枚举参数；values 不能为空（空则 panic，属编程错误）。
func EnumParam(name string, values []string, opts ...ParamOption) *ParamSpec {
	if len(values) == 0 {
		panic("tools: 枚举参数的取值不能为空（" + name + "）")
	}
	spec := newParam(name, ParamEnumeration, opts)
	spec.EnumValues = append([]string(nil), values...)
	return spec
}

// ArrayParam 声明数组参数；items 声明元素类型。
func ArrayParam(name string, items *ParamSpec, opts ...ParamOption) *ParamSpec {
	if items == nil {
		panic("tools: 数组参数必须给出元素声明（" + name + "）")
	}
	spec := newParam(name, ParamArray, opts)
	spec.Items = items
	return spec
}

// ObjectParam 声明对象参数；properties 声明嵌套字段。
func ObjectParam(name string, properties map[string]*ParamSpec, opts ...ParamOption) *ParamSpec {
	if len(properties) == 0 {
		panic("tools: 对象参数必须给出字段声明（" + name + "）")
	}
	spec := newParam(name, ParamObject, opts)
	spec.Properties = properties
	return spec
}

func newParam(name string, kind ParamType, opts []ParamOption) *ParamSpec {
	spec := &ParamSpec{Name: name, Type: kind}
	for _, opt := range opts {
		opt(spec)
	}
	return spec
}
