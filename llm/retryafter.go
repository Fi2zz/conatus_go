package llm

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ParseRetryAfter 解析 `Retry-After` 响应头；两种合法形态都收，解析不了返回 0。
//
//   - delta-seconds：`Retry-After: 30`；
//   - HTTP-date：`Retry-After: Wed, 21 Oct 2026 07:28:00 GMT`。
//
// 头缺失 / 非法 / 非正数 / 指向过去都返回 0，含义是「服务端没说」——交给退避策略用自己的节奏。
func ParseRetryAfter(header string) time.Duration {
	return ParseRetryAfterAt(header, time.Now())
}

// ParseRetryAfterAt 是 [ParseRetryAfter] 的可注入时钟形态。
//
// HTTP-date 形态要拿「现在」才能算差值，时间敏感的用例靠它保持确定性（D-T1-8：时间敏感件
// 不许有裸 now）。
func ParseRetryAfterAt(header string, now time.Time) time.Duration {
	value := strings.TrimSpace(header)
	if value == "" {
		return 0
	}
	if seconds, ok := retryAfterSeconds(value); ok {
		return seconds
	}
	return httpDateDelay(value, now)
}

// retryAfterSeconds 解析 delta-seconds 形态；非法或非正数返回 ok=false。
func retryAfterSeconds(value string) (time.Duration, bool) {
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

// httpDateDelay 解析 HTTP-date 形态；非法或指向过去返回 0。
func httpDateDelay(value string, now time.Time) time.Duration {
	at, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	delta := at.Sub(now)
	if delta < 0 {
		return 0
	}
	return delta
}
