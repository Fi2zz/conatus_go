package core_test

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Fi2zz/conatus_go/core"
)

// effectWorld 是验证用的「世界」：真实外部状态 + 独立于被测记账的可观测投影。
//
// 投影的每一项都必须独立于被测插件的记账，否则「忘了记账」会把泄漏一起掩盖掉：
//
//   - 定时器与订阅读世界自己代理的存活位，另加真实 tick 计数与 goroutine 退出
//     作为硬证据；
//   - 资源句柄读自己的 open 标志；
//   - 服务读 core.Context.LocalServiceKeys。
//
// 世界只**单调**记录「创建过哪些资源」，从不做删除，因此漏释放只会让投影变化。
// 唯一由被测代码写入的是资源自身的状态位，而它正是逆必须作用的对象。
type effectWorld struct {
	shared   map[string]int
	registry map[string]struct{}

	tickers []*timerHandle
	streams []*streamHandle
	handles []*resourceHandle

	seq int
}

func newEffectWorld() *effectWorld {
	return &effectWorld{shared: map[string]int{}, registry: map[string]struct{}{}}
}

func (w *effectWorld) nextName(prefix string) string {
	name := fmt.Sprintf("%s-%d", prefix, w.seq)
	w.seq++
	return name
}

func (w *effectWorld) acquire() *resourceHandle {
	handle := &resourceHandle{id: w.nextName("h"), open: true}
	w.handles = append(w.handles, handle)
	return handle
}

func (w *effectWorld) startTimer() *timerHandle {
	handle := newTimerHandle()
	w.tickers = append(w.tickers, handle)
	return handle
}

func (w *effectWorld) openStream() *streamHandle {
	handle := newStreamHandle()
	w.streams = append(w.streams, handle)
	return handle
}

// project 返回独立可观测的投影；行序稳定，便于失败时逐行 diff（与 Dart 版同形）。
func (w *effectWorld) project(contexts []*core.Context) []string {
	lines := []string{
		"共享值: " + w.sharedPairs(),
		"注册表: " + w.registryNames(),
		"句柄: " + w.openHandleIDs(),
		fmt.Sprintf("定时器存活: %d", w.activeTimers()),
		fmt.Sprintf("订阅存活: %d", w.activeStreams()),
	}
	for _, ctx := range contexts {
		if ctx.Disposed() {
			continue
		}
		lines = append(lines, "上下文 "+ctx.Name()+": "+strings.Join(ctx.LocalServiceKeys(), "|"))
	}
	return lines
}

// allReleased 报告定时器与订阅是否都已释放。
func (w *effectWorld) allReleased() bool {
	for _, handle := range w.tickers {
		if handle.Active() {
			return false
		}
	}
	for _, handle := range w.streams {
		if handle.Active() {
			return false
		}
	}
	return true
}

// totalTicks 返回所有定时器真实触发的周期回调次数：卸载后它必须停止增长。
func (w *effectWorld) totalTicks() int64 {
	var total int64
	for _, handle := range w.tickers {
		total += handle.Ticks()
	}
	return total
}

// teardown 取消全部定时器、关闭全部订阅（测试收尾用，与插件行为无关）。
func (w *effectWorld) teardown() {
	for _, handle := range w.tickers {
		handle.Cancel()
	}
	for _, handle := range w.streams {
		handle.Cancel()
	}
}

func (w *effectWorld) activeTimers() int {
	count := 0
	for _, handle := range w.tickers {
		if handle.Active() {
			count++
		}
	}
	return count
}

func (w *effectWorld) activeStreams() int {
	count := 0
	for _, handle := range w.streams {
		if handle.Active() {
			count++
		}
	}
	return count
}

func (w *effectWorld) sharedPairs() string {
	keys := make([]string, 0, len(w.shared))
	for key := range w.shared {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%d", key, w.shared[key]))
	}
	return strings.Join(pairs, "|")
}

func (w *effectWorld) registryNames() string {
	names := make([]string, 0, len(w.registry))
	for name := range w.registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, "|")
}

func (w *effectWorld) openHandleIDs() string {
	ids := make([]string, 0, len(w.handles))
	for _, handle := range w.handles {
		if handle.open {
			ids = append(ids, handle.id)
		}
	}
	sort.Strings(ids)
	return strings.Join(ids, "|")
}
