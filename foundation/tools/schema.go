package tools

// ParameterSchema 把参数声明列表编译为模型可读的 `type: object` schema。
//
// 顶层 required 聚合标记必填的参数名；无必填项时不输出该字段。未声明参数时
// properties 为空对象——schema 本身仍然合法。
func ParameterSchema(params []*ParamSpec) map[string]any {
	properties := make(map[string]any, len(params))
	required := make([]string, 0, len(params))
	for _, param := range params {
		properties[param.Name] = param.ToJSON()
		if param.Required {
			required = append(required, param.Name)
		}
	}
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}
