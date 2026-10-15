package prompt_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Fi2zz/conatus_go/core"
	"github.com/Fi2zz/conatus_go/foundation/prompt"
)

func fixedClock(moment time.Time) func() time.Time {
	return func() time.Time { return moment }
}

func TestTimeAnchorRendersDateWeekdayAndZone(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	registry, err := prompt.Provide(ctx)
	if err != nil {
		t.Fatalf("provide 失败：%v", err)
	}

	// 用 UTC 时刻注入，偏移固定为 +00:00，断言不受运行环境时区影响。
	if _, err := prompt.ProvideTimePrompt(ctx,
		prompt.WithTimePrompt(registry),
		prompt.WithTimeClock(fixedClock(time.Date(2026, 9, 16, 6, 3, 22, 0, time.UTC))),
		prompt.WithZoneName("Asia/Shanghai"),
	); err != nil {
		t.Fatalf("注册时间锚点失败：%v", err)
	}

	contexts := registry.Contexts()
	if len(contexts) != 1 || contexts[0].Name != prompt.TimeContextName {
		t.Fatalf("上下文 = %v，期望只有 %q", contexts, prompt.TimeContextName)
	}
	want := "[当前时间]\n2026-09-16 周三 · Asia/Shanghai (UTC+00:00)"
	if got := registry.Assemble(nil).Contexts[0].Text; got != want {
		t.Fatalf("锚点文本 = %q，期望 %q", got, want)
	}
}

func TestTimeAnchorReevaluatesAcrossDays(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	registry, _ := prompt.Provide(ctx)
	day := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	if _, err := prompt.ProvideTimePrompt(ctx,
		prompt.WithTimePrompt(registry),
		prompt.WithTimeClock(func() time.Time { return day }),
		prompt.WithZoneName("UTC"),
	); err != nil {
		t.Fatalf("注册失败：%v", err)
	}

	if got := registry.RenderContexts(registry.Assemble(nil)); !strings.Contains(got, "2026-09-16") {
		t.Fatalf("锚点未渲染当天：%q", got)
	}

	day = time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	if got := registry.RenderContexts(registry.Assemble(nil)); !strings.Contains(got, "2026-09-17") {
		t.Fatalf("锚点应每轮重新求值（跨天自动更新），得到 %q", got)
	}
}

func TestTimeAnchorDefaultsToLocalZoneName(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	registry, _ := prompt.Provide(ctx)
	if _, err := prompt.ProvideTimePrompt(ctx,
		prompt.WithTimePrompt(registry),
		prompt.WithTimeClock(fixedClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))),
	); err != nil {
		t.Fatalf("注册失败：%v", err)
	}

	text := registry.Assemble(nil).Contexts[0].Text

	if !strings.HasPrefix(text, "[当前时间]\n2026-01-01") {
		t.Fatalf("锚点 = %q，期望以日期开头", text)
	}
	if !strings.Contains(text, "(UTC+00:00)") {
		t.Fatalf("锚点 = %q，期望包含 UTC 偏移", text)
	}
}

func TestTimeAnchorRevocationRemovesContribution(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	registry, _ := prompt.Provide(ctx)
	off, err := prompt.ProvideTimePrompt(ctx, prompt.WithTimePrompt(registry))
	if err != nil {
		t.Fatalf("注册失败：%v", err)
	}

	if err := off(); err != nil {
		t.Fatalf("撤销失败：%v", err)
	}

	if len(registry.Contexts()) != 0 {
		t.Fatal("撤销后不该还有上下文")
	}
	if got := registry.RenderContexts(registry.Assemble(nil)); got != "" {
		t.Fatalf("撤销后不该贡献内容：%q", got)
	}
}

func TestTimeAnchorSortsAheadOfOrdinaryContexts(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	registry, _ := prompt.Provide(ctx)
	if _, err := prompt.ProvideTimePrompt(ctx, prompt.WithTimePrompt(registry)); err != nil {
		t.Fatalf("注册失败：%v", err)
	}
	if _, err := registry.Context(prompt.Context{
		Name:  "ordinary",
		Order: 0,
		Text:  func() string { return "普通上下文" },
	}); err != nil {
		t.Fatalf("注册失败：%v", err)
	}

	names := []string{}
	for _, context := range registry.Assemble(nil).Contexts {
		names = append(names, context.Name)
	}

	if want := []string{prompt.TimeContextName, "ordinary"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("上下文顺序 = %v，期望 %v（时间锚点 order = %d，靠前）",
			names, want, prompt.TimeContextOrder)
	}
}

func TestTimeAnchorRequiresPromptService(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)

	if _, err := prompt.ProvideTimePrompt(ctx); err == nil {
		t.Fatal("未提供 systemPrompt 服务时应返回错误")
	}
}

func TestFormatClockOffset(t *testing.T) {
	cases := []struct {
		offset time.Duration
		want   string
	}{
		{8 * time.Hour, "+08:00"},
		{-(5*time.Hour + 30*time.Minute), "-05:30"},
		{0, "+00:00"},
		{-(30 * time.Minute), "-00:30"},
	}
	for _, item := range cases {
		if got := prompt.FormatClockOffset(item.offset); got != item.want {
			t.Fatalf("FormatClockOffset(%v) = %q，期望 %q", item.offset, got, item.want)
		}
	}
}
