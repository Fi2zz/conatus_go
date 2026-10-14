package core_test

import (
	"fmt"
	"math/rand"

	"github.com/Fi2zz/conatus_go/core"
)

// pluginMode 是插件的行为模式，用于构造真实失败模式与负向对照。
type pluginMode int

const (
	// modeCorrect 正确：每个效果的逆都被登记。
	modeCorrect pluginMode = iota
	// modePlacebo 安慰剂：登记了逆，但它什么都不做。
	modePlacebo
	// modeUnregistered 漏登记：效果施加了，逆从未登记。
	modeUnregistered
	// modeHalfRestore 半个逆：只登记第一个效果的逆，其余用安慰剂顶替。
	modeHalfRestore
)

// pluginSpec 是一个待挂载的插件。
type pluginSpec struct {
	name    string
	effects []effectKind
	mode    pluginMode
}

func newPluginSpec(name string, effects ...effectKind) pluginSpec {
	return pluginSpec{name: name, effects: effects}
}

func newPluginSpecWithMode(name string, mode pluginMode, effects ...effectKind) pluginSpec {
	return pluginSpec{name: name, effects: effects, mode: mode}
}

// playback 是一次回放的两端轨迹。
type playback struct {
	mounted   [][]string
	unmounted [][]string
}

// reversibilityHarness 驱动插件挂载与卸载，并按投影逐前缀比对。
//
// oracle 按论文 Theorem 16 取：先记下「只装了前 k 个插件」时的投影，逆序卸载后
// 第 k 步必须回到该投影——比「最终等于初始」强得多，能杀掉「几个逆互相抵消」与
// 「只在整体 LIFO 下才成立」的假可逆。
type reversibilityHarness struct {
	root  *core.Context
	world *effectWorld

	mounted []*core.Context

	// tracked 是夹具自检用：真正交给作用域登记的逆的个数。它观察的是**夹具自己**
	// 的行为，用于钉住三种失败模式确实被构造成了不同形态。
	tracked int
}

func newReversibilityHarness() *reversibilityHarness {
	return &reversibilityHarness{root: core.NewRoot("root"), world: newEffectWorld()}
}

// project 返回当前独立可观测投影。
func (h *reversibilityHarness) project() []string {
	contexts := append([]*core.Context{h.root}, h.mounted...)
	return h.world.project(contexts)
}

// mount 把 spec 挂载为一个子上下文（效果与逆都落在其中）。
func (h *reversibilityHarness) mount(spec pluginSpec) *core.Context {
	ctx, err := h.root.Plugin(spec.name, func(c *core.Context) error {
		for index, kind := range spec.effects {
			h.register(c, applyEffect(h.world, c, kind), spec, index)
		}
		return nil
	})
	if err != nil {
		panic("验证夹具：挂载插件失败：" + err.Error())
	}
	h.mounted = append(h.mounted, ctx)
	return ctx
}

// play 挂载全部插件，再按 unmountOrder（缺省 LIFO）卸载，返回两端轨迹。
func (h *reversibilityHarness) play(specs []pluginSpec, unmountOrder []int) playback {
	here := make([]*core.Context, 0, len(specs))
	mounted := [][]string{h.project()}
	for _, spec := range specs {
		here = append(here, h.mount(spec))
		mounted = append(mounted, h.project())
	}
	order := unmountOrder
	if order == nil {
		order = lifoOrder(len(specs))
	}
	unmounted := make([][]string, 0, len(order))
	for _, index := range order {
		here[index].Dispose()
		unmounted = append(unmounted, h.project())
	}
	return playback{mounted: mounted, unmounted: unmounted}
}

// runLifo 是 Theorem 16 的 oracle：逆序卸载后第 k 步必须等于「只装了前 n−k 个」。
// 返回违规描述；空切片表示通过。
func (h *reversibilityHarness) runLifo(specs []pluginSpec) []string {
	result := h.play(specs, nil)
	var problems []string
	for step := range result.unmounted {
		index := len(specs) - 1 - step
		problems = append(problems,
			diffProjection(specs[index].name, result.mounted[index], result.unmounted[step])...)
	}
	return problems
}

// teardown 取消定时器、关闭订阅，避免泄漏的周期定时器与 goroutine 拖住测试进程。
func (h *reversibilityHarness) teardown() { h.world.teardown() }

func (h *reversibilityHarness) register(ctx *core.Context, inverse core.Disposer, spec pluginSpec, index int) {
	if !registersInverse(spec.mode) {
		return
	}
	ctx.Track(h.effective(inverse, spec.mode, index))
	h.tracked++
}

// registersInverse 报告该模式是否真的把逆交给作用域登记——三种失败模式里只有
// 漏登记一个都不登记，夹具自检据此自证构造有效。
func registersInverse(mode pluginMode) bool {
	return mode != modeUnregistered
}

// effective 按模式决定实际登记哪个逆。
func (h *reversibilityHarness) effective(inverse core.Disposer, mode pluginMode, index int) core.Disposer {
	if mode == modeCorrect {
		return inverse
	}
	if mode == modeHalfRestore && index == 0 {
		return inverse
	}
	return placebo
}

func placebo() error { return nil }

func lifoOrder(count int) []int {
	order := make([]int, 0, count)
	for i := count - 1; i >= 0; i-- {
		order = append(order, i)
	}
	return order
}

func diffProjection(name string, expected, actual []string) []string {
	if len(expected) != len(actual) {
		return []string{fmt.Sprintf("卸载 %s 后投影行数从 %d 变成 %d", name, len(expected), len(actual))}
	}
	var problems []string
	for i := range expected {
		if expected[i] != actual[i] {
			problems = append(problems, fmt.Sprintf(
				"卸载 %s 后未还原，第 %d 行：期望「%s」实际「%s」", name, i, expected[i], actual[i]))
		}
	}
	return problems
}

// randomPlugins 用 seed 生成 count 个随机插件（每个 1–3 个效果，名字唯一）。
func randomPlugins(seed, count int) []pluginSpec {
	source := rand.New(rand.NewSource(int64(seed)))
	kinds := []effectKind{
		effectBumpShared, effectProvideService, effectOpenStream,
		effectStartTimer, effectAcquireHandle, effectRegisterName,
	}
	specs := make([]pluginSpec, 0, count)
	for i := 0; i < count; i++ {
		specs = append(specs, newPluginSpec(fmt.Sprintf("p%d", i), randomEffects(source, kinds)...))
	}
	return specs
}

func randomEffects(source *rand.Rand, kinds []effectKind) []effectKind {
	effects := make([]effectKind, 0, 3)
	for k := 0; k <= source.Intn(3); k++ {
		effects = append(effects, kinds[source.Intn(len(kinds))])
	}
	return effects
}
