package session

import (
	"fmt"
	"sort"
	"sync"

	"github.com/Fi2zz/conatus_go/foundation/home"
)

// StoreOption 定制 [Store]。
type StoreOption func(*Store)

// WithPersistence 接入持久化端口（缺省不落盘）。
func WithPersistence(persistence Persistence) StoreOption {
	return func(s *Store) { s.persistence = persistence }
}

// WithStoreErrorHandler 替换落盘错误接收器（缺省写 stderr）。
func WithStoreErrorHandler(handler func(error)) StoreOption {
	return func(s *Store) { s.errors.set(handler) }
}

// Store 是会话仓库：进程内持有活跃会话，并可选地持久化其事件。
//
// 追加即落盘（同步），因此不需要 Dart 的 Flush：Create/Adopt/Open 返回后，之后
// 每次 Append 返回时事件都已写进持久化后端。
type Store struct {
	mu          sync.Mutex
	persistence Persistence
	sessions    map[string]*Session
	errors      *errorSink
}

// NewStore 创建会话仓库。
func NewStore(opts ...StoreOption) *Store {
	store := &Store{sessions: map[string]*Session{}, errors: newErrorSink()}
	for _, opt := range opts {
		opt(store)
	}
	return store
}

// Persistence 返回接入的持久化端口（可能为 nil）。
func (s *Store) Persistence() Persistence { return s.persistence }

// Len 返回活跃会话数。
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// Get 查找活跃会话。
func (s *Store) Get(id string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, found := s.sessions[id]
	return session, found
}

// IDs 返回活跃会话 id（升序）。Dart 侧是插入序，Go 侧改用排序以保证确定性。
func (s *Store) IDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Create 创建新会话；id 为空时自动生成（`session_<uuid>`）。同 id 重复创建返回错误。
func (s *Store) Create(id string) (*Session, error) {
	resolved := id
	if resolved == "" {
		resolved = "session_" + home.NewUUIDv4()
	}
	session, err := New(resolved)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[resolved]; exists {
		return nil, fmt.Errorf("会话 %q 已存在", resolved)
	}
	s.sessions[resolved] = session
	s.attach(session)
	return session, nil
}

// Adopt 注册一个外部创建的会话（如 [Session.Fork] 的产物）。
//
// 与 [Store.Create]（空会话）不同，它会把会话的**全部**事件落盘——fork 的继承历史
// 不能在重开时丢失；随后注册追加写入与关闭清理。同 id 已活跃返回错误。
//
// 种子落盘在锁外做（长 I/O）：因此同一会话的并发追加有极小概率插在种子之前落盘，
// 框架内所有路径都是单写者，不受影响。
func (s *Store) Adopt(session *Session) (*Session, error) {
	id := session.ID()
	s.mu.Lock()
	if _, exists := s.sessions[id]; exists {
		s.mu.Unlock()
		return nil, fmt.Errorf("会话 %q 已存在", id)
	}
	s.sessions[id] = session
	s.attach(session)
	persistence := s.persistence
	s.mu.Unlock()

	s.persistSeed(persistence, id, session.Events())
	return session, nil
}

// Open 打开会话：已活跃则直接返回，否则从持久化载入事件作为种子（不重复落盘）。
func (s *Store) Open(id string) (*Session, error) {
	s.mu.Lock()
	if active, found := s.sessions[id]; found {
		s.mu.Unlock()
		return active, nil
	}
	persistence := s.persistence
	s.mu.Unlock()

	seed, err := loadSeed(persistence, id)
	if err != nil {
		return nil, err
	}
	session, err := New(id, WithSeed(seed))
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, found := s.sessions[id]; found {
		return existing, nil // 并发打开：以先到的为准
	}
	s.sessions[id] = session
	s.attach(session)
	return session, nil
}
