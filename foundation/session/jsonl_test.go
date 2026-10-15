package session_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Fi2zz/conatus_go/foundation/session"
)

func TestJSONLAppendCreatesFileWithRestrictedPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 没有 POSIX 权限位")
	}
	dir := filepath.Join(t.TempDir(), "sessions")
	store := session.NewStore(session.WithPersistence(session.NewJSONLPersistence(dir)))
	current, _ := store.Create("s1")

	if _, err := current.Append(session.EventUserMessage, session.WithEventData("hi")); err != nil {
		t.Fatalf("追加失败：%v", err)
	}

	path := filepath.Join(dir, "s1.jsonl")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("会话文件应已创建：%v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("会话文件权限 = %o，期望 600（含完整对话与工具输出，可能夹带密钥）", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("会话目录应已创建：%v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("会话目录权限 = %o，期望 700", dirInfo.Mode().Perm())
	}
}

func TestJSONLRepairsLegacyLoosePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 没有 POSIX 权限位")
	}
	dir := filepath.Join(t.TempDir(), "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("建目录失败：%v", err)
	}
	path := filepath.Join(dir, "legacy.jsonl")
	legacy := `{"seq":0,"type":"a","time":"2026-10-15T09:00:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatalf("准备遗留文件失败：%v", err)
	}

	persistence := session.NewJSONLPersistence(dir)
	if err := persistence.Append("legacy", session.NewEvent("legacy", "b", 1)); err != nil {
		t.Fatalf("追加失败：%v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("读取权限失败：%v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("遗留的宽松文件应在下次追加时被补收紧，当前 %o", info.Mode().Perm())
	}
	events, err := persistence.Load("legacy")
	if err != nil || len(events) != 2 {
		t.Fatalf("载入 = (%d 条, %v)，期望 2 条（补收紧不该丢内容）", len(events), err)
	}
}

func TestJSONLRedactsSecretsBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(session.WithPersistence(session.NewJSONLPersistence(dir)))
	current, _ := store.Create("s1")

	if _, err := current.Append("config", session.WithEventData(map[string]any{
		"api_key": "sk-plaintext-abcdefgh",
		"model":   "doubao",
	})); err != nil {
		t.Fatalf("追加失败：%v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "s1.jsonl"))
	if err != nil {
		t.Fatalf("读文件失败：%v", err)
	}
	if strings.Contains(string(content), "sk-plaintext-abcdefgh") {
		t.Fatal("明文密钥落盘了——会话文件会被备份/同步，脱敏必须在写盘前生效")
	}
	if !strings.Contains(string(content), "sk-p...efgh") {
		t.Fatalf("应写入掩码形式，实际内容：%s", content)
	}
	if !strings.Contains(string(content), "doubao") {
		t.Fatal("非敏感字段不该被改动")
	}
}

func TestJSONLLoadSkipsBlankAndBrokenLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	body := strings.Join([]string{
		`{"seq":0,"type":"a","time":"2026-10-15T09:00:00Z"}`,
		``,
		`   `,
		`{这行不是 JSON}`,
		`{"seq":1,"type":"b"}`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("准备文件失败：%v", err)
	}

	events, err := session.NewJSONLPersistence(dir).Load("s1")
	if err != nil {
		t.Fatalf("载入失败：%v", err)
	}
	if len(events) != 2 {
		t.Fatalf("载入 %d 条，期望 2（空行与坏行跳过）", len(events))
	}
	if events[0].Type != "a" || events[1].Type != "b" {
		t.Fatalf("事件 = %+v", events)
	}
	if events[0].Time.IsZero() {
		t.Fatal("时间应被还原")
	}
}

func TestJSONLLoadMissingSessionIsEmpty(t *testing.T) {
	events, err := session.NewJSONLPersistence(t.TempDir()).Load("absent")
	if err != nil || len(events) != 0 {
		t.Fatalf("载入不存在的会话 = (%v, %v)，期望空且无错", events, err)
	}
}

func TestJSONLListSortsAndFiltersNonJSONL(t *testing.T) {
	dir := t.TempDir()
	persistence := session.NewJSONLPersistence(dir)
	for _, id := range []string{"b", "a"} {
		if err := persistence.Append(id, session.NewEvent(id, "x", 0)); err != nil {
			t.Fatalf("追加失败：%v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("准备文件失败：%v", err)
	}

	ids, err := persistence.List()
	if err != nil {
		t.Fatalf("列举失败：%v", err)
	}
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("列举 = %v，期望 [a b]（升序、只认 .jsonl）", ids)
	}
}

func TestJSONLListOnMissingDirectoryIsEmpty(t *testing.T) {
	persistence := session.NewJSONLPersistence(filepath.Join(t.TempDir(), "absent"))

	ids, err := persistence.List()
	if err != nil || len(ids) != 0 {
		t.Fatalf("目录不存在时列举 = (%v, %v)，期望空且无错", ids, err)
	}
}

func TestJSONLRemoveIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	persistence := session.NewJSONLPersistence(dir)
	if err := persistence.Append("s1", session.NewEvent("s1", "x", 0)); err != nil {
		t.Fatalf("追加失败：%v", err)
	}

	if err := persistence.Remove("s1"); err != nil {
		t.Fatalf("删除失败：%v", err)
	}
	if err := persistence.Remove("s1"); err != nil {
		t.Fatalf("重复删除不该报错：%v", err)
	}
	if ids, _ := persistence.List(); len(ids) != 0 {
		t.Fatalf("删除后仍有文件：%v", ids)
	}
}

func TestJSONLDefaultDirFollowsConatusHome(t *testing.T) {
	t.Setenv("CONATUS_HOME", filepath.Join(t.TempDir(), "custom-home"))

	persistence := session.NewJSONLPersistence("")

	if !strings.HasSuffix(persistence.Dir(), filepath.Join("custom-home", "sessions")) {
		t.Fatalf("缺省目录 = %q，期望落在 CONATUS_HOME/sessions 下", persistence.Dir())
	}
}

func TestEventJSONRoundTrip(t *testing.T) {
	moment := time.Date(2026, 10, 15, 9, 30, 0, 0, time.UTC)
	original := session.NewEvent("s1", session.EventToolResult, 3,
		session.WithEventTime(moment),
		session.WithEventData(map[string]any{"ok": true}),
		session.WithEventID("evt-1"),
		session.WithParentEventID("evt-0"))

	restored := session.EventFromJSON(original.ToJSON())

	if restored.Seq != 3 || restored.Type != session.EventToolResult {
		t.Fatalf("还原事件 = %+v", restored)
	}
	if !restored.Time.Equal(moment) {
		t.Fatalf("时间 = %v，期望 %v", restored.Time, moment)
	}
	if restored.ID != "evt-1" || restored.ParentEventID != "evt-0" || restored.SessionID != "s1" {
		t.Fatalf("标识字段未往返：%+v", restored)
	}
	if fields, ok := restored.Data.(map[string]any); !ok || fields["ok"] != true {
		t.Fatalf("负载未往返：%v", restored.Data)
	}
}

func TestEventFromJSONToleratesBrokenFields(t *testing.T) {
	restored := session.EventFromJSON(map[string]any{"type": 42, "time": "不是时间"})

	if restored.Type != "" || !restored.Time.IsZero() {
		t.Fatalf("坏字段应取零值，得到 %+v", restored)
	}
}
