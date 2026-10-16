package llm

import (
	"bufio"
	"errors"
	"io"
	"iter"
	"strings"
	"unicode"
)

// SSE 帧的固定标记。
const (
	sseDataPrefix = "data:"
	sseDoneMarker = "[DONE]"
)

// SSEPayloads 把 SSE 字节流切成一帧一帧的 `data:` 载荷。
//
// 入口只吃 io.Reader、不碰网络：单测直接喂 strings.NewReader 即可。规则与 Dart 的
// `_sseData` 逐条对齐：
//
//   - 行尾空白先去掉；**空行是帧分隔符**，此刻才产出一帧；
//   - `:` 开头的注释行、`event:` / `id:` / `retry:` 等字段一律跳过（都不以 `data:` 开头）；
//   - 载荷取 `data:` 之后的内容并去掉前导空白；多行 data 用 `\n` 连接，产出前再去掉尾部空白；
//   - `[DONE]` 先产出未决内容，随后**结束整条流**——其后即便还有字节也不再产出；
//   - 流结束时未决缓冲仍要产出（服务端最后一帧可能没有收尾空行）。
//
// 载荷是不是合法 JSON 不在这里判断：服务端可能插心跳帧，交给调用方跳过。
func SSEPayloads(body io.Reader) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		scanner := &sseScanner{reader: bufio.NewReader(body)}
		for {
			payload, ok, err := scanner.next()
			if err != nil {
				yield("", err)
				return
			}
			if !ok {
				return
			}
			if !yield(payload, nil) {
				return
			}
		}
	}
}

// sseScanner 是 SSE 的拉取式读取器：按行喂缓冲，攒出一帧一帧的载荷。
//
// 用 bufio.Reader 而不是 bufio.Scanner：Scanner 默认 64KB 行长上限会让长帧变成读错误，
// 而工具参数分片攒长帧是常态。
type sseScanner struct {
	reader *bufio.Reader
	buffer sseBuffer
}

// next 返回下一个载荷；没有更多帧时 ok 为 false，err 只在底层读取失败时非 nil。
func (s *sseScanner) next() (string, bool, error) {
	for {
		line, err := s.reader.ReadString('\n')
		payload, ok := s.buffer.feed(line)
		if s.buffer.done {
			return payload, ok, nil
		}
		if err == nil {
			if ok {
				return payload, true, nil
			}
			continue
		}
		return s.finish(err, payload, ok)
	}
}

// finish 处理读取结束：EOF 时把未决缓冲产出，其余错误原样上报。
func (s *sseScanner) finish(err error, payload string, ok bool) (string, bool, error) {
	if !errors.Is(err, io.EOF) {
		return "", false, err
	}
	if ok {
		return payload, true, nil
	}
	flushed, has := s.buffer.flush()
	return flushed, has, nil
}

// sseBuffer 是帧缓冲：一帧可由多行 data 组成，空行才收尾。
type sseBuffer struct {
	lines []string
	done  bool
}

// feed 喂入一行，返回本行触发的产出（空行收尾或 `[DONE]` 截断）以及是否真的有产出。
func (b *sseBuffer) feed(line string) (string, bool) {
	trimmed := strings.TrimRightFunc(line, unicode.IsSpace)
	if trimmed == "" {
		return b.flush()
	}
	if !strings.HasPrefix(trimmed, sseDataPrefix) {
		return "", false
	}
	payload := strings.TrimLeftFunc(trimmed[len(sseDataPrefix):], unicode.IsSpace)
	if payload == sseDoneMarker {
		b.done = true
		return b.flush()
	}
	b.lines = append(b.lines, payload)
	return "", false
}

// flush 产出未决缓冲并清空；没有内容时 ok 为 false。
func (b *sseBuffer) flush() (string, bool) {
	if len(b.lines) == 0 {
		return "", false
	}
	payload := strings.TrimRightFunc(strings.Join(b.lines, "\n"), unicode.IsSpace)
	b.lines = b.lines[:0]
	return payload, true
}
