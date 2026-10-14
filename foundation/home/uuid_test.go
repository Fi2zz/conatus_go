package home_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Fi2zz/conatus_go/foundation/home"
)

var uuidPattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func TestNewUUIDv4HasExpectedShape(t *testing.T) {
	if got := home.NewUUIDv4(); !uuidPattern.MatchString(got) {
		t.Fatalf("UUID 形态 = %q，期望 8-4-4-4-12 小写十六进制", got)
	}
}

func TestNewUUIDv4SetsVersionAndVariantBits(t *testing.T) {
	for i := 0; i < 20; i++ {
		id := home.NewUUIDv4()
		if id[14] != '4' {
			t.Fatalf("version 位应为 4，得到 %q", id)
		}
		if !strings.ContainsRune("89ab", rune(id[19])) {
			t.Fatalf("variant 位应为 8/9/a/b，得到 %q", id)
		}
	}
}

func TestNewUUIDv4IsUnique(t *testing.T) {
	if home.NewUUIDv4() == home.NewUUIDv4() {
		t.Fatal("两次生成不应相同")
	}
}
