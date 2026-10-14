package core_test

import (
	"reflect"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
)

func TestMaskSecret(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"空串", "", ""},
		{"短密钥整串星号", "abc", "***"},
		{"边界 8 位整串星号", "abcdefgh", "********"},
		{"9 位保留首尾各 4", "abcdefghi", "abcd...fghi"},
		{"长密钥", "ARK-sk-1234567890abcd", "ARK-...abcd"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := core.MaskSecret(c.value); got != c.want {
				t.Fatalf("MaskSecret(%q) = %q，期望 %q", c.value, got, c.want)
			}
		})
	}
}

func TestIsSensitiveKeyMatchesStyles(t *testing.T) {
	sensitive := []string{
		"key", "keys", "token", "tokens", "secret", "secrets", "password",
		"passwd", "authorization", "credential", "credentials",
		"API_KEY", "ARK_API_KEY", "apiKey", "api-key", "api_key",
		"accessKey", "privateKey", "secretkey", "Authorization-Token",
	}
	for _, key := range sensitive {
		if !core.IsSensitiveKey(key) {
			t.Errorf("IsSensitiveKey(%q) = false，期望 true", key)
		}
	}
}

func TestIsSensitiveKeyRejectsOrdinaryKeys(t *testing.T) {
	ordinary := []string{
		"", "-", "name", "count", "monkey", "hockey", "keynote", "moneys",
		"users", "session_id", "created_at", "note",
	}
	for _, key := range ordinary {
		if core.IsSensitiveKey(key) {
			t.Errorf("IsSensitiveKey(%q) = true，期望 false", key)
		}
	}
}

func TestRedactSecretsNested(t *testing.T) {
	input := map[string]any{
		"model": "doubao",
		"headers": map[string]any{
			"Authorization": "Bearer sk-1234567890abcdef",
			"content-type":  "application/json",
		},
		"retries":   2,
		"api_key":   "abcdefgh",
		"nil_token": nil,
		"attempts":  []any{1, map[string]any{"token": "sk-aaaaaaaaaaaa"}},
	}
	got := core.RedactSecrets(input).(map[string]any)

	if got["model"] != "doubao" {
		t.Errorf("非敏感值被改动：%v", got["model"])
	}
	if got["retries"] != 2 {
		t.Errorf("非字符串敏感值应原样保留，得到 %v", got["retries"])
	}
	if got["api_key"] != "********" {
		t.Errorf("api_key 未按短密钥规则遮蔽：%v", got["api_key"])
	}
	if got["nil_token"] != nil {
		t.Errorf("nil 敏感值应保持 nil，得到 %v", got["nil_token"])
	}
	headers := got["headers"].(map[string]any)
	if headers["Authorization"] != "Bear...cdef" {
		t.Errorf("嵌套 Authorization 未遮蔽：%v", headers["Authorization"])
	}
	if headers["content-type"] != "application/json" {
		t.Errorf("非敏感头部被改动：%v", headers["content-type"])
	}
	attempts := got["attempts"].([]any)
	nested := attempts[1].(map[string]any)
	if nested["token"] != "sk-a...aaaa" {
		t.Errorf("列表内嵌套 map 未遮蔽：%v", nested["token"])
	}
}

func TestRedactSecretsKeepsNonStringSensitiveAsStars(t *testing.T) {
	got := core.RedactSecrets(map[string]any{"port_key": 8848}).(map[string]any)
	if got["port_key"] != "***" {
		t.Fatalf("非字符串敏感值应遮蔽为 ***，得到 %v", got["port_key"])
	}
}

func TestRedactSecretsDoesNotMutateInput(t *testing.T) {
	input := map[string]any{"api_key": "sk-plaintext-123456"}
	_ = core.RedactSecrets(input)
	if input["api_key"] != "sk-plaintext-123456" {
		t.Fatalf("入参被改写：%v", input["api_key"])
	}
}

func TestRedactSecretsPassesThroughScalarsAndUnknownShapes(t *testing.T) {
	if got := core.RedactSecrets("plain"); got != "plain" {
		t.Fatalf("标量应原样返回，得到 %v", got)
	}
	nonStringKeyed := map[int]any{1: "x"}
	if got := core.RedactSecrets(nonStringKeyed); !reflect.DeepEqual(got, nonStringKeyed) {
		t.Fatalf("非字符串键 map 应原样返回，得到 %v", got)
	}
}
