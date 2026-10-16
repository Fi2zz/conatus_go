package agent

import (
	"context"

	"github.com/Fi2zz/conatus_go/foundation/prompt"
	"github.com/Fi2zz/conatus_go/foundation/session"
	"github.com/Fi2zz/conatus_go/foundation/tools"
	"github.com/Fi2zz/conatus_go/llm"
)

// 缺省装配。
const (
	// DefaultMaxSteps 是单轮最大模型调用步数。
	DefaultMaxSteps = 8
	// DefaultSystemPrompt 是没接 prompt 注册表时的兜底人设。
	DefaultSystemPrompt = "你是一个可靠的助手。需要外部信息或操作时调用工具；否则直接回答。"
)

// LoopOption 定制 [Loop]。
type LoopOption func(*Loop)

// WithSession 绑定会话；缺省不绑定（临时对话不落事件）。
func WithSession(target *session.Session) LoopOption {
	return func(l *Loop) { l.session = target }
}

// WithPrompt 接入 system prompt 注册表；缺省只用兜底人设。
func WithPrompt(registry *prompt.Registry) LoopOption {
	return func(l *Loop) { l.prompt = registry }
}

// WithMaxSteps 设置单轮最大模型调用步数（必须为正，否则 panic）。
func WithMaxSteps(steps int) LoopOption {
	return func(l *Loop) { l.maxSteps = steps }
}

// WithDefaultSystemPrompt 覆盖兜底人设。
func WithDefaultSystemPrompt(text string) LoopOption {
	return func(l *Loop) { l.defaultSystemPrompt = text }
}

// WithObserver 装配每轮的观察口。
func WithObserver(observe Observer) LoopOption {
	return func(l *Loop) { l.observer = observe }
}

// WithStreamObserver 装配流式增量回调：装了就走流式端点，供宿主实时渲染。
func WithStreamObserver(observe func(llm.StreamEvent)) LoopOption {
	return func(l *Loop) { l.streamObserver = observe }
}

// WithSteering 装配轮内插话来源：每步组请求前取回排队插话并注入。
func WithSteering(source SteeringSource) LoopOption {
	return func(l *Loop) { l.steering = source }
}

// WithTurnTracker 装配轮次生命周期钩子。
func WithTurnTracker(tracker TurnTracker) LoopOption {
	return func(l *Loop) { l.tracker = tracker }
}

// Loop 是 Agent 主循环。
//
// 构造后字段只读，因此天然并发安全。Dart 侧那些「由别的插件后置挂载」的接缝
// （steering / turnTracker）在 Go 里一律改成构造期装配：可变公共字段要么得加锁
// （而持锁调用外部代码会死锁），要么就有数据竞争。
type Loop struct {
	llm                 llm.Provider
	tools               *tools.Registry
	session             *session.Session
	prompt              *prompt.Registry
	maxSteps            int
	defaultSystemPrompt string
	observer            Observer
	streamObserver      func(llm.StreamEvent)
	steering            SteeringSource
	tracker             TurnTracker
}

// NewLoop 装配主循环：llm 与 tools 是必需依赖，其余用选项给。
func NewLoop(provider llm.Provider, registry *tools.Registry, opts ...LoopOption) *Loop {
	loop := &Loop{
		llm:                 provider,
		tools:               registry,
		maxSteps:            DefaultMaxSteps,
		defaultSystemPrompt: DefaultSystemPrompt,
	}
	for _, opt := range opts {
		opt(loop)
	}
	if loop.maxSteps <= 0 {
		panic("agent: maxSteps 必须为正数")
	}
	return loop
}

// Run 跑一轮：从 userInput 到最终文本回复。
//
// images 非空时随用户消息发给模型并写进会话事件（多模态输入）。ctx 结束（barge-in /
// 退出）时以 [ErrCancelled] 结束。
func (l *Loop) Run(ctx context.Context, userInput string, images []llm.Image) (Turn, error) {
	if err := l.beginTurn(ctx, userInput); err != nil {
		return Turn{}, err
	}
	turn, runErr := l.runOnce(ctx, userInput, images)
	endErr := l.endTurn(ctx, turn.Reply, runErr)
	if runErr != nil {
		return Turn{}, runErr
	}
	if endErr != nil {
		return Turn{}, endErr
	}
	return turn, nil
}

// runOnce 跑单轮：抄下历史 → 写用户事件 → 组开场消息 → 步进到收口。
func (l *Loop) runOnce(ctx context.Context, userInput string, images []llm.Image) (Turn, error) {
	if err := EnsureSessionOpen(l.session); err != nil {
		return Turn{}, err
	}
	// 历史先抄下来：用户事件随后就写进日志，等写完再派生会把本轮输入派生两遍。
	history := DeriveMessages(RecentEvents(l.session))
	if err := l.appendUserEvent(userInput, images); err != nil {
		return Turn{}, err
	}
	current := &round{
		loop:      l,
		ctx:       ctx,
		userInput: userInput,
		messages:  l.openingMessages(userInput, images, history),
	}
	return current.stepThrough()
}

// openingMessages 组装开场消息：system（可缓存前缀）+ 会话历史 + 本轮用户输入。
//
// system 上的 Cacheable 只是本地标记、不进请求体：它标明「稳定前缀到此为止」，
// 供缓存装饰器算前缀与命中率。
//
// 与 Dart 的差异（这是修坑，不是走样）：Dart 那份的 messages 完全由事件日志派生，
// 于是没绑定会话（临时对话）时**用户输入根本发不出去**——日志里就没有那条事件。
// 这里显式带上本轮用户消息，历史又是在写事件之前抄的，所以不会重复。
func (l *Loop) openingMessages(
	userInput string,
	images []llm.Image,
	history []llm.Message,
) []llm.Message {
	system := llm.Message{
		Role:      llm.RoleSystem,
		Content:   BuildSystemText(l.defaultSystemPrompt, l.prompt),
		Cacheable: true,
	}
	opening := append([]llm.Message{system}, history...)
	return append(opening, llm.Message{Role: llm.RoleUser, Content: userInput, Images: images})
}

// appendUserEvent 把用户消息写进会话。
func (l *Loop) appendUserEvent(userInput string, images []llm.Image) error {
	data := map[string]any{"text": userInput}
	if len(images) > 0 {
		data["images"] = ImagesToJSON(images)
	}
	return l.appendEvent(session.EventUserMessage, data)
}

// appendEvent 写一条会话事件；没绑定会话（临时对话）时什么也不做。
//
// 会话已关闭时统一报 [ErrSessionClosed]（session 包自己那条错误只说「无法追加」，
// 循环的调用方需要一个稳定的判据），其余写入失败原样上抛。
func (l *Loop) appendEvent(eventType string, data map[string]any) error {
	if l.session == nil {
		return nil
	}
	if err := EnsureSessionOpen(l.session); err != nil {
		return err
	}
	_, err := l.session.Append(eventType, session.WithEventData(data))
	return err
}

// observe 通报一次观察事件；没装观察口时什么也不做。
func (l *Loop) observe(kind string, data map[string]any) {
	if l.observer != nil {
		l.observer(kind, data)
	}
}

// beginTurn 通知追踪器一轮开始（没装追踪器时什么也不做）。
func (l *Loop) beginTurn(ctx context.Context, userInput string) error {
	if l.tracker == nil {
		return nil
	}
	return l.tracker.BeginTurn(ctx, userInput)
}

// endTurn 通知追踪器一轮结束。
func (l *Loop) endTurn(ctx context.Context, reply string, cause error) error {
	if l.tracker == nil {
		return nil
	}
	return l.tracker.EndTurn(ctx, reply, cause)
}
