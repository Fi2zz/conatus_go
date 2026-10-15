// Package loader 按名注册插件工厂，并用声明式配置树装载它们（对应 Dart 的 loader 插件）。
//
// Dart 没有动态 `import()`，loader 因此用注册表代替模块解析：宿主先 [Loader.Register]
// 插件工厂，再用配置树声明要加载哪些 entry。已加载的 entry 都是 loader 所在上下文的
// 子上下文，随宿主上下文释放自动卸载。
//
//	instance, err := loader.Provide(ctx, loader.WithPlugins(map[string]loader.PluginFactory{
//		"logger": func(child *core.Context, config any) error { return provideLogger(child) },
//	}), loader.WithConfig([]loader.LoaderEntry{{ID: "log", Name: "logger"}}))
//
// # 与 Dart 版的形态差异
//
//   - 可预期失败返回 error（Dart 抛 LoaderException）：分类用 [errors.Is] 配合
//     [ErrDuplicateEntry] / [ErrUnknownPlugin] / [ErrUnknownEntry]；
//   - 插件工厂返回 error（Dart 的工厂无返回值、只能抛异常）：返回错误时子上下文被
//     回滚，entry 不保留；
//   - 分组节点是显式字段 [LoaderEntry.Group]（Dart 用 `name == null`）；
//   - [Loader.Names] 返回字典序（Dart 是插入序）：Go 的 map 无序，与
//     [core.Context.LocalServiceKeys] 是同一条取舍；[Loader.IDs] 仍按加载顺序。
//
// # 并发契约
//
// 所有方法可被多 goroutine 并发使用。锁只在读写内部状态时持有，插件工厂与撤销函数
// 一律在锁外调用，因此工厂里重入 loader 的方法不会死锁。并发的 Load / Remove 交错
// 是「最后登记者胜」：被并发摘除的 entry 的子上下文仍登记在 loader 所在上下文上，
// 最终由宿主上下文释放时兜底撤销。
package loader

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/Fi2zz/conatus_go/core"
)

// ServiceKey 是装载器在上下文中的服务键。
const ServiceKey = "loader"

// 失败分类：Dart 侧只有一种 LoaderException，靠消息文本区分；Go 侧收紧为可判定的哨兵。
var (
	// ErrDuplicateEntry 表示同一 Loader 内 entry id 重复。
	ErrDuplicateEntry = errors.New("loader: entry id 重复")
	// ErrUnknownPlugin 表示 entry 指向的插件名没有注册。
	ErrUnknownPlugin = errors.New("loader: 插件未注册")
	// ErrUnknownEntry 表示 Remove / Reload 的 id 不存在。
	ErrUnknownEntry = errors.New("loader: 未知 entry")
)

// PluginFactory 在子上下文上施加插件副作用。
//
// 与 Dart 的差别：这里的 error 是返回值而不是异常，因此「安装到一半失败」能被
// [Loader.Load] 察觉并回滚子上下文。
type PluginFactory func(ctx *core.Context, config any) error

// Loader 持有插件注册表与已加载的 entry 树。零值不可用，用 [NewLoader] 或 [Provide]。
type Loader struct {
	ctx *core.Context

	mu        sync.Mutex
	factories map[string]PluginFactory
	entries   map[string]LoaderEntry
	order     []string
	running   map[string]*core.Context
	seq       int
}

// NewLoader 创建装载器，可带一张预注册表（nil 表示空表）。
func NewLoader(ctx *core.Context, plugins map[string]PluginFactory) *Loader {
	if ctx == nil {
		panic("loader: NewLoader 收到 nil 上下文")
	}
	instance := &Loader{
		ctx:       ctx,
		factories: map[string]PluginFactory{},
		entries:   map[string]LoaderEntry{},
		running:   map[string]*core.Context{},
	}
	for name, factory := range plugins {
		if factory == nil {
			// 与 [Loader.Register] 不同：构造期没有返回 error 的位置，nil 工厂是
			// 编程错误（Dart 的类型系统让它不可表达），就地 panic 好过加载时才炸。
			panic(fmt.Sprintf("loader: 插件 %q 的工厂不能为 nil", name))
		}
		instance.factories[name] = factory
	}
	return instance
}

// Register 注册插件工厂；同名覆盖。
func (l *Loader) Register(name string, factory PluginFactory) error {
	if name == "" {
		return errors.New("loader: 插件名不能为空")
	}
	if factory == nil {
		return fmt.Errorf("loader: 插件 %q 的工厂不能为 nil", name)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.factories[name] = factory
	return nil
}

// Unregister 注销插件工厂，返回是否确实移除了一个。
func (l *Loader) Unregister(name string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, found := l.factories[name]
	delete(l.factories, name)
	return found
}

// Has 返回是否已注册某插件。
func (l *Loader) Has(name string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, found := l.factories[name]
	return found
}

// Names 返回已注册的插件名，按字典序。
func (l *Loader) Names() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	names := make([]string, 0, len(l.factories))
	for name := range l.factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
