package home

import (
	"crypto/rand"
	"strings"
)

const hexDigits = "0123456789abcdef"

// NewUUIDv4 生成一个随机 UUID v4：小写十六进制、`8-4-4-4-12` 分段。
//
// 用于会话 id 等需要全局唯一标识、且不希望引入依赖的场景。随机源不可用属环境级
// 故障（无法降级成「可用的标识」），因此 panic 而不是返回错误。
func NewUUIDv4() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic("home: 生成 UUID 失败：" + err.Error())
	}
	raw[6] = (raw[6] & 0x0f) | 0x40 // version 4
	raw[8] = (raw[8] & 0x3f) | 0x80 // variant 10xx
	return formatUUID(raw)
}

func formatUUID(raw [16]byte) string {
	var builder strings.Builder
	builder.Grow(36)
	for index, value := range raw {
		if index == 4 || index == 6 || index == 8 || index == 10 {
			builder.WriteByte('-')
		}
		builder.WriteByte(hexDigits[value>>4])
		builder.WriteByte(hexDigits[value&0x0f])
	}
	return builder.String()
}
