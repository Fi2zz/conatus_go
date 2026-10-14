package core

import "strings"

// sensitiveTokens 是敏感键名的规范化词元：键名按非字母数字拆分并转小写后，
// 命中其一即视为敏感。
var sensitiveTokens = map[string]struct{}{
	"key":           {},
	"apikey":        {},
	"token":         {},
	"secret":        {},
	"password":      {},
	"passwd":        {},
	"authorization": {},
	"credential":    {},
	"credentials":   {},
	"privatekey":    {},
	"accesskey":     {},
	"secretkey":     {},
}

// IsSensitiveKey 判断键名是否类似凭据字段。
//
// 同时兼容下划线、连字符与驼峰（`API_KEY` / `api-key` / `apiKey`），大小写不敏感。
func IsSensitiveKey(key string) bool {
	normalized := normalizeKey(key)
	if normalized == "" {
		return false
	}
	if isSensitiveToken(normalized) {
		return true
	}
	// 下划线/连字符风格：任意一段命中即敏感（`ARK_API_KEY` → `ark` + `api` + `key`）。
	return hasSensitivePart(key)
}

// MaskSecret 返回凭据的脱敏表示：前 4 位 + `...` + 后 4 位。
//
// 长度不足 8 时不做局部保留，直接以等长星号遮蔽，避免短密钥被几乎完整暴露。
func MaskSecret(value string) string {
	runes := []rune(value)
	if len(runes) <= 8 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:4]) + "..." + string(runes[len(runes)-4:])
}

// RedactSecrets 递归脱敏：键名命中 [IsSensitiveKey] 的字符串值会被 [MaskSecret]
// 遮蔽，非字符串的敏感值（如数字）一律遮蔽为 `***`，避免结构化凭据外泄。
//
// 不修改入参，返回新的 map / slice；其余标量原样返回。识别的结构体是 JSON 形态，
// 即 `map[string]any` 与 `[]any`（键为非字符串的 map 原样返回）。
func RedactSecrets(value any) any {
	if fields, ok := value.(map[string]any); ok {
		return redactMap(fields)
	}
	if items, ok := value.([]any); ok {
		return redactSlice(items)
	}
	return value
}

func isSensitiveToken(token string) bool {
	if _, ok := sensitiveTokens[token]; ok {
		return true
	}
	// 简单复数：`keys` / `tokens` / `secrets`。
	if !strings.HasSuffix(token, "s") {
		return false
	}
	_, ok := sensitiveTokens[token[:len(token)-1]]
	return ok
}

func isAlphanumeric(r rune) bool {
	if r >= 'a' && r <= 'z' {
		return true
	}
	return r >= '0' && r <= '9'
}

func normalizeKey(key string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(key) {
		if isAlphanumeric(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func hasSensitivePart(key string) bool {
	parts := strings.FieldsFunc(strings.ToLower(key), func(r rune) bool {
		return !isAlphanumeric(r)
	})
	for _, part := range parts {
		if isSensitiveToken(part) {
			return true
		}
	}
	return false
}

func redactMap(fields map[string]any) map[string]any {
	result := make(map[string]any, len(fields))
	for key, item := range fields {
		if IsSensitiveKey(key) {
			result[key] = maskValue(item)
			continue
		}
		result[key] = RedactSecrets(item)
	}
	return result
}

func redactSlice(items []any) []any {
	result := make([]any, len(items))
	for i, item := range items {
		result[i] = RedactSecrets(item)
	}
	return result
}

func maskValue(value any) any {
	if value == nil {
		return nil
	}
	if text, ok := value.(string); ok {
		return MaskSecret(text)
	}
	return "***"
}
