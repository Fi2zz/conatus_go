package tools

import "fmt"

// ToolArgumentError 在必填参数缺失、为 null 或类型不符时返回。
//
// 与 Dart 版同名同形（`ToolArgumentException: <message>`），便于两端对等断言。
type ToolArgumentError struct {
	Message string
}

func (e *ToolArgumentError) Error() string { return "ToolArgumentException: " + e.Message }

// ToolContext 是一次工具调用的类型安全参数视图。
type ToolContext struct {
	call ToolCall
}

// NewToolContext 用一次调用构造参数视图。
func NewToolContext(call ToolCall) *ToolContext { return &ToolContext{call: call} }

// CallID 返回调用标识。
func (c *ToolContext) CallID() string { return c.call.CallID }

// Arguments 返回解析后的参数对象（只读意图，调用方不应改写）。
func (c *ToolContext) Arguments() map[string]any { return c.call.Arguments }

// Has 返回参数是否存在（值可为 null）。
func (c *ToolContext) Has(name string) bool {
	_, present := c.call.Arguments[name]
	return present
}

// Value 返回原始参数值。
func (c *ToolContext) Value(name string) any { return c.call.Arguments[name] }

// String 读取可选字符串；缺失返回零值与 false，类型不符也只返回 false。
func (c *ToolContext) String(name string) (string, bool) {
	return optional(c.Value(name), asString)
}

// Integer 读取可选整数（接受整数值的数字，见 [asInteger]）。
func (c *ToolContext) Integer(name string) (int64, bool) {
	return optional(c.Value(name), asInteger)
}

// Number 读取可选数字（整数也接受，统一为 float64）。
func (c *ToolContext) Number(name string) (float64, bool) {
	return optional(c.Value(name), asNumber)
}

// Boolean 读取可选布尔。
func (c *ToolContext) Boolean(name string) (bool, bool) {
	return optional(c.Value(name), asBoolean)
}

// Array 读取可选数组。
func (c *ToolContext) Array(name string) ([]any, bool) {
	return optional(c.Value(name), asArray)
}

// Object 读取可选对象。
func (c *ToolContext) Object(name string) (map[string]any, bool) {
	return optional(c.Value(name), asObject)
}

// RequireString 读取必填字符串；缺失、为 null 或类型不符返回 [ToolArgumentError]。
func (c *ToolContext) RequireString(name string) (string, error) {
	value, err := c.requireValue(name)
	if err != nil {
		return "", err
	}
	text, ok := asString(value)
	if !ok {
		return "", typeError(name, "string", value)
	}
	return text, nil
}

// RequireInteger 读取必填整数。
func (c *ToolContext) RequireInteger(name string) (int64, error) {
	value, err := c.requireValue(name)
	if err != nil {
		return 0, err
	}
	number, ok := asInteger(value)
	if !ok {
		return 0, typeError(name, "int", value)
	}
	return number, nil
}

// RequireNumber 读取必填数字。
func (c *ToolContext) RequireNumber(name string) (float64, error) {
	value, err := c.requireValue(name)
	if err != nil {
		return 0, err
	}
	number, ok := asNumber(value)
	if !ok {
		return 0, typeError(name, "number", value)
	}
	return number, nil
}

// RequireBoolean 读取必填布尔。
func (c *ToolContext) RequireBoolean(name string) (bool, error) {
	value, err := c.requireValue(name)
	if err != nil {
		return false, err
	}
	flag, ok := asBoolean(value)
	if !ok {
		return false, typeError(name, "bool", value)
	}
	return flag, nil
}

// Get 读取任意类型的可选参数（不做数字归一，需要归一时用对应的类型化取值）。
func Get[T any](c *ToolContext, name string) (T, bool) {
	var zero T
	value := c.Value(name)
	if value == nil {
		return zero, false
	}
	typed, ok := value.(T)
	return typed, ok
}

// Require 读取任意类型的必填参数；失败返回 [ToolArgumentError]。
func Require[T any](c *ToolContext, name string) (T, error) {
	var zero T
	value, err := c.requireValue(name)
	if err != nil {
		return zero, err
	}
	typed, ok := value.(T)
	if !ok {
		return zero, typeError(name, typeNameOf(zero), value)
	}
	return typed, nil
}

func (c *ToolContext) requireValue(name string) (any, error) {
	value, present := c.call.Arguments[name]
	if !present {
		return nil, &ToolArgumentError{Message: fmt.Sprintf("缺少必填参数 %q", name)}
	}
	if value == nil {
		return nil, &ToolArgumentError{Message: fmt.Sprintf("参数 %q 不能为 null", name)}
	}
	return value, nil
}

func optional[T any](value any, convert func(any) (T, bool)) (T, bool) {
	if value == nil {
		var zero T
		return zero, false
	}
	return convert(value)
}

func typeError(name, expected string, value any) error {
	return &ToolArgumentError{
		Message: fmt.Sprintf("参数 %q 期望 %s，实际 %T", name, expected, value),
	}
}

func typeNameOf[T any](zero T) string {
	return fmt.Sprintf("%T", zero)
}
