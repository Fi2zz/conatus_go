package core

import (
	"fmt"
	"sort"
	"sync"
)

// serviceSlot 是一次「提供服务」登记的份额。
//
// 单独包一层是为了让撤销函数按指针比较实现 Dart 的 `identical`——
// `any` 直接比较会在 slice / map / func 值上 panic。
type serviceSlot struct {
	value any
}

// serviceRegistration 是一次服务登记的句柄，其 remove 即 [Context.Provide]
// 返回的 [Disposer]。
type serviceRegistration struct {
	context *Context
	key     string
	slot    *serviceSlot

	mu      sync.Mutex
	removed bool
}

// remove 撤销本次登记（幂等）。若服务已被覆盖（先撤销旧登记、又提供了新值），
// 不会误删新值。
func (r *serviceRegistration) remove() error {
	if r.markRemoved() {
		return nil
	}
	if !r.context.detachService(r.key, r.slot) {
		return nil
	}
	return r.context.notify()
}

func (r *serviceRegistration) markRemoved() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	alreadyRemoved := r.removed
	r.removed = true
	return alreadyRemoved
}

// Provide 在当前上下文提供服务。
//
// 服务随本上下文释放而自动移除；返回的 [Disposer] 亦可提前手动撤销（幂等）。
// 撤销会触发一次广播，因此依赖该服务的 inject 会被级联停用。
//
// 三种失败：同一上下文中重复提供同名服务、上下文已释放、value 为 nil。
// 后一条是 Dart 版的空洞——那边允许 `provide(key, null)`，但 `get<T extends Object>`
// 永远取不到它，于是「服务存在」与「服务可读」不一致；Go 侧收紧为显式错误。
func (c *Context) Provide(key string, value any) (Disposer, error) {
	registration, err := c.registerService(key, value)
	if err != nil {
		return nil, err
	}
	disposer := registration.remove
	c.Track(disposer)
	if err := c.notify(); err != nil {
		return disposer, err
	}
	return disposer, nil
}

// Service 沿父链查找服务原始值；找不到返回 false。
//
// 需要类型化结果时用 [Get] / [Require]。
func (c *Context) Service(key string) (any, bool) {
	c.mu.Lock()
	slot, found := c.services[key]
	c.mu.Unlock()
	if found {
		return slot.value, true
	}
	if c.parent == nil {
		return nil, false
	}
	return c.parent.Service(key)
}

// Has 返回服务是否可见（含继承）。
func (c *Context) Has(key string) bool {
	_, found := c.Service(key)
	return found
}

// LocalServiceKeys 返回本上下文**直接**提供的服务名（不含继承部分），按字典序排序。
//
// 排序是对 Dart 版插入序的替代：Go 的 map 无序，而 Dart 侧唯一的消费点本来就
// 会 sort()，行为无差异。
func (c *Context) LocalServiceKeys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]string, 0, len(c.services))
	for key := range c.services {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Get 沿父链查找服务并断言类型。
//
// 服务不存在或类型不符都返回零值与 false——Go 侧无法像 Dart 的
// `get<T extends Object>` 那样用泛型约束在编译期排除空值，只能在运行期区分，
// 需要区分「不存在」与「类型不符」时用 [Require]。
func Get[T any](c *Context, key string) (T, bool) {
	value, found := c.Service(key)
	if !found {
		var zero T
		return zero, false
	}
	typed, ok := value.(T)
	return typed, ok
}

// Require 与 [Get] 相同，但失败时返回错误（服务不可用 / 类型不符）。
func Require[T any](c *Context, key string) (T, error) {
	var zero T
	value, found := c.Service(key)
	if !found {
		return zero, fmt.Errorf("服务 %q 在上下文 %q 中不可用", key, c.Name())
	}
	typed, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("服务 %q 在上下文 %q 中类型不符：期望 %T，实际 %T",
			key, c.Name(), zero, value)
	}
	return typed, nil
}

func (c *Context) registerService(key string, value any) (*serviceRegistration, error) {
	if value == nil {
		return nil, fmt.Errorf("服务 %q 的值不能为 nil", key)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Disposed() {
		return nil, fmt.Errorf("上下文 %q 已释放，无法再提供服务 %q", c.name, key)
	}
	if _, exists := c.services[key]; exists {
		return nil, fmt.Errorf("服务 %q 已在上下文 %q 中提供", key, c.name)
	}
	slot := &serviceSlot{value: value}
	c.services[key] = slot
	return &serviceRegistration{context: c, key: key, slot: slot}, nil
}

func (c *Context) detachService(key string, slot *serviceSlot) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.services[key] != slot {
		return false
	}
	delete(c.services, key)
	return true
}

// dropRemainingServices 是释放路径上的兜底：正常情况下服务已被各自的撤销函数
// 移除，若某个撤销函数意外没做（或重新提供了服务），这里一并清掉。
func (c *Context) dropRemainingServices() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.services) == 0 {
		return false
	}
	c.services = map[string]*serviceSlot{}
	return true
}
