package tools

import "sync"

// slotList 是「登记 → 可移除」的通用列表。
//
// 撤销按登记时拿到的句柄定位，等价于 Dart 的 `List.remove(x)`（按元素同一性移除，
// 移除一个实例）。Go 的 slice + 指针句柄比 `reflect.Value.Pointer()` 可靠：同一函数
// 字面量生成的不同闭包会共享码指针，按码指针移除会误删别人。
type slotList[T any] struct {
	mu    sync.Mutex
	slots []*slot[T]
}

type slot[T any] struct{ value T }

func (l *slotList[T]) add(value T) *slot[T] {
	l.mu.Lock()
	defer l.mu.Unlock()
	handle := &slot[T]{value: value}
	l.slots = append(l.slots, handle)
	return handle
}

func (l *slotList[T]) remove(handle *slot[T]) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for index, existing := range l.slots {
		if existing != handle {
			continue
		}
		l.slots = append(l.slots[:index], l.slots[index+1:]...)
		return
	}
}

// snapshot 返回值快照：调用方在锁外遍历，回调里重入登记/撤销都不会死锁。
func (l *slotList[T]) snapshot() []T {
	l.mu.Lock()
	defer l.mu.Unlock()
	values := make([]T, 0, len(l.slots))
	for _, handle := range l.slots {
		values = append(values, handle.value)
	}
	return values
}

func (l *slotList[T]) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.slots)
}
