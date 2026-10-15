package session

import (
	"fmt"
	"time"
)

// Read 按时间过滤事件；零值表示该侧不设边界。
func (s *Session) Read(from, to time.Time) []Event {
	filtered := []Event{}
	for _, event := range *s.snapshot.Load() {
		if !from.IsZero() && event.Time.Before(from) {
			continue
		}
		if !to.IsZero() && event.Time.After(to) {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

// Replay 按事件顺序依次回调 handler；日志不被改写。
func (s *Session) Replay(handler func(Event)) {
	for _, event := range *s.snapshot.Load() {
		handler(event)
	}
}

// ForkOption 定制派生会话。
type ForkOption func(*forkConfig)

type forkConfig struct {
	fromEventID string
	id          string
}

// WithForkPoint 指定从哪个事件（含）处 fork；缺省取全部事件。
func WithForkPoint(eventID string) ForkOption {
	return func(c *forkConfig) { c.fromEventID = eventID }
}

// WithForkID 指定派生会话 id；缺省 `<id>-fork-<n>`。
func WithForkID(id string) ForkOption {
	return func(c *forkConfig) { c.id = id }
}

// Fork 派生一个新会话：以「截止指定事件（含）」的事件为种子，与原会话完全解耦，
// 原会话不受影响。找不到该事件时返回错误。
//
// 派生会话的种子条数记进 [Session.InheritedEventCount]，因此 [Session.OwnEvents]
// 只包含新追加的后缀——派生状态（任务树 / 目标 / 计划模式）只折叠自身后缀，这正是
// 「fork 不继承父会话活动状态」的实现基础。
func (s *Session) Fork(opts ...ForkOption) (*Session, error) {
	config := forkConfig{}
	for _, opt := range opts {
		opt(&config)
	}
	events := *s.snapshot.Load()
	seed := events
	if config.fromEventID != "" {
		index := indexOfEvent(events, config.fromEventID)
		if index < 0 {
			return nil, fmt.Errorf("会话 %q 中不存在事件 %q", s.id, config.fromEventID)
		}
		seed = events[:index+1]
	}

	s.mu.Lock()
	s.forks++
	forks := s.forks
	s.mu.Unlock()

	id := config.id
	if id == "" {
		id = fmt.Sprintf("%s-fork-%d", s.id, forks)
	}
	return New(id, WithSeed(seed), WithInheritedEventCount(len(seed)))
}

func indexOfEvent(events []Event, eventID string) int {
	for index, event := range events {
		if event.ID == eventID {
			return index
		}
	}
	return -1
}
