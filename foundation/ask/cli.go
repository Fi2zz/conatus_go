package ask

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// CLIAskUser 是面向命令行/文本界面的默认提问器。
//
// 它只做两件事：把提示写出去，然后等一行输入进来——真正的输入源由宿主投递
// （[CLIAskUser.Submit]），因此 GUI 事件循环、管道与测试替身都能驱动同一份实现。
// Dart 版同样如此：CliAskUser 从不读 stdin，只写 stdout。
type CLIAskUser struct {
	mu        sync.Mutex
	writer    io.Writer
	pending   []chan answer
	cancelled bool
}

// answer 是一次提问的结局：值或取消。
type answer struct {
	value string
	err   error
}

// CLIOption 定制 [NewCLIAskUser]。
type CLIOption func(*CLIAskUser)

// WithPromptWriter 指定提示的输出目标（缺省 os.Stdout）。
func WithPromptWriter(writer io.Writer) CLIOption {
	return func(a *CLIAskUser) { a.writer = writer }
}

// NewCLIAskUser 创建提问器。
func NewCLIAskUser(opts ...CLIOption) *CLIAskUser {
	instance := &CLIAskUser{writer: os.Stdout}
	for _, opt := range opts {
		opt(instance)
	}
	return instance
}

// Ask 输出 prompt 并等待一行输入；等待期间被 [CLIAskUser.Cancel] 打断时返回
// [ErrCancelled]。
//
// 已取消之后再调用是**立即**失败（Dart 的 ask 此时同步 throw，不返回失败的 Future）。
func (a *CLIAskUser) Ask(prompt string) (string, error) {
	slot, writer, err := a.begin()
	if err != nil {
		return "", err
	}
	if err := writePrompt(writer, prompt); err != nil {
		a.retire(slot)
		return "", err
	}
	result := <-slot
	return result.value, result.err
}

// Submit 把一行输入投递给最早等待中的提问，返回是否确实投递。
//
// Dart 版无返回值、空队列时静默忽略；Go 侧把「没人等」报出来，便于诊断投递时序。
func (a *CLIAskUser) Submit(line string) bool {
	a.mu.Lock()
	if len(a.pending) == 0 {
		a.mu.Unlock()
		return false
	}
	slot := a.pending[0]
	a.pending = a.pending[1:]
	a.mu.Unlock()

	slot <- answer{value: line}
	return true
}

// Pending 返回等待中的提问数（供界面提示「正在等待输入」用）。
func (a *CLIAskUser) Pending() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.pending)
}

// Cancel 取消所有等待中的提问，并使后续 [CLIAskUser.Ask] 直接失败。幂等。
func (a *CLIAskUser) Cancel() {
	a.mu.Lock()
	a.cancelled = true
	waiting := a.pending
	a.pending = nil
	a.mu.Unlock()

	for _, slot := range waiting {
		slot <- answer{err: ErrCancelled}
	}
}

// begin 登记一个等待槽位；已取消时直接失败，不留下无人认领的槽位。
func (a *CLIAskUser) begin() (chan answer, io.Writer, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelled {
		return nil, nil, ErrCancelled
	}
	slot := make(chan answer, 1)
	a.pending = append(a.pending, slot)
	return slot, a.writer, nil
}

// retire 撤下等待槽位（提示写失败时用，避免槽位永远没人投递）。
func (a *CLIAskUser) retire(slot chan answer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	kept := make([]chan answer, 0, len(a.pending))
	for _, waiting := range a.pending {
		if waiting != slot {
			kept = append(kept, waiting)
		}
	}
	a.pending = kept
}

func writePrompt(writer io.Writer, prompt string) error {
	if _, err := fmt.Fprintln(writer, prompt); err != nil {
		return fmt.Errorf("ask: 输出提示失败：%w", err)
	}
	return nil
}
