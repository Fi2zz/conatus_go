package tools

import (
	"encoding/json"
	"math"
	"reflect"
)

// asNumber 把 Go 的各种数字形态归一成 float64。
//
// 为什么需要放宽：Dart 的 jsonDecode 保留 int 与 double 之分（因此那边
// `value is int` 的严格判断成立），而 Go 的 encoding/json 默认把数字解成 float64、
// 或用 UseNumber 解成 json.Number。统一接受两者（含整数值的浮点），否则模型传来的
// `5` 会被判成「类型不符」。
// REASON: 数字类型归一是一张静态映射表，拆开反而更难读。
func asNumber(value any) (float64, bool) {
	if number, ok := value.(json.Number); ok {
		parsed, err := number.Float64()
		return parsed, err == nil
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(reflected.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(reflected.Uint()), true
	case reflect.Float32, reflect.Float64:
		return reflected.Float(), true
	default:
		return 0, false
	}
}

// asInteger 在 [asNumber] 之上要求取值是整数值（3.5 之类不算）。
func asInteger(value any) (int64, bool) {
	number, ok := asNumber(value)
	if !ok {
		return 0, false
	}
	truncated := math.Trunc(number)
	if truncated != number {
		return 0, false
	}
	return int64(truncated), true
}

// asString 只接受字符串（不做隐式转换：工具参数的类型错了就该报错）。
func asString(value any) (string, bool) {
	text, ok := value.(string)
	return text, ok
}

// asBoolean 只接受布尔。
func asBoolean(value any) (bool, bool) {
	flag, ok := value.(bool)
	return flag, ok
}

// asArray 只接受数组。
func asArray(value any) ([]any, bool) {
	items, ok := value.([]any)
	return items, ok
}

// asObject 只接受字符串键的映射。
func asObject(value any) (map[string]any, bool) {
	fields, ok := value.(map[string]any)
	return fields, ok
}
