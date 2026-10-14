// Package home 是本机环境基础件：用户级数据目录解析、唯一标识生成、落盘权限收紧。
//
// 三件事都属于「本机环境契约」，被 session 持久化、检查点归档等落盘方共享；
// 放在最底层，不依赖 foundation 的其它子包。
package home

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// HomeEnv 是覆盖 conatus 用户级数据根目录的环境变量名。
const HomeEnv = "CONATUS_HOME"

// HomeDirName 是 conatus 用户级数据根目录名（位于用户目录下）。
const HomeDirName = ".conatus"

// Env 是环境变量查询函数；nil 表示读进程环境。
type Env func(key string) string

// ProcessEnv 读进程环境。
func ProcessEnv(key string) string { return os.Getenv(key) }

// MapEnv 把一张表适配成 [Env]（测试用）。
func MapEnv(values map[string]string) Env {
	return func(key string) string { return values[key] }
}

// ResolveHomeDir 解析用户目录：`HOME` → Windows `USERPROFILE` → 都没有返回错误。
//
// 绝不在当前工作目录兜底：解析不到就快速失败，由调用方决定如何呈现。
func ResolveHomeDir(env Env) (string, error) {
	source := envOrProcess(env)
	if home := strings.TrimSpace(source("HOME")); home != "" {
		return home, nil
	}
	if profile := strings.TrimSpace(source("USERPROFILE")); profile != "" {
		return profile, nil
	}
	return "", errors.New("无法定位用户目录：请设置 HOME 或 " + HomeEnv + " 环境变量")
}

// ResolveConatusHome 解析 conatus 用户级数据根目录：
// `CONATUS_HOME` 非空优先，否则 `<用户目录>/.conatus`。
func ResolveConatusHome(env Env) (string, error) {
	source := envOrProcess(env)
	if overridden := strings.TrimSpace(source(HomeEnv)); overridden != "" {
		return overridden, nil
	}
	userHome, err := ResolveHomeDir(source)
	if err != nil {
		return "", err
	}
	return filepath.Join(userHome, HomeDirName), nil
}

func envOrProcess(env Env) Env {
	if env == nil {
		return ProcessEnv
	}
	return env
}
