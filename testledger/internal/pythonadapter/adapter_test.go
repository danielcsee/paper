package pythonadapter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/danielcsee/sciterm/testledger/internal/config"
)

func TestDiscoverIgnoresFormattingAndExcludesNestedLines(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample.py")
	first := "def outer(value):\n    # comment\n    def inner():\n        return 1\n    return value + 1\n"
	if err := os.WriteFile(path, []byte(first), 0o600); err != nil {
		t.Fatal(err)
	}
	lang := config.Language{Name: "python", Python: "python3", Include: []string{"**/*.py"}}
	adapter := Adapter{}
	before, err := adapter.Discover(context.Background(), root, lang)
	if err != nil {
		t.Fatal(err)
	}
	second := "def outer( value ):\n\n    def inner():\n        return 1\n    return value + 1  # changed comment\n"
	if err := os.WriteFile(path, []byte(second), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := adapter.Discover(context.Background(), root, lang)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 2 || len(after) != 2 {
		t.Fatalf("unexpected symbols: %d and %d", len(before), len(after))
	}
	if before[0].SemanticHash != after[0].SemanticHash || before[1].SemanticHash != after[1].SemanticHash {
		t.Fatal("formatting-only edit changed semantic hash")
	}
	for _, line := range before[0].ExecutableLines {
		if line == 4 {
			t.Fatal("nested function body attributed to outer function")
		}
	}
}
