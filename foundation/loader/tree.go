package loader

import (
	"fmt"

	"github.com/Fi2zz/conatus_go/core"
)

// IDs 返回已加载 entry 的 id，按加载顺序。
func (l *Loader) IDs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.order...)
}

// Empty 返回是否没有任何 entry。
func (l *Loader) Empty() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries) == 0
}

// EntryOf 返回 entry 的配置节点；未知 id 返回 false。
func (l *Loader) EntryOf(id string) (LoaderEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, found := l.entries[id]
	return entry, found
}

// ContextOf 返回 entry 的插件子上下文；未加载、分组、禁用或已卸载时返回 false。
func (l *Loader) ContextOf(id string) (*core.Context, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	handle, found := l.running[id]
	return handle, found
}

// Load 加载一个 entry 并返回其 id。分组会递归加载 [LoaderEntry.Children]。
//
// 失败时回滚该 entry 的占位：重复 id、插件未注册、工厂返回错误都不留残迹。
// Dart 版还有个可选的 parent 参数用于自动 id 加前缀，那只是递归的内部需要，
// Go 侧不对外暴露。
func (l *Loader) Load(entry LoaderEntry) (string, error) { return l.load(entry, "") }

func (l *Loader) load(entry LoaderEntry, parent string) (string, error) {
	id := l.entryID(entry.ID, parent)
	if err := l.reserve(id, entry); err != nil {
		return "", err
	}
	if err := l.activate(id, entry); err != nil {
		l.drop(id)
		return "", err
	}
	return id, l.loadChildren(id, entry.Children)
}

func (l *Loader) loadChildren(parent string, children []LoaderEntry) error {
	for _, child := range children {
		if _, err := l.load(child, parent); err != nil {
			return err
		}
	}
	return nil
}

// entryID 返回显式 id；为空时分配自动 id。
func (l *Loader) entryID(id, parent string) string {
	if id != "" {
		return id
	}
	return l.nextID(parent)
}

// Remove 卸载 entry 及其所有后代。
func (l *Loader) Remove(id string) error {
	handles, found := l.detach(id)
	if !found {
		return fmt.Errorf("%w：%q", ErrUnknownEntry, id)
	}
	disposeAll(handles)
	return nil
}

// Reload 重启单个 entry 的插件；分组与禁用项只摘除旧子上下文，不重启。
func (l *Loader) Reload(id string) error {
	entry, handle, found := l.detachOne(id)
	if !found {
		return fmt.Errorf("%w：%q", ErrUnknownEntry, id)
	}
	if handle != nil {
		handle.Dispose()
	}
	return l.activate(id, entry)
}

// reserve 登记 entry 并保留加载顺序；id 已存在时报 [ErrDuplicateEntry]。
func (l *Loader) reserve(id string, entry LoaderEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, taken := l.entries[id]; taken {
		return fmt.Errorf("%w：%q", ErrDuplicateEntry, id)
	}
	l.entries[id] = entry
	l.order = append(l.order, id)
	return nil
}

// activate 运行 entry 的插件，并把子上下文登记为 running。
//
// 分组与禁用项直接返回：它们登记在配置树里，但不加载插件。
func (l *Loader) activate(id string, entry LoaderEntry) error {
	if entry.Group || entry.Disabled {
		return nil
	}
	factory, registered := l.factoryOf(entry.Name)
	if !registered {
		return fmt.Errorf("%w：%q（entry %q）", ErrUnknownPlugin, entry.Name, id)
	}
	child, err := l.ctx.Plugin(id, func(c *core.Context) error { return factory(c, entry.Config) })
	if err != nil {
		return err
	}
	l.attach(id, child)
	return nil
}

func (l *Loader) factoryOf(name string) (PluginFactory, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	factory, found := l.factories[name]
	return factory, found
}

func (l *Loader) attach(id string, child *core.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.running[id] = child
}

// drop 摘除占位：加载失败的 entry 不该留在配置树里。
func (l *Loader) drop(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, id)
	delete(l.running, id)
	l.order = dropIDs(l.order, []string{id})
}

func (l *Loader) nextID(parent string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	id := fmt.Sprintf("entry-%d", l.seq)
	if parent == "" {
		return id
	}
	return parent + ":" + id
}
