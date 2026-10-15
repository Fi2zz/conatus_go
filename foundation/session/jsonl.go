package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Fi2zz/conatus_go/foundation/home"
)

// JSONLPersistence 是本地 JSONL 实现：一会话一文件，每行一条事件的 JSON。
type JSONLPersistence struct {
	dir string
}

// NewJSONLPersistence 创建 JSONL 持久化；dir 为空时用 `<conatus 用户目录>/sessions`。
func NewJSONLPersistence(dir string) *JSONLPersistence {
	if dir == "" {
		dir = defaultSessionDir()
	}
	return &JSONLPersistence{dir: dir}
}

// Dir 返回会话文件所在目录。
func (p *JSONLPersistence) Dir() string { return p.dir }

// List 返回目录下已持久化的会话 id（升序）。
func (p *JSONLPersistence) List() ([]string, error) {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	ids := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, jsonlSuffix) {
			ids = append(ids, strings.TrimSuffix(name, jsonlSuffix))
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// Load 载入某会话的全部事件；不存在时返回空列表。空行与坏行跳过。
func (p *JSONLPersistence) Load(id string) ([]Event, error) {
	content, err := os.ReadFile(p.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	events := []Event{}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := map[string]any{}
		if jsonErr := json.Unmarshal([]byte(line), &fields); jsonErr != nil {
			continue
		}
		events = append(events, EventFromJSON(fields))
	}
	return events, nil
}

// Append 追加一条事件。
//
// 权限顺序是「先建空文件 → 收紧 0600 → 再写」，而不是「先写再收紧」：后者会让首行
// 内容在收紧前短暂以 umask 权限落盘，形成明文窗口。落盘后 Sync，保证 Append 返回
// 即已持久化（比 Dart 的 `flush: true` 更强）。
func (p *JSONLPersistence) Append(id string, event Event) error {
	path := p.path(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := ensureRestricted(path); err != nil {
		return err
	}
	line, err := json.Marshal(event.ToJSON())
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

// Remove 删除某会话的持久化数据；不存在不算错误。
func (p *JSONLPersistence) Remove(id string) error {
	err := os.Remove(p.path(id))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (p *JSONLPersistence) path(id string) string {
	return filepath.Join(p.dir, id+jsonlSuffix)
}

const jsonlSuffix = ".jsonl"

// ensureRestricted 确保文件存在且权限为 0600。
//
// 用「读当前权限位」而不是内存集合判定：收紧失败不会留下「已完成」的假记录，下次
// 追加会重试；旧版本遗留的宽松文件也因此被补收紧。
func ensureRestricted(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm() == 0o600 {
		return nil
	}
	return home.RestrictFilePermissions(path)
}

func defaultSessionDir() string {
	root, err := home.ResolveConatusHome(nil)
	if err != nil {
		return filepath.Join(os.TempDir(), "conatus", "sessions")
	}
	return filepath.Join(root, "sessions")
}
