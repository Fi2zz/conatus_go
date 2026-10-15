package session_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Fi2zz/conatus_go/foundation/session"
)

func TestAppendAssignsSeqFromZeroAndCarriesData(t *testing.T) {
	current, err := session.New("s1")
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}

	first, err := current.Append(session.EventUserMessage, session.WithEventData("hi"))
	if err != nil {
		t.Fatalf("追加失败：%v", err)
	}
	second, err := current.Append(session.EventAssistantMessage)
	if err != nil {
		t.Fatalf("追加失败：%v", err)
	}

	if first.Seq != 0 || second.Seq != 1 {
		t.Fatalf("seq = (%d, %d)，期望 (0, 1)", first.Seq, second.Seq)
	}
	if first.Type != session.EventUserMessage || first.Data != "hi" {
		t.Fatalf("首条事件 = %+v", first)
	}
	if current.Len() != 2 {
		t.Fatalf("事件数 = %d，期望 2", current.Len())
	}
	if last := current.Events()[1]; last.Seq != second.Seq || last.ID != second.ID {
		t.Fatalf("末条事件 = %+v，期望 %+v", last, second)
	}
	if !current.CreatedAt().After(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("创建时间异常：%v", current.CreatedAt())
	}
	if lastID, found := current.LastEventID(); !found || lastID != second.ID {
		t.Fatalf("LastEventID = (%q, %v)", lastID, found)
	}
}

func TestOnEventNotifiesUntilRevoked(t *testing.T) {
	current, _ := session.New("s1")
	seen := []int{}
	off := current.OnEvent(func(event session.Event) { seen = append(seen, event.Seq) })

	if _, err := current.Append("a"); err != nil {
		t.Fatalf("追加失败：%v", err)
	}
	if err := off(); err != nil {
		t.Fatalf("撤销监听失败：%v", err)
	}
	if _, err := current.Append("b"); err != nil {
		t.Fatalf("追加失败：%v", err)
	}

	if len(seen) != 1 || seen[0] != 0 {
		t.Fatalf("收到的事件 = %v，期望 [0]", seen)
	}
}

func TestCloseRejectsAppendAndNotifiesOnce(t *testing.T) {
	current, _ := session.New("s1")
	closed := 0
	current.OnClose(func() { closed++ })

	current.Close()
	current.Close()

	if !current.Closed() {
		t.Fatal("关闭后 Closed() 应为 true")
	}
	if closed != 1 {
		t.Fatalf("关闭回调次数 = %d，期望 1（幂等）", closed)
	}
	if _, err := current.Append("a"); err == nil {
		t.Fatal("关闭后追加应返回错误")
	}
}

// TestCloseNotifiesOnFirstCall 是「只关一次」的用例：Dart 侧的对应用例连关两次，
// 会被「首次关闭不通知、第二次才通知」的实现骗过去。
func TestCloseNotifiesOnFirstCall(t *testing.T) {
	current, _ := session.New("s1")
	closed := 0
	current.OnClose(func() { closed++ })

	current.Close()

	if closed != 1 {
		t.Fatalf("首次关闭就该通知监听器，得到 %d 次", closed)
	}
	if !current.Closed() {
		t.Fatal("关闭后 Closed() 应为 true")
	}
}

func TestOnCloseOnClosedSessionFiresImmediately(t *testing.T) {
	current, _ := session.New("s1")
	current.Close()

	closed := 0
	current.OnClose(func() { closed++ })

	if closed != 1 {
		t.Fatalf("已关闭时登记关闭监听应立即回调，得到 %d 次", closed)
	}
}

func TestSeedContinuesSeqAndCreationTime(t *testing.T) {
	moment := time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC)
	current, err := session.New("s1", session.WithSeed([]session.Event{
		{Seq: 0, Type: "a", Time: moment},
		{Seq: 1, Type: "b", Time: moment},
	}))
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}

	if current.Len() != 2 {
		t.Fatalf("事件数 = %d，期望 2", current.Len())
	}
	if !current.CreatedAt().Equal(moment) {
		t.Fatalf("创建时间 = %v，期望 %v", current.CreatedAt(), moment)
	}
	appended, err := current.Append("c")
	if err != nil {
		t.Fatalf("追加失败：%v", err)
	}
	if appended.Seq != 2 {
		t.Fatalf("续接 seq = %d，期望 2", appended.Seq)
	}
}

func TestSeedRejectsInvalidInheritedCount(t *testing.T) {
	_, err := session.New("s1",
		session.WithSeed([]session.Event{{Seq: 0, Type: "a"}}),
		session.WithInheritedEventCount(2))

	if err == nil {
		t.Fatal("继承条数超出种子范围应返回错误")
	}
}

func TestOwnEventsSkipsInheritedPrefix(t *testing.T) {
	source, _ := session.New("s1")
	first, _ := source.Append("a")
	second, _ := source.Append("b")

	fork, err := source.Fork(session.WithForkPoint(second.ID), session.WithForkID("f1"))
	if err != nil {
		t.Fatalf("fork 失败：%v", err)
	}
	if fork.ID() != "f1" || fork.Len() != 2 || fork.InheritedEventCount() != 2 {
		t.Fatalf("派生会话 = (id %q, len %d, inherited %d)", fork.ID(), fork.Len(), fork.InheritedEventCount())
	}
	if len(fork.OwnEvents()) != 0 {
		t.Fatalf("派生会话自身事件应为空，得到 %d 条", len(fork.OwnEvents()))
	}

	if _, err := fork.Append("c"); err != nil {
		t.Fatalf("追加失败：%v", err)
	}
	if len(fork.OwnEvents()) != 1 || len(source.OwnEvents()) != 2 || source.Len() != 2 {
		t.Fatal("派生会话应只拥有新追加的后缀，且不影响来源")
	}
	if first.ID == second.ID {
		t.Fatal("事件 id 应各不相同")
	}
}

func TestForkWithoutPointSeedsEverything(t *testing.T) {
	source, _ := session.New("s1")
	source.Append("a")
	source.Append("b")

	fork, err := source.Fork()
	if err != nil {
		t.Fatalf("fork 失败：%v", err)
	}

	if fork.Len() != 2 {
		t.Fatalf("全量 fork 的事件数 = %d，期望 2", fork.Len())
	}
	if !strings.HasPrefix(fork.ID(), "s1-fork-") {
		t.Fatalf("缺省派生 id = %q，期望 s1-fork-*", fork.ID())
	}
}

func TestForkRejectsUnknownEvent(t *testing.T) {
	source, _ := session.New("s1")
	source.Append("a")

	if _, err := source.Fork(session.WithForkPoint("evt-missing")); err == nil {
		t.Fatal("找不到事件时应返回错误")
	}
}

func TestReadFiltersByTime(t *testing.T) {
	base := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	current, _ := session.New("s1")
	current.Append("a", session.WithEventTime(base))
	current.Append("b", session.WithEventTime(base.Add(time.Minute)))

	if got := current.Read(time.Time{}, time.Time{}); len(got) != 2 {
		t.Fatalf("无边界读取 = %d 条，期望 2", len(got))
	}
	if got := current.Read(base.Add(time.Second), time.Time{}); len(got) != 1 || got[0].Type != "b" {
		t.Fatalf("按起点过滤 = %+v", got)
	}
	if got := current.Read(time.Time{}, base); len(got) != 1 || got[0].Type != "a" {
		t.Fatalf("按终点过滤 = %+v", got)
	}
}

func TestReplayVisitsEventsInOrder(t *testing.T) {
	current, _ := session.New("s1")
	current.Append("a")
	current.Append("b")
	seen := []string{}

	current.Replay(func(event session.Event) { seen = append(seen, event.Type) })

	if len(seen) != 2 || seen[0] != "a" || seen[1] != "b" {
		t.Fatalf("重放顺序 = %v，期望 [a b]", seen)
	}
}

func TestAppendEventStampsSessionAndSeq(t *testing.T) {
	current, _ := session.New("s1")

	stamped, err := current.AppendEvent(session.Event{Type: "external", SessionID: "other", ID: "evt-keep"})
	if err != nil {
		t.Fatalf("追加失败：%v", err)
	}

	if stamped.SessionID != "s1" || stamped.Seq != 0 {
		t.Fatalf("回填事件 = %+v，期望被盖成本会话的 id 与 seq", stamped)
	}
	if stamped.ID != "evt-keep" {
		t.Fatalf("已有 id 应保留，得到 %q", stamped.ID)
	}
	generated, _ := current.AppendEvent(session.Event{Type: "external"})
	if generated.ID == "" || generated.Seq != 1 {
		t.Fatalf("缺省 id 应自动生成，得到 %+v", generated)
	}
}

// TestListenerCanReadSessionWhileAppending 钉住「读路径不加锁」这条设计：监听器在
// 追加锁内被调用，此前的帧里它必须能安全地读会话（model-visible 不变式检查的形状）。
func TestListenerCanReadSessionWhileAppending(t *testing.T) {
	current, _ := session.New("s1")
	lengths := []int{}
	current.OnEvent(func(event session.Event) {
		lengths = append(lengths, current.Len())
		if _, found := current.LastEventID(); !found {
			t.Error("追加过程中应能读到事件")
		}
	})

	current.Append("a")
	current.Append("b")

	if len(lengths) != 2 || lengths[0] != 1 || lengths[1] != 2 {
		t.Fatalf("监听器读到的事件数 = %v，期望 [1 2]", lengths)
	}
}

func TestConcurrentAppendsKeepSeqOrder(t *testing.T) {
	current, _ := session.New("s1")
	var group sync.WaitGroup
	for index := 0; index < 8; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			if _, err := current.Append("concurrent", session.WithEventData(index)); err != nil {
				t.Errorf("并发追加失败：%v", err)
			}
		}(index)
	}
	group.Wait()

	if current.Len() != 8 {
		t.Fatalf("事件数 = %d，期望 8", current.Len())
	}
	seen := map[int]bool{}
	for index, event := range current.Events() {
		if event.Seq != index {
			t.Fatalf("第 %d 条事件的 seq = %d，期望 %d", index, event.Seq, index)
		}
		seen[event.Seq] = true
	}
	for index := 0; index < 8; index++ {
		if !seen[index] {
			t.Fatalf("seq %d 缺失", index)
		}
	}
}
