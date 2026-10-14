package core

import (
	"errors"
	"fmt"
	"sync"
)

// DefaultMaxRounds 是单次 [Reactor.Notify] 允许的最大重跑轮数。
const DefaultMaxRounds = 100

// ErrNotConverged 表示共效应重评估在 maxRounds 轮后仍未收敛。
var ErrNotConverged = errors.New("共效应重评估未收敛")

// Listener 是反应器监听器签名。
type Listener func()

// ListenerHandle 是 [Reactor.Add] 返回的移除句柄。
//
// Dart 版用 `remove(listener)` 按函数同一性移除；Go 的函数值不可比较
// （只能与 nil 比较，`reflect.Value.Pointer()` 又不保证区分同一字面量的不同
// 闭包），因此改为句柄移除——这是本项目从 Dart 移植到 Go 必须换的 API 形态之一。
type ListenerHandle struct {
	owner *Reactor
	slot  *listenerSlot
}

// Remove 移除对应监听器，返回是否确实移除了一个实例（幂等）。
func (h *ListenerHandle) Remove() bool {
	if h == nil || h.owner == nil {
		return false
	}
	return h.owner.removeSlot(h.slot)
}

type listenerSlot struct {
	listener Listener
}

// Reactor 是服务变更的同步广播器。
//
// 采用「脏标记 + 重跑」策略处理重入：若在通知过程中（某个监听器内）又发生了
// 服务变更，则整个监听器列表会被重新评估一轮，直到系统收敛。这保证了依赖链
// A → B → C 能一次性稳定下来，无需调用方手动编排顺序。
type Reactor struct {
	maxRounds int

	mu      sync.Mutex
	slots   []*listenerSlot
	running bool
	dirty   bool
}

// NewReactor 创建反应器。
//
// maxRounds 限制单次 [Reactor.Notify] 中允许的最大重跑轮数，用于在循环依赖
// 导致无法收敛时快速失败，而不是死循环。maxRounds 非正数属编程错误，
// 与 time.NewTicker 一致用 panic 表达。
func NewReactor(maxRounds int) *Reactor {
	if maxRounds <= 0 {
		panic("core: NewReactor 的 maxRounds 必须为正数")
	}
	return &Reactor{maxRounds: maxRounds}
}

// MaxRounds 返回单次 Notify 中允许的最大重跑轮数。
func (r *Reactor) MaxRounds() int { return r.maxRounds }

// Running 返回是否正在广播中。
func (r *Reactor) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// Len 返回当前登记的监听器数量。
func (r *Reactor) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.slots)
}

// Add 添加监听器并返回移除句柄。同一函数可被重复添加，各自独立移除。
// listener 为 nil 属编程错误。
func (r *Reactor) Add(listener Listener) *ListenerHandle {
	if listener == nil {
		panic("core: Add 收到 nil Listener")
	}
	slot := &listenerSlot{listener: listener}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.slots = append(r.slots, slot)
	return &ListenerHandle{owner: r, slot: slot}
}

// Notify 触发一次广播。
//
// 若当前已有广播在进行中（重入），仅设置脏标记后立即返回，由最外层的广播循环
// 负责再跑一轮。注意：从别的 goroutine 进入的变更同样只置脏标记，不会等待正在
// 广播的那一轮收敛。
//
// 若在 maxRounds 轮内未能收敛（每轮都产生新的脏标记），返回 [ErrNotConverged]。
// 这通常意味着两个组件在循环地互相激活。返回错误后反应器仍可复用。
func (r *Reactor) Notify() error {
	if !r.enter() {
		return nil
	}
	defer r.leave()
	round := 0
	for {
		round++
		if round > r.maxRounds {
			return fmt.Errorf(
				"共效应重评估在 %d 轮后仍未收敛，可能存在循环依赖（A 的激活依赖 B，B 的激活又依赖 A）：%w",
				r.maxRounds, ErrNotConverged,
			)
		}
		r.broadcast()
		if !r.takeDirty() {
			return nil
		}
	}
}

func (r *Reactor) enter() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		r.dirty = true
		return false
	}
	r.running = true
	return true
}

func (r *Reactor) leave() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = false
}

// broadcast 清脏标记后按登记顺序调用一份监听器快照。
//
// 快照遍历避免监听器在回调中增删监听器造成并发修改；也保证不在持锁状态下调用
// 外部代码（监听器里重入 Notify / Add / Remove 都不会死锁）。
func (r *Reactor) broadcast() {
	for _, listener := range r.snapshot() {
		listener()
	}
}

func (r *Reactor) snapshot() []Listener {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dirty = false
	listeners := make([]Listener, 0, len(r.slots))
	for _, slot := range r.slots {
		listeners = append(listeners, slot.listener)
	}
	return listeners
}

func (r *Reactor) takeDirty() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dirty
}

func (r *Reactor) removeSlot(target *listenerSlot) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, slot := range r.slots {
		if slot != target {
			continue
		}
		r.slots = append(r.slots[:i], r.slots[i+1:]...)
		return true
	}
	return false
}
