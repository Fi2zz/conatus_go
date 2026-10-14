package logging_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Fi2zz/conatus_go/foundation/logging"
)

type collector struct{ records []logging.Record }

func (c *collector) Export(record logging.Record) { c.records = append(c.records, record) }

func messages(records []logging.Record) []string {
	out := make([]string, 0, len(records))
	for _, record := range records {
		out = append(out, record.Message.(string))
	}
	return out
}

func TestServiceFiltersBelowMinLevel(t *testing.T) {
	service := logging.NewService(logging.WithMinLevel(logging.LevelWarn))
	sink := &collector{}
	service.AddExporter(sink)

	service.Debug("d")
	service.Info("i")
	service.Warn("w")
	service.Error("e")

	got := messages(sink.records)
	if len(got) != 2 || got[0] != "w" || got[1] != "e" {
		t.Fatalf("导出记录 = %v，期望 [w e]", got)
	}
}

func TestNamedLoggerRecordsNameErrorAndStack(t *testing.T) {
	service := logging.NewService()
	sink := &collector{}
	service.AddExporter(sink)
	stack := []byte("goroutine 1 [running]:")
	boom := errors.New("boom")

	service.Logger("chat").Warn("超时", logging.WithError(boom), logging.WithStack(stack))

	if len(sink.records) != 1 {
		t.Fatalf("应记录 1 条，得到 %d 条", len(sink.records))
	}
	record := sink.records[0]
	if record.Level != logging.LevelWarn {
		t.Fatalf("级别 = %v，期望 warn", record.Level)
	}
	if record.Name != "chat" {
		t.Fatalf("名字 = %q，期望 chat", record.Name)
	}
	if record.Message != "超时" {
		t.Fatalf("消息 = %v，期望 超时", record.Message)
	}
	if !errors.Is(record.Err, boom) {
		t.Fatalf("错误 = %v，期望 boom", record.Err)
	}
	if string(record.Stack) != string(stack) {
		t.Fatalf("堆栈 = %q，期望 %q", record.Stack, stack)
	}
}

func TestRemoveExporterIsIdempotent(t *testing.T) {
	service := logging.NewService()
	sink := &collector{}

	service.AddExporter(sink)
	if len(service.Exporters()) != 1 {
		t.Fatalf("导出器数 = %d，期望 1", len(service.Exporters()))
	}
	if !service.RemoveExporter(sink) {
		t.Fatal("首次移除应返回 true")
	}
	if service.RemoveExporter(sink) {
		t.Fatal("重复移除应返回 false")
	}
	if len(service.Exporters()) != 0 {
		t.Fatalf("导出器数 = %d，期望 0", len(service.Exporters()))
	}
}

func TestRecentBufferHonoursLimit(t *testing.T) {
	service := logging.NewService(logging.WithRecentLimit(2))

	service.Info("1")
	service.Info("2")
	service.Info("3")

	got := messages(service.Recent())
	if len(got) != 2 || got[0] != "2" || got[1] != "3" {
		t.Fatalf("recent = %v，期望 [2 3]", got)
	}
}

func TestInjectedClockStampsRecords(t *testing.T) {
	fixed := time.Date(2026, 10, 14, 9, 30, 0, 0, time.UTC)
	service := logging.NewService(logging.WithClock(func() time.Time { return fixed }))
	sink := &collector{}
	service.AddExporter(sink)

	service.Info("hi")

	if !sink.records[0].Time.Equal(fixed) {
		t.Fatalf("时间 = %v，期望注入的 %v（时间敏感件必须走可注入时钟）", sink.records[0].Time, fixed)
	}
}

func TestDefaultNameComesFromService(t *testing.T) {
	service := logging.NewService(logging.WithDefaultName("app"))
	sink := &collector{}
	service.AddExporter(sink)

	service.Info("hi")

	if sink.records[0].Name != "app" {
		t.Fatalf("名字 = %q，期望 app", sink.records[0].Name)
	}
	if service.Logger("").Name() != "app" {
		t.Fatalf("空名字应回退到 defaultName，得到 %q", service.Logger("").Name())
	}
}
