package llm_test

import (
	"testing"
	"time"

	"github.com/Fi2zz/conatus_go/llm"
)

func TestParseRetryAfterReadsDeltaSeconds(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{"秒数", "30", 30 * time.Second},
		{"两侧空白", "  12 ", 12 * time.Second},
		{"零等于没说", "0", 0},
		{"负数等于没说", "-5", 0},
		{"空头", "", 0},
		{"纯空白", "   ", 0},
		{"非法文本", "soon", 0},
	}
	for _, item := range cases {
		if got := llm.ParseRetryAfter(item.header); got != item.want {
			t.Fatalf("%s：ParseRetryAfter(%q) = %v，期望 %v", item.name, item.header, got, item.want)
		}
	}
}

func TestParseRetryAfterAtReadsHTTPDate(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{"未来 90 秒", "Thu, 15 Oct 2026 12:01:30 GMT", 90 * time.Second},
		{"恰好等于现在", "Thu, 15 Oct 2026 12:00:00 GMT", 0},
		{"指向过去等于没说", "Thu, 15 Oct 2026 11:59:00 GMT", 0},
		{"非法日期", "2026-10-15T12:01:30Z", 0},
		{"残缺日期", "Thu, 15 Oct 2026", 0},
	}
	for _, item := range cases {
		if got := llm.ParseRetryAfterAt(item.header, now); got != item.want {
			t.Fatalf("%s：ParseRetryAfterAt(%q) = %v，期望 %v", item.name, item.header, got, item.want)
		}
	}
}
