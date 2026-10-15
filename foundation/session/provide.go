package session

import (
	"github.com/Fi2zz/conatus_go/core"
)

// 服务键。
const (
	// StoreKey 是会话仓库在上下文中的服务键。
	StoreKey = "sessions"
	// PersistenceKey 是持久化端口在上下文中的服务键。
	PersistenceKey = "sessionPersistence"
)

// ProvidePersistence 把持久化端口提供到上下文；persistence 为 nil 时用 JSONL 实现
// （目录缺省 `<conatus 用户目录>/sessions`）。
func ProvidePersistence(ctx *core.Context, persistence Persistence) (Persistence, error) {
	if persistence == nil {
		persistence = NewJSONLPersistence("")
	}
	if _, err := ctx.Provide(PersistenceKey, persistence); err != nil {
		return nil, err
	}
	return persistence, nil
}

// ProvideOption 定制 [ProvideSessions]。
type ProvideOption func(*provideSettings)

type provideSettings struct {
	store       *Store
	persistence Persistence
	hasStore    bool
}

// WithStore 提供现成仓库（缺省新建）。
func WithStore(store *Store) ProvideOption {
	return func(s *provideSettings) {
		s.store = store
		s.hasStore = true
	}
}

// WithSessionPersistence 指定持久化端口（缺省复用上下文里已提供的那个）。
func WithSessionPersistence(persistence Persistence) ProvideOption {
	return func(s *provideSettings) { s.persistence = persistence }
}

// ProvideSessions 把会话仓库提供到上下文（服务键 [StoreKey]）。
//
// 未显式传入持久化时，复用上下文里已提供的 [PersistenceKey]。
func ProvideSessions(ctx *core.Context, opts ...ProvideOption) (*Store, error) {
	settings := provideSettings{}
	for _, opt := range opts {
		opt(&settings)
	}
	store := settings.store
	if !settings.hasStore {
		store = NewStore(WithPersistence(resolvePersistence(ctx, settings.persistence)))
	}
	if _, err := ctx.Provide(StoreKey, store); err != nil {
		return nil, err
	}
	return store, nil
}

func resolvePersistence(ctx *core.Context, given Persistence) Persistence {
	if given != nil {
		return given
	}
	existing, found := core.Get[Persistence](ctx, PersistenceKey)
	if !found {
		return nil
	}
	return existing
}
