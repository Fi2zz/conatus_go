package loader

import (
	"strings"

	"github.com/Fi2zz/conatus_go/core"
)

// detach 摘除 id 及其后代（`id` 与 `id:` 前缀），返回按加载顺序排列的子上下文。
//
// 空闲 entry（分组、禁用、后代全是分组）也会被摘除，因此用 found 而不是
// handles 是否为空来判断 id 存在与否。
func (l *Loader) detach(id string) ([]*core.Context, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	targets := matchingIDs(l.order, id)
	if len(targets) == 0 {
		return nil, false
	}
	handles := make([]*core.Context, 0, len(targets))
	for _, key := range targets {
		if handle, running := l.running[key]; running {
			handles = append(handles, handle)
		}
		delete(l.running, key)
		delete(l.entries, key)
	}
	l.order = dropIDs(l.order, targets)
	return handles, true
}

// detachOne 摘除单个 entry 的子上下文，保留 entry 本身（[Loader.Reload] 用）。
func (l *Loader) detachOne(id string) (LoaderEntry, *core.Context, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, found := l.entries[id]
	if !found {
		return LoaderEntry{}, nil, false
	}
	handle := l.running[id]
	delete(l.running, id)
	return entry, handle, true
}

// detachAll 摘除全部 entry，返回按加载顺序排列的子上下文。
func (l *Loader) detachAll() []*core.Context {
	l.mu.Lock()
	defer l.mu.Unlock()
	handles := make([]*core.Context, 0, len(l.running))
	for _, key := range l.order {
		if handle, running := l.running[key]; running {
			handles = append(handles, handle)
		}
	}
	l.entries = map[string]LoaderEntry{}
	l.running = map[string]*core.Context{}
	l.order = nil
	return handles
}

// disposeAll 按加载顺序的逆序释放子上下文：后代先于祖先，符合 LIFO。
func disposeAll(handles []*core.Context) {
	for index := len(handles) - 1; index >= 0; index-- {
		handles[index].Dispose()
	}
}

func matchingIDs(order []string, id string) []string {
	prefix := id + ":"
	matched := make([]string, 0, 1)
	for _, key := range order {
		if key == id || strings.HasPrefix(key, prefix) {
			matched = append(matched, key)
		}
	}
	return matched
}

func dropIDs(order, targets []string) []string {
	kept := make([]string, 0, len(order))
	for _, key := range order {
		if !containsID(targets, key) {
			kept = append(kept, key)
		}
	}
	return kept
}

func containsID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}
