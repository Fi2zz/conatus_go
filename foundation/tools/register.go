package tools

import (
	"errors"
	"fmt"
	"sync"

	"github.com/Fi2zz/conatus_go/core"
)

// Register 注册一个工具，返回撤销函数（幂等）。
//
// 同名重复注册返回错误（Dart 版抛 StateError）。撤销按**槽位指针**比对：只有在
// 该名字仍指向本次登记的槽位时才移除，避免误删同名覆盖后的新工具。
func (r *Registry) Register(tool Tool) (core.Disposer, error) {
	if tool == nil {
		return nil, errors.New("tools: Register 收到 nil 工具")
	}
	name := tool.Name()
	if name == "" {
		return nil, errors.New("tools: 工具名不能为空")
	}
	handle := &toolSlot{tool: tool}

	r.mu.Lock()
	if _, exists := r.slots[name]; exists {
		r.mu.Unlock()
		return nil, fmt.Errorf("工具 %q 已注册", name)
	}
	r.slots[name] = handle
	r.order = append(r.order, name)
	r.mu.Unlock()
	r.notifyChanges()

	var once sync.Once
	return func() error {
		once.Do(func() { r.revokeSlot(name, handle) })
		return nil
	}, nil
}

// Guard 登记一个单调守卫，返回撤销函数（幂等）。
func (r *Registry) Guard(guard Guard) core.Disposer {
	handle := r.guards.add(guard)
	var once sync.Once
	return func() error {
		once.Do(func() { r.guards.remove(handle) })
		return nil
	}
}

// Use 登记一个环绕中间件，返回撤销函数（幂等）。
//
// **先登记的中间件在最外层**（与 Dart 版一致）；tag 用于 [Registry.CopyPipelineTo]
// 按类排除（如子注册表要换掉审批那一层，同时保留 hooks / 驱逐 / lint）。
func (r *Registry) Use(middleware Middleware, tag string) core.Disposer {
	handle := r.middlewares.add(middlewareEntry{fn: middleware, tag: tag})
	var once sync.Once
	return func() error {
		once.Do(func() { r.middlewares.remove(handle) })
		return nil
	}
}

// CopyPipelineTo 把本注册表的守卫与中间件复制到 target（不复制工具与结果监听器）。
//
// 供「受限子注册表」复用宿主同一条执行管线：子表只放白名单内的工具，但审批、
// 结果驱逐、hooks、lint 等对子调用同样生效。复制的是中间件闭包本身；闭包内对
// 注册表的引用仍指向**源**表（如审批会用源表查工具元信息），与直接调用源表一致。
//
// excludeTags 里的中间件跳过不复制。
func (r *Registry) CopyPipelineTo(target *Registry, excludeTags ...string) {
	for _, guard := range r.guards.snapshot() {
		target.Guard(guard)
	}
	for _, entry := range r.middlewares.snapshot() {
		if entry.tag != "" && containsTag(excludeTags, entry.tag) {
			continue
		}
		target.Use(entry.fn, entry.tag)
	}
}

// OnChange 监听工具表变更（注册与撤销各触发一次），返回撤销函数（幂等）。
func (r *Registry) OnChange(listener func()) core.Disposer {
	handle := r.changes.add(listener)
	var once sync.Once
	return func() error {
		once.Do(func() { r.changes.remove(handle) })
		return nil
	}
}

// OnResult 监听每次调用结局，返回撤销函数（幂等）。
func (r *Registry) OnResult(listener ResultListener) core.Disposer {
	handle := r.results.add(listener)
	var once sync.Once
	return func() error {
		once.Do(func() { r.results.remove(handle) })
		return nil
	}
}

func (r *Registry) notifyChanges() {
	for _, listener := range r.changes.snapshot() {
		listener()
	}
}

func (r *Registry) revokeSlot(name string, handle *toolSlot) {
	r.mu.Lock()
	if r.slots[name] != handle {
		r.mu.Unlock()
		return
	}
	delete(r.slots, name)
	r.order = removeName(r.order, name)
	r.mu.Unlock()
	r.notifyChanges()
}

func removeName(names []string, target string) []string {
	for index, name := range names {
		if name != target {
			continue
		}
		return append(names[:index], names[index+1:]...)
	}
	return names
}

func containsTag(tags []string, target string) bool {
	for _, tag := range tags {
		if tag == target {
			return true
		}
	}
	return false
}
