package core_test

import (
	"testing"

	"github.com/Fi2zz/conatus_go/core"
)

// assertPanics 断言 body 触发 panic——用于钉住「构造参数非法属编程错误」的边界。
func assertPanics(t *testing.T, name string, body func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%s：期望 panic，但没有", name)
		}
	}()
	body()
}

// mustProvide 提供服务并在失败时立刻结束用例。
func mustProvide(t *testing.T, ctx *core.Context, key string, value any) core.Disposer {
	t.Helper()
	disposer, err := ctx.Provide(key, value)
	if err != nil {
		t.Fatalf("provide(%q) 失败：%v", key, err)
	}
	return disposer
}
