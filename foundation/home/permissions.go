package home

import (
	"fmt"
	"os"
	"runtime"
)

// RestrictFilePermissions 把 path 指向的文件权限收紧为 0600（属主读写，组/其他无权限）。
//
// 为什么需要：会话 JSONL、检查点归档等都是**逐行落盘的完整对话与工作区内容**——
// 用户可能在里面粘贴过密钥、token、私有代码片段，而默认权限随 umask（通常
// 0644/0755），同机其他用户能直接读。收紧失败必须返回错误：权限没收紧成功就不该
// 被当作已收紧，静默失败会把安全承诺变成空话。
func RestrictFilePermissions(path string) error {
	return restrict(path, 0o600, "0600")
}

// RestrictDirectoryPermissions 把 path 指向的目录权限收紧为 0700（属主可读写执行）。
//
// 用途：检查点这类「按会话分目录承载敏感内容」的落盘面，收紧外层目录一次即可挡住
// 其他用户遍历进去；文件自身的 0600 作为兜底。
func RestrictDirectoryPermissions(path string) error {
	return restrict(path, 0o700, "0700")
}

func restrict(path string, mode os.FileMode, label string) error {
	if runtime.GOOS == "windows" {
		return nil // Windows 没有 POSIX 权限位
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod %s 失败：%w", label, err)
	}
	return nil
}
