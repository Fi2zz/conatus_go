package core

import "sync"

// Context 统一上下文：同时承载**效应**（可逆副作用）与**共效应**（声明式依赖）。
//
// 这是「上下文范式」的落地：
//
//   - 效应通过 [Context.Track] / [Context.Effect] / [Context.OnDispose] 登记，
//     随上下文释放而撤销；
//   - 共效应通过 [Context.Provide]（提供服务）与 [Context.Inject]（声明依赖）表达，
//     服务可用性变化时会自动激活 / 停用组件。
//
// 上下文组成一棵树，服务查找沿父链向上：
//
//	root ──┬── pluginA ──┬── sub1
//	       └── pluginB   └── sub2
//
// 子上下文可见父上下文提供的服务；反之不成立。父上下文释放会级联释放子树
// （子上下文的释放动作本身被登记为父上下文的一个撤销函数）。
type Context struct {
	parent  *Context
	reactor *Reactor
	scope   *EffectScope
	errors  *errorSink
	name    string

	mu       sync.Mutex
	services map[string]*serviceSlot
}

// NewRoot 创建一个根上下文。
func NewRoot(name string) *Context {
	return &Context{
		reactor:  NewReactor(DefaultMaxRounds),
		scope:    NewEffectScope(),
		errors:   newErrorSink(),
		name:     name,
		services: map[string]*serviceSlot{},
	}
}

// Name 返回上下文名称，用于调试与日志。
func (c *Context) Name() string { return c.name }

// Parent 返回父上下文；根上下文返回 nil。
func (c *Context) Parent() *Context { return c.parent }

// Disposed 返回上下文是否已释放。
func (c *Context) Disposed() bool { return c.scope.Disposed() }

// String 返回 `Context(名称)`——子上下文名用它拼前缀，投影与日志逐字节对齐 Dart 版。
func (c *Context) String() string { return "Context(" + c.name + ")" }

// SetErrorHandler 替换错误接收器（nil 表示恢复默认：写 stderr）。
//
// 接收器由根上下文持有、整棵树共享，因此设置一次即对全树生效。
func (c *Context) SetErrorHandler(handler func(error)) { c.errors.set(handler) }

// Track 登记一个撤销函数。上下文释放时按 LIFO 顺序执行。
func (c *Context) Track(d Disposer) { c.scope.Track(d) }

// OnDispose 是 [Context.Track] 的语义化别名。
func (c *Context) OnDispose(d Disposer) { c.Track(d) }

// Effect 执行 body；若其返回值是 [Disposer]，则自动登记。
func (c *Context) Effect(body func() Disposer) Disposer { return c.scope.Capture(body) }

// Plugin 加载一个插件：在派生的子上下文中执行 install。
//
// 返回的子上下文可用于后续卸载（[Context.Dispose]）；该子上下文同时被登记到当前
// 上下文，因此父上下文释放时会一并卸载。install 返回错误时子上下文被回滚，
// 错误原样返回给调用方。
func (c *Context) Plugin(name string, install func(*Context) error) (*Context, error) {
	child := c.newChild(c.String() + "/" + name)
	c.Track(Ignore(child.disposeInternal))
	if err := install(child); err != nil {
		child.disposeInternal()
		return nil, err
	}
	return child, nil
}

// Dispose 释放上下文：撤销所有效应，移除所有提供的服务。幂等。
//
// 撤销过程中产生的错误交给错误接收器（默认写 stderr）而不是返回：卸载路径要
// 「尽可能多清理」，不应该因为某个逆出错就中断。Dart 版在这里是静默丢弃错误，
// Go 侧改为上报。
func (c *Context) Dispose() { c.disposeInternal() }

func (c *Context) newChild(name string) *Context {
	return &Context{
		parent:   c,
		reactor:  c.reactor,
		scope:    NewEffectScope(),
		errors:   c.errors,
		name:     name,
		services: map[string]*serviceSlot{},
	}
}

func (c *Context) notify() error { return c.reactor.Notify() }

func (c *Context) disposeInternal() {
	for _, err := range c.scope.Dispose() {
		c.errors.report(err)
	}
	if c.dropRemainingServices() {
		_ = c.notify()
	}
}
