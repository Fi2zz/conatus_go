package core_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
)

// 真实 OS 资源（文件描述符）的可逆性检查。
//
// 合成代理（定时器 / 订阅 / 句柄）只能证明「机制正确」；这里打开的是**真实的文件
// 句柄**，并用 `lsof` 在 OS 层面数它——这是「效果真落地」与「逆真释放」的硬证据，
// 不是代理。
//
// 口径：只数 **FD 名里含本测试临时目录** 的记录，因此与本测试无关的 FD（测试框架、
// 网络等）完全不干扰计数。
func openFdsUnder(prefix string) (int, bool) {
	command := exec.Command("lsof", "-p", fmt.Sprintf("%d", os.Getpid()))
	output, err := command.Output()
	if err != nil {
		return 0, false
	}
	count := 0
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, prefix) {
			count++
		}
	}
	return count, true
}

func TestRealFileDescriptorsAreReleased(t *testing.T) {
	dir := t.TempDir()

	if _, available := openFdsUnder(dir); !available {
		t.Skip("本机没有 lsof，无法做 OS 级 FD 计数")
	}

	root := core.NewRoot("root")
	t.Cleanup(root.Dispose)

	files := make([]*os.File, 0, 3)
	for i := 0; i < 3; i++ {
		path := filepath.Join(dir, fmt.Sprintf("f%d", i))
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("写测试文件失败：%v", err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatalf("打开测试文件失败：%v", err)
		}
		files = append(files, file)
	}

	plugin, err := root.Plugin("io", func(c *core.Context) error {
		for _, file := range files {
			target := file
			c.Track(func() error { return target.Close() })
		}
		return nil
	})
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	opened, _ := openFdsUnder(dir)
	if opened != 3 {
		t.Fatalf("打开 3 个文件句柄后计数 = %d，期望 3", opened)
	}

	plugin.Dispose()

	released, _ := openFdsUnder(dir)
	if released != 0 {
		t.Fatalf("卸载后 FD 计数 = %d，期望 0（句柄真释放）", released)
	}
}
