package core

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// Disposer 撤销函数：调用它应使系统恢复到副作用施加之前的状态。
//
// 同一个 Disposer 可能被多次调用（例如作用域释放后又手动调用），
// 因此实现应当是幂等的。
type Disposer func() error

// Ignore 把一个不产生错误的清理函数适配成 [Disposer]。
func Ignore(fn func()) Disposer {
	return func() error {
		fn()
		return nil
	}
}

// EffectReversibility 是效应的可逆性类别（回溯语义的元数据依据）：
// 回滚引擎与 UI 时间轴据此决定「恢复到过去」对一类效应意味着什么。
//
// 纯元数据，不影响回滚行为。
type EffectReversibility int

const (
	// ReversibilityInternal 纯内部状态：撤销函数即可完整回滚，零成本。
	ReversibilityInternal EffectReversibility = iota
	// ReversibilityRegenerable 可再生的衍生物：标记失效，用到时重新生成。
	ReversibilityRegenerable
	// ReversibilityCompensable 外部可补偿：不能撤回，只能补偿（Saga 模式）。
	ReversibilityCompensable
	// ReversibilityOneWayDoor 单向门：物理不可逆，回溯到此为止。
	ReversibilityOneWayDoor
)

var reversibilityNames = [...]string{"internal", "regenerable", "compensable", "oneWayDoor"}

func (r EffectReversibility) String() string {
	if int(r) < 0 || int(r) >= len(reversibilityNames) {
		return fmt.Sprintf("EffectReversibility(%d)", int(r))
	}
	return reversibilityNames[r]
}

// EffectScope 收集一组可逆副作用，并按 LIFO（后进先出）顺序统一撤销。
//
// 三条硬保证：
//
//   - 幂等：重复 [EffectScope.Dispose] 只真正释放一次；
//   - 迟到安全：作用域释放后再 [EffectScope.Track]，撤销函数立即执行，
//     避免异步初始化中的清理函数造成泄漏；
//   - 抗异常：单个撤销函数返回错误或 panic 都不会阻断其余撤销，
//     所有错误由 [EffectScope.Dispose] 聚合返回。
type EffectScope struct {
	id            string
	reversibility EffectReversibility

	mu        sync.Mutex
	disposers []Disposer
	disposed  bool
}

// ScopeOption 定制 [EffectScope]。
type ScopeOption func(*EffectScope)

// WithScopeID 指定作用域稳定身份（缺省自动分配，进程内单调唯一）。
func WithScopeID(id string) ScopeOption {
	return func(s *EffectScope) { s.id = id }
}

// WithReversibility 指定可逆性类别元数据。
func WithReversibility(r EffectReversibility) ScopeOption {
	return func(s *EffectScope) { s.reversibility = r }
}

// NewEffectScope 创建一个空作用域。
func NewEffectScope(opts ...ScopeOption) *EffectScope {
	scope := &EffectScope{id: nextScopeID(), reversibility: ReversibilityInternal}
	for _, opt := range opts {
		opt(scope)
	}
	return scope
}

// ID 返回作用域稳定身份。
func (s *EffectScope) ID() string { return s.id }

// Reversibility 返回可逆性类别元数据。
func (s *EffectScope) Reversibility() EffectReversibility { return s.reversibility }

// Disposed 返回作用域是否已被释放。
func (s *EffectScope) Disposed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.disposed
}

// Len 返回当前登记的撤销函数数量。
func (s *EffectScope) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.disposers)
}

// Track 登记一个撤销函数。
//
// 若作用域已经释放，d 会被**立即执行**（而不是被静默丢弃）。d 为 nil 属编程错误。
func (s *EffectScope) Track(d Disposer) {
	if d == nil {
		panic("core: Track 收到 nil Disposer")
	}
	if !s.enqueue(d) {
		_ = d()
	}
}

// Capture 执行 body；若它返回的是 [Disposer]，则自动登记。
func (s *EffectScope) Capture(body func() Disposer) Disposer {
	d := body()
	s.Track(d)
	return d
}

// Run 执行 body，不对其返回值做任何处理。
//
// 对应 Dart 版 `capture` 中「返回值不是 Disposer 就不登记」的那一半语义；
// 静态类型下这一分支不必靠运行时判断，故拆成显式方法。
func (s *EffectScope) Run(body func()) {
	body()
}

// Dispose 按 LIFO 顺序执行所有撤销函数，返回捕获到的全部错误。
//
// 幂等：重复调用只有第一次真正释放。单个撤销函数返回错误或 panic 都不阻断
// 其余撤销——卸载需要「尽可能多清理」。
func (s *EffectScope) Dispose() []error {
	if !s.markDisposed() {
		return nil
	}
	var errs []error
	for {
		d, ok := s.pop()
		if !ok {
			return errs
		}
		if err := runDisposer(d); err != nil {
			errs = append(errs, err)
		}
	}
}

func (s *EffectScope) enqueue(d Disposer) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disposed {
		return false
	}
	s.disposers = append(s.disposers, d)
	return true
}

func (s *EffectScope) markDisposed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	alreadyDisposed := s.disposed
	s.disposed = true
	return !alreadyDisposed
}

func (s *EffectScope) pop() (Disposer, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.disposers) == 0 {
		return nil, false
	}
	last := len(s.disposers) - 1
	d := s.disposers[last]
	s.disposers = s.disposers[:last]
	return d, true
}

func runDisposer(d Disposer) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("撤销函数 panic：%v", recovered)
		}
	}()
	return d()
}

var scopeSeq atomic.Uint64

func nextScopeID() string {
	return fmt.Sprintf("scope-%d", scopeSeq.Add(1)-1)
}
