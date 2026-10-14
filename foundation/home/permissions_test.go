package home_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Fi2zz/conatus_go/foundation/home"
)

func TestRestrictFilePermissionsTightensTo0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 没有 POSIX 权限位")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatalf("准备文件失败：%v", err)
	}

	if err := home.RestrictFilePermissions(path); err != nil {
		t.Fatalf("收紧权限失败：%v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("读取权限失败：%v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("权限 = %o，期望 600", info.Mode().Perm())
	}
}

func TestRestrictDirectoryPermissionsTightensTo0700(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 没有 POSIX 权限位")
	}
	dir := filepath.Join(t.TempDir(), "checkpoints")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("准备目录失败：%v", err)
	}

	if err := home.RestrictDirectoryPermissions(dir); err != nil {
		t.Fatalf("收紧权限失败：%v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("读取权限失败：%v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("权限 = %o，期望 700", info.Mode().Perm())
	}
}

func TestRestrictFilePermissionsFailsOnMissingPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 没有 POSIX 权限位")
	}
	err := home.RestrictFilePermissions(filepath.Join(t.TempDir(), "absent"))

	if err == nil {
		t.Fatal("收紧失败必须返回错误，静默通过会把安全承诺变成空话")
	}
}
