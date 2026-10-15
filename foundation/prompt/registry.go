package prompt

import (
	"fmt"
	"sort"
	"sync"

	"github.com/Fi2zz/conatus_go/core"
)

// Registry 是 prompt 段与上下文的注册表，可被多 goroutine 并发使用。
type Registry struct {
	mu       sync.Mutex
	sections []*sectionSlot
	contexts []*contextSlot
	addSeq   int
}

type sectionSlot struct{ section Section }

type contextSlot struct{ context Context }

// NewRegistry 创建空注册表。
func NewRegistry() *Registry { return &Registry{} }

// Sections 返回已注册的 prompt 段（注册顺序）。
func (r *Registry) Sections() []Section {
	r.mu.Lock()
	defer r.mu.Unlock()
	sections := make([]Section, 0, len(r.sections))
	for _, slot := range r.sections {
		sections = append(sections, slot.section)
	}
	return sections
}

// Contexts 返回已注册的动态上下文（注册顺序）。
func (r *Registry) Contexts() []Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	contexts := make([]Context, 0, len(r.contexts))
	for _, slot := range r.contexts {
		contexts = append(contexts, slot.context)
	}
	return contexts
}

// Section 注册一段 prompt；同名重复注册返回错误。返回的 Disposer 幂等。
//
// 撤销按**槽位**定位：即便同名段被撤销后重新注册，迟到的旧 Disposer 也不会误删新段。
func (r *Registry) Section(section Section) (core.Disposer, error) {
	if section.Name == "" {
		return nil, fmt.Errorf("prompt: section 缺少名字")
	}
	slot := &sectionSlot{section: section}

	r.mu.Lock()
	for _, existing := range r.sections {
		if existing.section.Name == section.Name {
			r.mu.Unlock()
			return nil, fmt.Errorf("prompt 段 %q 已注册", section.Name)
		}
	}
	r.sections = append(r.sections, slot)
	r.mu.Unlock()

	var once sync.Once
	return func() error {
		once.Do(func() { r.removeSectionSlot(slot) })
		return nil
	}, nil
}

// Add 用一段纯文本创建并注册 prompt 段；名字缺省时自动生成（`add-N`，撤销后不复用）。
// 返回段名，供 [Registry.Remove] 使用。
func (r *Registry) Add(text string, opts ...AddOption) (string, error) {
	settings := addSettings{}
	for _, opt := range opts {
		opt(&settings)
	}
	name := settings.name
	if name == "" {
		name = r.nextAddName()
	}
	if _, err := r.Section(Section{Name: name, Order: settings.order, Text: func() string { return text }}); err != nil {
		return "", err
	}
	return name, nil
}

// AddOption 定制 [Registry.Add]。
type AddOption func(*addSettings)

type addSettings struct {
	name  string
	order int
}

// WithSectionName 指定段名（缺省自动生成）。
func WithSectionName(name string) AddOption {
	return func(s *addSettings) { s.name = name }
}

// WithSectionOrder 指定排序权重（缺省 0）。
func WithSectionOrder(order int) AddOption {
	return func(s *addSettings) { s.order = order }
}

// Context 注册一份动态上下文；同名重复注册返回错误。返回的 Disposer 幂等。
func (r *Registry) Context(context Context) (core.Disposer, error) {
	if context.Name == "" {
		return nil, fmt.Errorf("prompt: context 缺少名字")
	}
	slot := &contextSlot{context: context}

	r.mu.Lock()
	for _, existing := range r.contexts {
		if existing.context.Name == context.Name {
			r.mu.Unlock()
			return nil, fmt.Errorf("prompt 上下文 %q 已注册", context.Name)
		}
	}
	r.contexts = append(r.contexts, slot)
	r.mu.Unlock()

	var once sync.Once
	return func() error {
		once.Do(func() { r.removeContextSlot(slot) })
		return nil
	}, nil
}

// Remove 按段名移除一段 prompt 段；未注册返回 false（幂等）。
func (r *Registry) Remove(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index, slot := range r.sections {
		if slot.section.Name != name {
			continue
		}
		r.sections = append(r.sections[:index], r.sections[index+1:]...)
		return true
	}
	return false
}

// Assemble 装配：按 (Order, Name) 升序取段与上下文，并在本次装配时求值 provider。
//
// 每次装配都重新求值，因此跨天、跨状态的动态内容自动更新（缓存由调用方决定）。
func (r *Registry) Assemble(variables map[string]string) Assembly {
	r.mu.Lock()
	sections := append([]*sectionSlot(nil), r.sections...)
	contexts := append([]*contextSlot(nil), r.contexts...)
	r.mu.Unlock()

	sort.Slice(sections, func(i, j int) bool {
		return precedes(sections[i].section.Order, sections[i].section.Name,
			sections[j].section.Order, sections[j].section.Name)
	})
	sort.Slice(contexts, func(i, j int) bool {
		return precedes(contexts[i].context.Order, contexts[i].context.Name,
			contexts[j].context.Order, contexts[j].context.Name)
	})

	assembly := Assembly{Variables: variables}
	for _, slot := range sections {
		assembly.Sections = append(assembly.Sections, AssembledSection{
			Name: slot.section.Name, Text: slot.section.Text(),
		})
	}
	for _, slot := range contexts {
		assembly.Contexts = append(assembly.Contexts, AssembledContext{
			Name: slot.context.Name, Text: slot.context.Text(),
		})
	}
	return assembly
}

func (r *Registry) removeSectionSlot(target *sectionSlot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index, slot := range r.sections {
		if slot != target {
			continue
		}
		r.sections = append(r.sections[:index], r.sections[index+1:]...)
		return
	}
}

func (r *Registry) removeContextSlot(target *contextSlot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index, slot := range r.contexts {
		if slot != target {
			continue
		}
		r.contexts = append(r.contexts[:index], r.contexts[index+1:]...)
		return
	}
}

// nextAddName 生成未占用的 `add-N`：单调递增、撤销后不复用。
func (r *Registry) nextAddName() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for {
		candidate := fmt.Sprintf("add-%d", r.addSeq)
		r.addSeq++
		if !r.hasSectionName(candidate) {
			return candidate
		}
	}
}

// hasSectionName 报告段名是否已被占用（调用方须持有锁）。
func (r *Registry) hasSectionName(name string) bool {
	for _, slot := range r.sections {
		if slot.section.Name == name {
			return true
		}
	}
	return false
}

func precedes(orderA int, nameA string, orderB int, nameB string) bool {
	if orderA != orderB {
		return orderA < orderB
	}
	return nameA < nameB
}
