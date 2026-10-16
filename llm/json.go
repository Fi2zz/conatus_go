package llm

import "encoding/json"

// 本文件是 JSON 取值的归一：帧与请求体都从 map[string]any 里取字段，取值口径必须一致。
//
// Go 侧与 Dart 有一处固定差异：encoding/json 把数字一律解成 float64（Dart 保留 int 与
// double 之分），所以整数字段（如 `tool_calls[].index`）也按 float64 判定。

// decodeFrame 把载荷解成 JSON 对象；非法 JSON、数组、标量与 null 都返回 ok=false。
func decodeFrame(payload string) (map[string]any, bool) {
	var frame map[string]any
	if err := json.Unmarshal([]byte(payload), &frame); err != nil {
		return nil, false
	}
	if frame == nil {
		return nil, false
	}
	return frame, true
}

// objectOf 把值取成 JSON 对象；不是对象时 ok 为 false。
func objectOf(value any) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	return object, ok
}

// textOf 把值取成字符串；不是字符串时返回空串（配合「空串不覆盖已有值」的语义使用）。
func textOf(value any) string {
	text, _ := value.(string)
	return text
}

// nonEmptyString 取非空字符串；空串一律当作「这个字段没来」。
func nonEmptyString(value any) (string, bool) {
	text, ok := value.(string)
	if !ok || text == "" {
		return "", false
	}
	return text, true
}

// defaultText 空串时取缺省值。
func defaultText(text, fallback string) string {
	if text == "" {
		return fallback
	}
	return text
}

// integralOf 取整数值：JSON 数字解出的是 float64（见文件头说明）。
func integralOf(value any) (int, bool) {
	number, ok := value.(float64)
	if !ok {
		return 0, false
	}
	return int(number), true
}
