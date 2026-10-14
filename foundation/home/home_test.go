package home_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Fi2zz/conatus_go/foundation/home"
)

func TestResolveConatusHomePrefersOverride(t *testing.T) {
	got, err := home.ResolveConatusHome(home.MapEnv(map[string]string{
		home.HomeEnv: "/data/conatus",
		"HOME":       "/home/u",
	}))

	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if got != "/data/conatus" {
		t.Fatalf("结果 = %q，期望 /data/conatus", got)
	}
}

func TestResolveConatusHomeTreatsBlankOverrideAsUnset(t *testing.T) {
	got, err := home.ResolveConatusHome(home.MapEnv(map[string]string{
		home.HomeEnv: "   ",
		"HOME":       "/home/u",
	}))

	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if want := filepath.Join("/home/u", home.HomeDirName); got != want {
		t.Fatalf("结果 = %q，期望 %q", got, want)
	}
}

func TestResolveHomeDirPrefersHOME(t *testing.T) {
	got, err := home.ResolveHomeDir(home.MapEnv(map[string]string{
		"HOME":        "/home/u",
		"USERPROFILE": `C:\Users\u`,
	}))

	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if got != "/home/u" {
		t.Fatalf("结果 = %q，期望 /home/u", got)
	}
}

func TestResolveHomeDirFallsBackToUserProfile(t *testing.T) {
	got, err := home.ResolveHomeDir(home.MapEnv(map[string]string{"USERPROFILE": `C:\Users\u`}))

	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if got != `C:\Users\u` {
		t.Fatalf("结果 = %q，期望 C:\\Users\\u", got)
	}
}

func TestResolveHomeDirNeverFallsBackToWorkingDirectory(t *testing.T) {
	_, err := home.ResolveHomeDir(home.MapEnv(map[string]string{}))
	if err == nil {
		t.Fatal("用户目录缺失时应该失败，绝不回退 cwd")
	}
	if !strings.Contains(err.Error(), "无法定位用户目录") {
		t.Fatalf("错误信息应说明无法定位用户目录，得到 %v", err)
	}

	if _, err := home.ResolveConatusHome(home.MapEnv(map[string]string{})); err == nil {
		t.Fatal("用户目录缺失时 conatus 数据目录也不该解析成功")
	}
}

func TestNilEnvReadsProcessEnvironment(t *testing.T) {
	t.Setenv("HOME", filepath.Join(t.TempDir(), "process-home"))

	got, err := home.ResolveHomeDir(nil)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if !strings.HasSuffix(got, "process-home") {
		t.Fatalf("结果 = %q，期望读到进程环境里的 HOME", got)
	}
}
