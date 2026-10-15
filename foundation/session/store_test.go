package session_test

import (
	"errors"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
	"github.com/Fi2zz/conatus_go/foundation/session"
)

func TestStoreCreateGetIDsAndDuplicate(t *testing.T) {
	store := session.NewStore()

	current, err := store.Create("a")
	if err != nil {
		t.Fatalf("创建失败：%v", err)
	}
	if found, ok := store.Get("a"); !ok || found != current {
		t.Fatal("应按 id 取回同一个会话")
	}
	if ids := store.IDs(); len(ids) != 1 || ids[0] != "a" {
		t.Fatalf("活跃会话 = %v，期望 [a]", ids)
	}
	if _, err := store.Create("a"); err == nil {
		t.Fatal("重复 id 应返回错误")
	}
	if store.Len() != 1 {
		t.Fatalf("活跃会话数 = %d，期望 1", store.Len())
	}
}

func TestStoreCreateGeneratesUUIDBasedID(t *testing.T) {
	store := session.NewStore()
	pattern := regexp.MustCompile(
		`^session_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

	first, err := store.Create("")
	if err != nil {
		t.Fatalf("创建失败：%v", err)
	}
	second, err := store.Create("")
	if err != nil {
		t.Fatalf("创建失败：%v", err)
	}

	if !pattern.MatchString(first.ID()) {
		t.Fatalf("缺省 id = %q，期望 session_<uuid>", first.ID())
	}
	if first.ID() == second.ID() {
		t.Fatal("两次生成的 id 不该相同")
	}
}

func TestStoreCloseClosesSessionAndRemovesIt(t *testing.T) {
	store := session.NewStore()
	current, _ := store.Create("a")

	if !store.Close("a") {
		t.Fatal("关闭已存在的会话应返回 true")
	}
	if !current.Closed() {
		t.Fatal("关闭会话后 Closed() 应为 true")
	}
	if _, found := store.Get("a"); found {
		t.Fatal("关闭后不该还在活跃集里")
	}
	if store.Close("a") {
		t.Fatal("重复关闭应返回 false")
	}
}

func TestStoreAdoptRegistersExternalSession(t *testing.T) {
	store := session.NewStore()
	source, _ := store.Create("a")
	source.Append(session.EventUserMessage, session.WithEventData(map[string]any{"text": "hi"}))

	fork, err := source.Fork(session.WithForkID("session_forked-1"))
	if err != nil {
		t.Fatalf("fork 失败：%v", err)
	}
	adopted, err := store.Adopt(fork)
	if err != nil {
		t.Fatalf("收养失败：%v", err)
	}

	if adopted != fork {
		t.Fatal("收养应返回同一个会话")
	}
	if found, ok := store.Get("session_forked-1"); !ok || found != fork {
		t.Fatal("收养后应能在活跃集里找到")
	}
	if fork.Len() != 1 {
		t.Fatalf("fork 事件数 = %d，期望 1（继承来源事件）", fork.Len())
	}
	if _, err := store.Adopt(fork); err == nil {
		t.Fatal("重复收养应返回错误")
	}
}

func TestStoreAdoptPersistsForkSeedAndReopens(t *testing.T) {
	dir := t.TempDir()
	persistence := session.NewJSONLPersistence(dir)
	store := session.NewStore(session.WithPersistence(persistence))

	source, _ := store.Create("s1")
	source.Append(session.EventUserMessage, session.WithEventData(map[string]any{"text": "第一轮"}))
	source.Append(session.EventAssistantMessage, session.WithEventData(map[string]any{"text": "回答"}))
	lastID, _ := source.LastEventID()

	fork, err := source.Fork(session.WithForkPoint(lastID), session.WithForkID("forked"))
	if err != nil {
		t.Fatalf("fork 失败：%v", err)
	}
	if _, err := store.Adopt(fork); err != nil {
		t.Fatalf("收养失败：%v", err)
	}
	if _, err := fork.Append(session.EventUserMessage, session.WithEventData(map[string]any{"text": "追问"})); err != nil {
		t.Fatalf("追加失败：%v", err)
	}

	reopened := session.NewStore(session.WithPersistence(persistence))
	loaded, err := reopened.Open("forked")
	if err != nil {
		t.Fatalf("重开失败：%v", err)
	}

	if loaded.Len() != 3 {
		t.Fatalf("重开事件数 = %d，期望 3（继承 2 + 新追加 1）", loaded.Len())
	}
	if loaded.InheritedEventCount() != 0 {
		t.Fatal("重新打开是普通加载，不是 fork，继承条数应为 0")
	}
	events := loaded.Events()
	if events[0].Type != session.EventUserMessage || events[2].Type != session.EventUserMessage {
		t.Fatalf("事件顺序 = %v", typesOf(events))
	}
}

func TestStoreAppendPersistsAndReopens(t *testing.T) {
	dir := t.TempDir()
	persistence := session.NewJSONLPersistence(dir)
	store := session.NewStore(session.WithPersistence(persistence))

	current, _ := store.Create("s1")
	if _, err := current.Append(session.EventUserMessage, session.WithEventData(map[string]any{"text": "你好"})); err != nil {
		t.Fatalf("追加失败：%v", err)
	}

	reopened := session.NewStore(session.WithPersistence(persistence))
	loaded, err := reopened.Open("s1")
	if err != nil {
		t.Fatalf("重开失败：%v", err)
	}

	if loaded.Len() != 1 {
		t.Fatalf("重开事件数 = %d，期望 1（Append 返回即已落盘）", loaded.Len())
	}
	fields, ok := loaded.Events()[0].Data.(map[string]any)
	if !ok || fields["text"] != "你好" {
		t.Fatalf("负载未往返：%v", loaded.Events()[0].Data)
	}
	ids, err := reopened.PersistedIDs()
	if err != nil || len(ids) != 1 || ids[0] != "s1" {
		t.Fatalf("已持久化 id = (%v, %v)，期望 [s1]", ids, err)
	}
}

func TestStoreBurstAppendsKeepOrder(t *testing.T) {
	persistence := session.NewJSONLPersistence(t.TempDir())
	store := session.NewStore(session.WithPersistence(persistence))

	current, _ := store.Create("burst")
	current.Append(session.EventUserMessage, session.WithEventData(map[string]any{"text": "说普通话"}))
	current.Append(session.EventAssistantMessage, session.WithEventData(map[string]any{"text": "好的"}))

	reopened := session.NewStore(session.WithPersistence(persistence))
	loaded, err := reopened.Open("burst")
	if err != nil {
		t.Fatalf("重开失败：%v", err)
	}
	if got := typesOf(loaded.Events()); len(got) != 2 ||
		got[0] != session.EventUserMessage || got[1] != session.EventAssistantMessage {
		t.Fatalf("事件顺序 = %v", got)
	}
}

func TestStoreOpenFreshThenPersists(t *testing.T) {
	persistence := session.NewJSONLPersistence(t.TempDir())
	store := session.NewStore(session.WithPersistence(persistence))

	current, err := store.Open("fresh")
	if err != nil {
		t.Fatalf("打开失败：%v", err)
	}
	if current.Len() != 0 {
		t.Fatalf("未持久化的会话应为空，得到 %d 条", current.Len())
	}

	if _, err := current.Append("a"); err != nil {
		t.Fatalf("追加失败：%v", err)
	}

	reopened := session.NewStore(session.WithPersistence(persistence))
	loaded, err := reopened.Open("fresh")
	if err != nil {
		t.Fatalf("重开失败：%v", err)
	}
	if loaded.Len() != 1 {
		t.Fatalf("重开事件数 = %d，期望 1", loaded.Len())
	}
}

func TestStoreOpenReturnsActiveSession(t *testing.T) {
	store := session.NewStore()
	created, _ := store.Create("a")

	opened, err := store.Open("a")
	if err != nil || opened != created {
		t.Fatalf("已活跃的会话应直接返回：(%v, %v)", opened, err)
	}
}

func TestStoreRemoveClosesAndDeletes(t *testing.T) {
	persistence := session.NewJSONLPersistence(t.TempDir())
	store := session.NewStore(session.WithPersistence(persistence))
	current, _ := store.Create("s1")
	current.Append("a")

	removed, err := store.Remove("s1")
	if err != nil || !removed {
		t.Fatalf("删除 = (%v, %v)，期望 true", removed, err)
	}
	if _, found := store.Get("s1"); found {
		t.Fatal("删除后不该还在活跃集里")
	}
	ids, err := persistence.List()
	if err != nil || len(ids) != 0 {
		t.Fatalf("持久化里应为空：(%v, %v)", ids, err)
	}
}

func TestStoreReportsPersistenceFailuresToHandler(t *testing.T) {
	boom := errors.New("磁盘满了")
	reported := []error{}
	store := session.NewStore(
		session.WithPersistence(failingPersistence{err: boom}),
		session.WithStoreErrorHandler(func(err error) { reported = append(reported, err) }),
	)
	current, _ := store.Create("s1")

	if _, err := current.Append("a"); err != nil {
		t.Fatalf("落盘失败不该让 Append 失败（错误走接收器）：%v", err)
	}
	if len(reported) != 1 || !errors.Is(reported[0], boom) {
		t.Fatalf("接收到的错误 = %v，期望 1 条 boom", reported)
	}
}

func TestProvideSessionsReusesProvidedPersistence(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	persistence := session.NewJSONLPersistence(filepath.Join(t.TempDir(), "unused"))

	if _, err := session.ProvidePersistence(ctx, persistence); err != nil {
		t.Fatalf("提供持久化失败：%v", err)
	}
	store, err := session.ProvideSessions(ctx)
	if err != nil {
		t.Fatalf("提供会话仓库失败：%v", err)
	}

	provided, found := core.Get[*session.Store](ctx, session.StoreKey)
	if !found || provided != store {
		t.Fatal("服务应指向同一个仓库")
	}
	got, found := core.Get[session.Persistence](ctx, session.PersistenceKey)
	if !found || got != session.Persistence(persistence) {
		t.Fatal("仓库应复用上下文里已提供的持久化")
	}
}

func TestProvidePersistenceDefaultsToJSONL(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)

	persistence, err := session.ProvidePersistence(ctx, nil)
	if err != nil {
		t.Fatalf("提供持久化失败：%v", err)
	}
	if _, ok := persistence.(*session.JSONLPersistence); !ok {
		t.Fatalf("缺省实现 = %T，期望 *session.JSONLPersistence", persistence)
	}
}

func typesOf(events []session.Event) []string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

type failingPersistence struct{ err error }

func (f failingPersistence) List() ([]string, error)              { return nil, f.err }
func (f failingPersistence) Load(string) ([]session.Event, error) { return nil, f.err }
func (f failingPersistence) Append(string, session.Event) error   { return f.err }
func (f failingPersistence) Remove(string) error                  { return f.err }
