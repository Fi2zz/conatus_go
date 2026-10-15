package prompt_test

import (
	"reflect"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
	"github.com/Fi2zz/conatus_go/foundation/prompt"
)

func staticSection(name string, order int, text string) prompt.Section {
	return prompt.Section{Name: name, Order: order, Text: func() string { return text }}
}

func staticContext(name string, order int, text string) prompt.Context {
	return prompt.Context{Name: name, Order: order, Text: func() string { return text }}
}

func TestSectionRegistrationAndRevocation(t *testing.T) {
	registry := prompt.NewRegistry()

	off, err := registry.Section(staticSection("a", 0, "A"))
	if err != nil {
		t.Fatalf("注册失败：%v", err)
	}
	if got := registry.Sections(); len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("已注册段 = %v，期望 [a]", got)
	}

	if err := off(); err != nil {
		t.Fatalf("撤销失败：%v", err)
	}
	if len(registry.Sections()) != 0 {
		t.Fatal("撤销后应为空")
	}
	if err := off(); err != nil {
		t.Fatalf("重复撤销应幂等：%v", err)
	}
}

func TestSectionRejectsDuplicateAndAnonymous(t *testing.T) {
	registry := prompt.NewRegistry()
	if _, err := registry.Section(staticSection("a", 0, "A")); err != nil {
		t.Fatalf("注册失败：%v", err)
	}

	if _, err := registry.Section(staticSection("a", 0, "B")); err == nil {
		t.Fatal("同名重复注册应返回错误")
	}
	if _, err := registry.Section(prompt.Section{Text: func() string { return "x" }}); err == nil {
		t.Fatal("缺名字应返回错误")
	}
	if len(registry.Sections()) != 1 {
		t.Fatalf("失败的注册不该留下痕迹：%v", registry.Sections())
	}
}

func TestStaleDisposerDoesNotRemoveReregisteredSection(t *testing.T) {
	registry := prompt.NewRegistry()
	off, _ := registry.Section(staticSection("a", 0, "A"))
	if err := off(); err != nil {
		t.Fatalf("撤销失败：%v", err)
	}
	if _, err := registry.Section(staticSection("a", 0, "新")); err != nil {
		t.Fatalf("重新注册失败：%v", err)
	}

	if err := off(); err != nil {
		t.Fatalf("迟到撤销不该报错：%v", err)
	}
	if len(registry.Sections()) != 1 {
		t.Fatal("迟到的旧撤销误删了重新注册的同名段")
	}
}

func TestAddGeneratesUniqueNamesAndRemoves(t *testing.T) {
	registry := prompt.NewRegistry()

	first, err := registry.Add("第一段")
	if err != nil {
		t.Fatalf("添加失败：%v", err)
	}
	second, err := registry.Add("第二段")
	if err != nil {
		t.Fatalf("添加失败：%v", err)
	}
	if first != "add-0" || second != "add-1" {
		t.Fatalf("自动段名 = (%q, %q)，期望 (add-0, add-1)", first, second)
	}
	if !registry.Remove(first) {
		t.Fatal("移除已注册的段应返回 true")
	}
	if registry.Remove(first) {
		t.Fatal("重复移除应返回 false")
	}

	third, err := registry.Add("第三段")
	if err != nil {
		t.Fatalf("添加失败：%v", err)
	}
	if third != "add-2" {
		t.Fatalf("自动段名 = %q，期望 add-2（撤销后不复用序号）", third)
	}

	named, err := registry.Add("带名字", prompt.WithSectionName("custom"), prompt.WithSectionOrder(5))
	if err != nil || named != "custom" {
		t.Fatalf("显式段名 = (%q, %v)", named, err)
	}
	if _, err := registry.Add("又来了", prompt.WithSectionName("custom")); err == nil {
		t.Fatal("显式段名重复应返回错误")
	}
}

func TestContextRegistrationRejectsDuplicateAndRevokes(t *testing.T) {
	registry := prompt.NewRegistry()

	off, err := registry.Context(staticContext("c", 0, "C"))
	if err != nil {
		t.Fatalf("注册失败：%v", err)
	}
	if _, err := registry.Context(staticContext("c", 0, "C2")); err == nil {
		t.Fatal("同名重复注册应返回错误")
	}
	if err := off(); err != nil {
		t.Fatalf("撤销失败：%v", err)
	}
	if len(registry.Contexts()) != 0 {
		t.Fatal("撤销后应为空")
	}
}

func TestAssembleSortsByOrderThenName(t *testing.T) {
	registry := prompt.NewRegistry()
	for _, section := range []prompt.Section{
		staticSection("b", 1, "B"),
		staticSection("a", 1, "A"),
		staticSection("z", 0, "Z"),
	} {
		if _, err := registry.Section(section); err != nil {
			t.Fatalf("注册失败：%v", err)
		}
	}
	for _, context := range []prompt.Context{
		staticContext("second", 10, "2"),
		staticContext("first", -10, "1"),
	} {
		if _, err := registry.Context(context); err != nil {
			t.Fatalf("注册失败：%v", err)
		}
	}

	assembly := registry.Assemble(nil)

	if got := names(assembly.Sections); !reflect.DeepEqual(got, []string{"z", "a", "b"}) {
		t.Fatalf("段顺序 = %v，期望 [z a b]（order 升序，同序按名字）", got)
	}
	if got := contextNames(assembly.Contexts); !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatalf("上下文顺序 = %v，期望 [first second]", got)
	}
}

func TestAssembleReevaluatesProvidersEachTime(t *testing.T) {
	registry := prompt.NewRegistry()
	step := 0
	if _, err := registry.Section(prompt.Section{
		Name: "live",
		Text: func() string { step++; return "第 " + string(rune('0'+step)) + " 次" },
	}); err != nil {
		t.Fatalf("注册失败：%v", err)
	}

	first := registry.Assemble(nil).Sections[0].Text
	second := registry.Assemble(nil).Sections[0].Text

	if first == second {
		t.Fatalf("每次装配都应重新求值，两次都是 %q", first)
	}
}

func TestRenderJoinsSectionsAndInterpolates(t *testing.T) {
	registry := prompt.NewRegistry()
	if _, err := registry.Section(staticSection("a", 0, "你好，{{who}}")); err != nil {
		t.Fatalf("注册失败：%v", err)
	}
	if _, err := registry.Section(staticSection("b", 1, "{{missing}} 保留原样")); err != nil {
		t.Fatalf("注册失败：%v", err)
	}

	assembly := registry.Assemble(map[string]string{"who": "助手"})

	want := "你好，助手\n\n{{missing}} 保留原样"
	if got := registry.Render(assembly); got != want {
		t.Fatalf("渲染 = %q，期望 %q", got, want)
	}
	if got := registry.Render(assembly, prompt.WithSeparator(" | ")); got != "你好，助手 | {{missing}} 保留原样" {
		t.Fatalf("自定义分隔符渲染 = %q", got)
	}
}

func TestRenderContextsSkipsEmptyText(t *testing.T) {
	registry := prompt.NewRegistry()
	for _, context := range []prompt.Context{
		staticContext("empty", 0, ""),
		staticContext("filled", 1, "有内容"),
		staticContext("alsoEmpty", 2, ""),
	} {
		if _, err := registry.Context(context); err != nil {
			t.Fatalf("注册失败：%v", err)
		}
	}

	if got := registry.RenderContexts(registry.Assemble(nil)); got != "有内容" {
		t.Fatalf("上下文渲染 = %q，期望 有内容（空文本不贡献内容，也不留分隔符）", got)
	}
}

func TestAssembleKeepsSectionListImmutable(t *testing.T) {
	registry := prompt.NewRegistry()
	if _, err := registry.Section(staticSection("a", 0, "A")); err != nil {
		t.Fatalf("注册失败：%v", err)
	}

	sections := registry.Sections()
	sections[0].Name = "篡改"

	if got := registry.Sections()[0].Name; got != "a" {
		t.Fatalf("外部改动影响了注册表：%q", got)
	}
}

func TestProvideAndFrom(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)

	registry, err := prompt.Provide(ctx)
	if err != nil {
		t.Fatalf("provide 失败：%v", err)
	}
	from, err := prompt.From(ctx)
	if err != nil || from != registry {
		t.Fatalf("From = (%v, %v)，期望同一实例", from, err)
	}

	other := core.NewRoot("other")
	t.Cleanup(other.Dispose)
	if _, err := prompt.From(other); err == nil {
		t.Fatal("未提供注册表时 From 应返回错误")
	}
}

func names(sections []prompt.AssembledSection) []string {
	out := make([]string, 0, len(sections))
	for _, section := range sections {
		out = append(out, section.Name)
	}
	return out
}

func contextNames(contexts []prompt.AssembledContext) []string {
	out := make([]string, 0, len(contexts))
	for _, context := range contexts {
		out = append(out, context.Name)
	}
	return out
}
