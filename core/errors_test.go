package core_test

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
)

// SetErrorHandler(nil) 应恢复默认处理器：把错误写进 stderr，而不是继续走自定义接收器。
func TestSetErrorHandlerNilRestoresStderr(t *testing.T) {
	root := core.NewRoot("root")
	var reported []error
	root.SetErrorHandler(func(err error) { reported = append(reported, err) })
	root.SetErrorHandler(nil)

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败：%v", err)
	}
	original := os.Stderr
	os.Stderr = writer
	t.Cleanup(func() {
		os.Stderr = original
		writer.Close()
		reader.Close()
	})

	root.Track(func() error { return errors.New("boom") })
	root.Dispose()
	writer.Close()

	content, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("读取 stderr 失败：%v", readErr)
	}
	if !strings.Contains(string(content), "boom") {
		t.Fatalf("默认处理器应把错误写进 stderr，实际得到 %q", string(content))
	}
	if len(reported) != 0 {
		t.Fatalf("恢复默认处理器后不应再走自定义接收器，得到 %d 条", len(reported))
	}
}
