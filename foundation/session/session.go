package session

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// SessionOption 定制 [Session]。
type SessionOption func(*sessionConfig)

type sessionConfig struct {
	seed      []Event
	inherited int
	clock     func() time.Time
}

// WithSeed 用既有事件作为种子（重开或 fork 时用）。
func WithSeed(events []Event) SessionOption {
	return func(c *sessionConfig) { c.seed = events }
}

// WithInheritedEventCount 声明种子前若干条属于父会话（fork 时用）。
func WithInheritedEventCount(count int) SessionOption {
	return func(c *sessionConfig) { c.inherited = count }
}

// WithSessionClock 注入时钟（只影响空会话的创建时间）。
func WithSessionClock(clock func() time.Time) SessionOption {
	return func(c *sessionConfig) { c.clock = clock }
}

// Session 是一次会话：持有 append-only 的事件日志与监听器。
//
// 读路径不加锁（读的是原子发布的不可变快照），写路径在会话锁内串行。因此监听器里
// 可以安全地**读**会话，但**不得重入** Append / Close / Fork。
type Session struct {
	id        string
	inherited int
	createdAt time.Time

	snapshot   atomic.Pointer[[]Event]
	closedFlag atomic.Bool
	mu         sync.Mutex
	events     []Event
	eventSinks []*eventSink
	closeSinks []*closeSink
	seq        int
	forks      int
}

// New 构造一个会话；种子事件会续接 seq 与创建时间。
func New(id string, opts ...SessionOption) (*Session, error) {
	config := sessionConfig{clock: time.Now}
	for _, opt := range opts {
		opt(&config)
	}
	if config.inherited < 0 || config.inherited > len(config.seed) {
		return nil, errors.New("session: 继承事件条数必须落在种子范围内")
	}

	session := &Session{id: id, inherited: config.inherited}
	session.events = append([]Event(nil), config.seed...)
	if len(session.events) > 0 {
		session.seq = session.events[len(session.events)-1].Seq + 1
		session.createdAt = session.events[0].Time
	} else {
		session.createdAt = config.clock()
	}
	session.publish()
	return session, nil
}

// ID 返回会话标识（也是持久化文件名）。
func (s *Session) ID() string { return s.id }

// InheritedEventCount 返回由种子继承的父会话事件条数。
func (s *Session) InheritedEventCount() int { return s.inherited }

// CreatedAt 返回会话创建时间（取首条事件时间）。
func (s *Session) CreatedAt() time.Time { return s.createdAt }

// Events 返回事件日志的副本（读不加锁，可在监听器里安全调用）。
func (s *Session) Events() []Event {
	return append([]Event(nil), *s.snapshot.Load()...)
}

// OwnEvents 返回本会话自身拥有的事件（跳过继承前缀）。
func (s *Session) OwnEvents() []Event {
	events := *s.snapshot.Load()
	if s.inherited >= len(events) {
		return nil
	}
	return append([]Event(nil), events[s.inherited:]...)
}

// Len 返回事件条数。
func (s *Session) Len() int { return len(*s.snapshot.Load()) }

// Closed 返回会话是否已关闭。
func (s *Session) Closed() bool { return s.closedFlag.Load() }

// LastEventID 返回最后一条事件的 id；空日志返回 false。
func (s *Session) LastEventID() (string, bool) {
	events := *s.snapshot.Load()
	if len(events) == 0 {
		return "", false
	}
	return events[len(events)-1].ID, true
}

// Append 追加一条事件并返回它；会话关闭后返回错误。
//
// 监听器在追加锁内被调用：它们因此看到与 seq 一致的顺序，并且可以安全地读会话
// （快照读取不加锁）；但不得重入 Append / Close / Fork。
func (s *Session) Append(eventType string, opts ...EventOption) (Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closedFlag.Load() {
		return Event{}, fmt.Errorf("会话 %q 已关闭，无法追加事件", s.id)
	}
	event := NewEvent(s.id, eventType, s.seq, opts...)
	s.seq++
	s.record(event)
	return event, nil
}

// AppendEvent 追加一条已构造的事件，返回落定后的事件。
//
// 事件若不是本会话所有，会用本会话 id 与下一个 seq 重新盖章。这使 SessionLog 与
// 反序列化路径可以安全回填事件。
func (s *Session) AppendEvent(event Event) (Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closedFlag.Load() {
		return Event{}, fmt.Errorf("会话 %q 已关闭，无法追加事件", s.id)
	}
	stamped := event
	stamped.SessionID = s.id
	stamped.Seq = s.seq
	if stamped.ID == "" {
		stamped.ID = NextEventID()
	}
	s.seq++
	s.record(stamped)
	return stamped, nil
}

func (s *Session) record(event Event) {
	s.events = append(s.events, event)
	s.publish()
	for _, sink := range append([]*eventSink(nil), s.eventSinks...) {
		sink.fn(event)
	}
}

// publish 发布新的不可变快照：读路径因此不必加锁，也就不会与监听器互相阻塞。
func (s *Session) publish() {
	snapshot := append([]Event(nil), s.events...)
	s.snapshot.Store(&snapshot)
}
