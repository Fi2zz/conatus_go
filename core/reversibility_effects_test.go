package core_test

import "github.com/Fi2zz/conatus_go/core"

// effectKind 是可撤销效果的种类（与 Dart 版 EffectKind 一一对应）。
type effectKind int

const (
	// effectBumpShared 改写共享值：逆必须恢复**它自己看到的上一个值**（顺序敏感）。
	effectBumpShared effectKind = iota
	// effectProvideService 在子上下文提供服务（core.Context.Provide 自带登记）。
	effectProvideService
	// effectOpenStream 开一条订阅，逆取消订阅。
	effectOpenStream
	// effectStartTimer 起一个周期定时器，逆停止它。
	effectStartTimer
	// effectAcquireHandle 申请一个外部资源句柄，逆关闭它。
	effectAcquireHandle
	// effectRegisterName 往共享注册表登记一个名字，逆移除它。
	effectRegisterName
)

// applyEffect 施加一种效果，返回它的逆（**尚未**登记）。
//
// 除 effectProvideService 由 core.Context.Provide 自登记外，其余逆都由调用方决定
// 是否登记——这正是「漏登记」这一失败模式得以被构造的原因。
// REASON: 六种效果的一一映射表，拆开反而更难与 Dart 版逐条对照。
func applyEffect(w *effectWorld, ctx *core.Context, kind effectKind) core.Disposer {
	switch kind {
	case effectBumpShared:
		return bumpShared(w)
	case effectProvideService:
		return provideService(w, ctx)
	case effectOpenStream:
		return core.Ignore(w.openStream().Cancel)
	case effectStartTimer:
		return core.Ignore(w.startTimer().Cancel)
	case effectAcquireHandle:
		return core.Ignore(w.acquire().Close)
	default:
		return registerName(w)
	}
}

// bumpShared 改写共享值：逆恢复它自己看到的上一个值。
func bumpShared(w *effectWorld) core.Disposer {
	previous, existed := w.shared["shared"]
	w.shared["shared"] = previous + 1
	return func() error {
		if !existed {
			delete(w.shared, "shared")
			return nil
		}
		w.shared["shared"] = previous
		return nil
	}
}

func provideService(w *effectWorld, ctx *core.Context) core.Disposer {
	disposer, err := ctx.Provide(w.nextName("svc"), len(w.shared))
	if err != nil {
		panic("验证世界：provide 失败：" + err.Error())
	}
	return disposer
}

func registerName(w *effectWorld) core.Disposer {
	name := w.nextName("r")
	w.registry[name] = struct{}{}
	return func() error {
		delete(w.registry, name)
		return nil
	}
}
