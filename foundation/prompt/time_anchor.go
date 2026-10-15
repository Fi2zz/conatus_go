package prompt

import (
	"fmt"
	"time"

	"github.com/Fi2zz/conatus_go/core"
)

// 时间锚点的名字与排序权重（靠前）。
const (
	// TimeContextName 是时间锚点在 system prompt 里的名字。
	TimeContextName = "time"
	// TimeContextOrder 是时间锚点在上下文之间的排序权重。
	TimeContextOrder = -10
)

// TimeAnchorOption 定制 [ProvideTimePrompt]。
type TimeAnchorOption func(*timeAnchorSettings)

type timeAnchorSettings struct {
	registry *Registry
	clock    func() time.Time
	zoneName string
}

// WithTimePrompt 指定目标注册表（缺省取上下文里的 [ServiceKey]）。
func WithTimePrompt(registry *Registry) TimeAnchorOption {
	return func(s *timeAnchorSettings) { s.registry = registry }
}

// WithTimeClock 注入时钟（缺省 time.Now）。
func WithTimeClock(clock func() time.Time) TimeAnchorOption {
	return func(s *timeAnchorSettings) { s.clock = clock }
}

// WithZoneName 指定展示用的时区名（如 `Asia/Shanghai`）；空串表示用本地时区名。
func WithZoneName(name string) TimeAnchorOption {
	return func(s *timeAnchorSettings) { s.zoneName = name }
}

// ProvideTimePrompt 把日粒度的当前日期注册为一份动态上下文（日期 + 星期 + 时区）。
//
// 模型没有时钟，相对日期（「明天」「下周三」）与带本地语义的时刻（「明早九点」）
// 都需要外部锚点才能换算成绝对时间。锚点只精确到日：system 是可缓存前缀，秒级变化
// 会让缓存每轮失效；精确到秒请交给时间工具。
//
// 返回撤销函数（幂等）。
func ProvideTimePrompt(ctx *core.Context, opts ...TimeAnchorOption) (core.Disposer, error) {
	settings := timeAnchorSettings{}
	for _, opt := range opts {
		opt(&settings)
	}
	registry := settings.registry
	if registry == nil {
		found, err := From(ctx)
		if err != nil {
			return nil, err
		}
		registry = found
	}
	clock := settings.clock
	if clock == nil {
		clock = time.Now
	}
	zoneName := settings.zoneName

	return registry.Context(Context{
		Name:  TimeContextName,
		Order: TimeContextOrder,
		Text:  func() string { return RenderTimeAnchor(clock(), zoneName) },
	})
}

// RenderTimeAnchor 渲染时间锚点文本：`[当前时间]\n<日期> 周<星期> · <时区> (UTC±HH:MM)`。
//
// 时区名缺省取该时刻自己的时区名；偏移同样取自该时刻（而不是运行环境的偏移）。
func RenderTimeAnchor(now time.Time, zoneName string) string {
	ownZone, offset := now.Zone()
	zone := zoneName
	if zone == "" {
		zone = ownZone
	}
	return fmt.Sprintf("[当前时间]\n%s 周%s · %s (UTC%s)",
		now.Format("2006-01-02"), weekdayName(now), zone,
		FormatClockOffset(time.Duration(offset)*time.Second))
}

// FormatClockOffset 把时区偏移格式化为 `±HH:MM`（例如 `+08:00`、`-05:30`）。
func FormatClockOffset(offset time.Duration) string {
	sign := "+"
	if offset < 0 {
		sign = "-"
	}
	minutes := int(offset.Abs().Minutes())
	return fmt.Sprintf("%s%02d:%02d", sign, minutes/60, minutes%60)
}

// weekdayName 返回中文星期（周一是「一」，周日是「日」）。
//
// Go 的 time.Weekday 以周日为 0，Dart 的 DateTime.weekday 以周一为 1，这里做一次换算。
func weekdayName(now time.Time) string {
	return weekdayNames[(int(now.Weekday())+6)%7]
}

var weekdayNames = [...]string{"一", "二", "三", "四", "五", "六", "日"}
