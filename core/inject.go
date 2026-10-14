package core

import (
	"strings"
	"sync"
)

// Inject 声明式依赖注入。
//
// 行为：
//
//   - 当 deps 全部可见时，在**一个全新的子上下文**中执行 callback；
//   - 任一依赖消失时，该子上下文连同其所有效应被撤销；
//   - 依赖重新齐全时，callback 会在另一个全新子上下文中再次执行。
//
// 返回的 [Disposer] 用于手动取消注入（幂等）；该取消动作本身也会被登记到当前
// 上下文，随宿主释放而自动执行。
//
// 与 Dart 版的差别仅在错误通道：Dart 用 `void Function(Context)` + try/catch，
// Go 的 callback 返回 error，出错时回滚子上下文并把错误交给错误接收器
// （对应 Dart 交给 `Zone.current.handleUncaughtError`——`evaluate` 在广播期间被
// 调用，调用栈已断开，错误无法同步冒泡给 Inject 的调用者）。
func (c *Context) Inject(deps []string, callback func(*Context) error) Disposer {
	injection := newInjection(c, deps, callback)
	injection.handle = c.reactor.Add(injection.evaluate)
	injection.evaluate()
	c.Track(injection.cancel)
	return injection.cancel
}

// injection 是一次 inject 声明的运行时状态。
type injection struct {
	context  *Context
	keys     []string
	callback func(*Context) error
	handle   *ListenerHandle

	mu        sync.Mutex
	active    *Context
	cancelled bool
}

func newInjection(c *Context, deps []string, callback func(*Context) error) *injection {
	return &injection{context: c, keys: dedupeKeys(deps), callback: callback}
}

// evaluate 是注册到反应器上的监听器：依赖齐全就激活，否则撤销激活。
func (i *injection) evaluate() {
	if !i.dependenciesReady() {
		i.deactivate()
		return
	}
	i.activateIfIdle()
}

// cancel 取消注入（幂等）：移除监听器并撤销当前激活的子上下文。
func (i *injection) cancel() error {
	if i.markCancelled() {
		return nil
	}
	i.handle.Remove()
	i.deactivate()
	return nil
}

func (i *injection) activateIfIdle() {
	child, ok := i.beginActivation()
	if !ok {
		return
	}
	if err := i.callback(child); err != nil {
		child.disposeInternal()
		i.deactivate()
		i.context.errors.report(err)
	}
}

func (i *injection) deactivate() {
	child, ok := i.detachActive()
	if ok {
		child.disposeInternal()
	}
}

func (i *injection) dependenciesReady() bool {
	for _, key := range i.keys {
		if !i.context.Has(key) {
			return false
		}
	}
	return true
}

func (i *injection) childName() string {
	return i.context.name + "<" + strings.Join(i.keys, "+") + ">"
}

func (i *injection) beginActivation() (*Context, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.activationBlocked() {
		return nil, false
	}
	child := i.context.newChild(i.childName())
	i.active = child
	return child, true
}

func (i *injection) detachActive() (*Context, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	child := i.active
	i.active = nil
	return child, child != nil
}

func (i *injection) markCancelled() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	alreadyCancelled := i.cancelled
	i.cancelled = true
	return alreadyCancelled
}

// activationBlocked 判断此刻不该新建激活子上下文：已取消、宿主已释放，或已经
// 处于激活态。调用方须持有 i.mu。
func (i *injection) activationBlocked() bool {
	if i.cancelled || i.context.Disposed() {
		return true
	}
	return i.active != nil
}

// dedupeKeys 去重并冻结依赖列表，避免调用方后续修改影响语义；保留首次出现次序。
func dedupeKeys(deps []string) []string {
	seen := make(map[string]struct{}, len(deps))
	keys := make([]string, 0, len(deps))
	for _, key := range deps {
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}
