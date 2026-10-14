package core_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
)

// 可逆性验证的观测等价边界：oracle 的分辨率由「观察什么」决定。
//
// 恢复是 up to **observational equivalence** 的，而等价关系取决于观察口径。本仓的
// 投影只比较键名、存活资源数与字符串，**不比对象身份、不比监听器顺序**。因此逆若
// 还原成一个「内容等价但不是同一个对象」的状态，oracle 会判为通过。
//
// 这是**设计上的宽松点，不是 bug**——但如果你关心对象身份（比如某个单例），就
// 必须把它加进投影。本文件把这个边界钉成文档：一旦有人把投影改得更细（开始比较
// 身份），下面的测试会失败，迫使那个决定被显式做出。
func freshBox() []int { return []int{1, 2, 3} }

func TestObservationalEquivalenceAcceptsFreshEquivalentObject(t *testing.T) {
	root := core.NewRoot("root")
	t.Cleanup(root.Dispose)

	original := freshBox()
	undo := mustProvide(t, root, "svc", original)
	project := func() []string {
		value, _ := root.Service("svc")
		return []string{fmt.Sprintf("svc: %v", value)}
	}

	before := project()
	if err := undo(); err != nil {
		t.Fatalf("撤销失败：%v", err)
	}
	mustProvide(t, root, "svc", freshBox())
	after := project()

	if !reflect.DeepEqual(after, before) {
		t.Fatalf("内容等价时 oracle 应判为还原成功：%v → %v", before, after)
	}

	current, _ := root.Service("svc")
	typed, ok := current.([]int)
	if !ok || len(typed) == 0 {
		t.Fatalf("服务值形态异常：%T", current)
	}
	if &typed[0] == &original[0] {
		t.Fatal("对象身份其实已经变了，但 oracle 看不出来——这正是本文件要钉住的边界")
	}
}

func TestObservationalEquivalenceCatchesDifferentValues(t *testing.T) {
	root := core.NewRoot("root")
	t.Cleanup(root.Dispose)

	undo := mustProvide(t, root, "svc", []int{1, 2, 3})
	project := func() []string {
		value, _ := root.Service("svc")
		return []string{fmt.Sprintf("svc: %v", value)}
	}

	before := project()
	if err := undo(); err != nil {
		t.Fatalf("撤销失败：%v", err)
	}
	mustProvide(t, root, "svc", []int{9, 9, 9})
	after := project()

	if reflect.DeepEqual(after, before) {
		t.Fatal("值真的不同时 oracle 必须抓住（本组探针非空转）")
	}
}
