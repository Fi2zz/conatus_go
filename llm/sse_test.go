package llm_test

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/Fi2zz/conatus_go/llm"
)

// gather 收集 SSE 载荷；读错误即失败。
func gather(t *testing.T, body string) []string {
	t.Helper()
	payloads := []string{}
	for payload, err := range llm.SSEPayloads(strings.NewReader(body)) {
		if err != nil {
			t.Fatalf("SSE 读取失败：%v", err)
		}
		payloads = append(payloads, payload)
	}
	return payloads
}

// 这些用例完全脱离网络：SSE 解析器只吃 io.Reader。
func TestSSEPayloadsSplitsFrames(t *testing.T) {
	longPayload := strings.Repeat("x", 200_000)
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"单帧", "data: {\"a\":1}\n\n", []string{`{"a":1}`}},
		{"连续多帧", "data: 一\n\ndata: 二\n\n", []string{"一", "二"}},
		{"末尾没有空行也产出", "data: 尾巴", []string{"尾巴"}},
		{"末尾只有一个换行也产出", "data: 尾\n", []string{"尾"}},
		{"空输入没有载荷", "", nil},
		{"只有空行没有载荷", "\n\n\n", nil},
		{"注释行跳过", ": 心跳\n\ndata: 有效\n\n", []string{"有效"}},
		{"event 与 id 字段跳过", "event: ping\nid: 7\nretry: 100\ndata: 有效\n\n", []string{"有效"}},
		{"data 后无空格", "data:{\"a\":1}\n\n", []string{`{"a":1}`}},
		{"data 后多个空格", "data:     {\"a\":1}\n\n", []string{`{"a":1}`}},
		{"多行 data 以换行拼接", "data: 甲\ndata: 乙\n\n", []string{"甲\n乙"}},
		{"CRLF 输入", "data: 一\r\n\r\n", []string{"一"}},
		{"行尾空白不落进载荷", "data: 一   \n\n", []string{"一"}},
		{"纯 data 行产出空载荷（由上层跳过）", "data:\n\n", []string{""}},
		{"超长单行不被截断", "data: " + longPayload + "\n\n", []string{longPayload}},
		{"DONE 只是收尾", "data: [DONE]\n\n", nil},
		{"DONE 前先产出未决内容", "data: 甲\ndata: [DONE]\n\n", []string{"甲"}},
		{"DONE 之后的帧不再产出", "data: 一\n\ndata: [DONE]\n\ndata: 二\n\n", []string{"一"}},
	}
	for _, item := range cases {
		if got := gather(t, item.body); !slices.Equal(got, item.want) {
			t.Fatalf("%s：载荷 = %q，期望 %q", item.name, got, item.want)
		}
	}
}

func TestSSEPayloadsReportsReadFailure(t *testing.T) {
	boom := errors.New("连接中断")
	body := io.MultiReader(strings.NewReader("data: 甲\n\n"), iotest.ErrReader(boom))

	payloads := []string{}
	var failure error
	for payload, err := range llm.SSEPayloads(body) {
		if err != nil {
			failure = err
			break
		}
		payloads = append(payloads, payload)
	}

	if !slices.Equal(payloads, []string{"甲"}) {
		t.Fatalf("出错前已拿到的载荷 = %q，期望 [甲]", payloads)
	}
	if !errors.Is(failure, boom) {
		t.Fatalf("错误 = %v，期望原样上报读失败", failure)
	}
}
