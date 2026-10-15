package loader

// Apply 全量替换配置树：先逆序卸载现有 entry，再按 entries 顺序加载。
//
// 中途加载失败时，此前已加载的 entry 保持加载状态、失败的 entry 不保留——与 Dart
// 一致（那边也是先清空再逐个 load，异常从中间冒出去）。
func (l *Loader) Apply(entries []LoaderEntry) error {
	disposeAll(l.detachAll())
	for _, entry := range entries {
		if _, err := l.Load(entry); err != nil {
			return err
		}
	}
	return nil
}

// ApplyJSON 从 JSON 配置全量替换配置树。
//
// 先解析全部节点再动现有树：解析失败不会破坏当前配置。
func (l *Loader) ApplyJSON(entries []map[string]any) error {
	parsed := make([]LoaderEntry, 0, len(entries))
	for _, raw := range entries {
		entry, err := EntryFromJSON(raw)
		if err != nil {
			return err
		}
		parsed = append(parsed, entry)
	}
	return l.Apply(parsed)
}
