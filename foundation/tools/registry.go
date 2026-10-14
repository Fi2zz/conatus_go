package tools

import (
	"sync"
	"time"
)

// ServiceKey 是工具注册表在上下文中的服务键。
const ServiceKey = "tools"

type toolSlot struct{ tool Tool }

type middlewareEntry struct {
	fn  Middleware
	tag string
}

// RegistryOption 定制 [Registry]。
type RegistryOption func(*Registry)

// WithDefaultTimeout 设定缺省超时：调用参数未给、工具自身也未声明时生效
// （0 表示不限时）。
func WithDefaultTimeout(timeout time.Duration) RegistryOption {
	return func(r *Registry) { r.defaultTimeout = timeout }
}

// Registry 是工具注册表与执行管线，可被多 goroutine 并发使用。
//
// 锁只在读写内部状态时持有：守卫、中间件、监听器与工具执行体一律在锁外调用，
// 因此回调里重入登记/撤销/调用都不会死锁。
type Registry struct {
	mu             sync.Mutex
	defaultTimeout time.Duration
	slots          map[string]*toolSlot
	order          []string

	guards      slotList[Guard]
	middlewares slotList[middlewareEntry]
	changes     slotList[func()]
	results     slotList[ResultListener]
}

// NewRegistry 创建一个空注册表。
func NewRegistry(opts ...RegistryOption) *Registry {
	registry := &Registry{slots: map[string]*toolSlot{}}
	for _, opt := range opts {
		opt(registry)
	}
	return registry
}

// DefaultTimeout 返回缺省超时（0 表示不限时）。
func (r *Registry) DefaultTimeout() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.defaultTimeout
}

// Len 返回已注册的工具数。
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.slots)
}

// Names 返回已注册的工具名（按注册顺序）。
func (r *Registry) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.order...)
}

// Get 查找工具；未注册返回 false。
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	slot, found := r.slots[name]
	if !found {
		return nil, false
	}
	return slot.tool, true
}

// Describe 返回当前可见工具的模型 schema 列表（白名单投影）。
func (r *Registry) Describe() []map[string]any {
	schemas := []map[string]any{}
	for _, name := range r.Names() {
		if schema, found := r.DescribeOne(name); found {
			schemas = append(schemas, schema)
		}
	}
	return schemas
}

// DescribeOne 按名描述单个工具；未注册返回 false。
func (r *Registry) DescribeOne(name string) (map[string]any, bool) {
	tool, found := r.Get(name)
	if !found {
		return nil, false
	}
	return SchemaOf(tool), true
}
