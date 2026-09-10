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

// A decorated one-line method used to report its decorator as an executable
// line. The decorator runs at import time, in coverage.py's empty context, so
// it could never be attributed to a test -- capping an @property at 50% and
// putting it permanently below any sane threshold.
func TestDiscoverExcludesDefinitionTimeLines(t *testing.T) {
	root := t.TempDir()
	source := "import functools\n\n\nclass Holder:\n    @property\n    def value(self):\n        return 1\n\n\n@functools.lru_cache(\n    maxsize=None,\n)\ndef wrapped(\n    first,\n    second=2,\n):\n    return first + second\n"
	if err := os.WriteFile(filepath.Join(root, "sample.py"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	lang := config.Language{Name: "python", Python: "python3", Include: []string{"**/*.py"}}
	symbols, err := Adapter{}.Discover(context.Background(), root, lang)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string][]int{}
	for _, symbol := range symbols {
		byName[symbol.QualifiedName] = symbol.ExecutableLines
	}
	if got := byName["Holder.value"]; len(got) != 1 || got[0] != 7 {
		t.Fatalf("Holder.value executable lines = %v, want only the body line 7", got)
	}
	// The decorator spans lines 10-12 and the signature 13-16; only the body
	// statement on line 17 is evidence a test called the function.
	if got := byName["wrapped"]; len(got) != 1 || got[0] != 17 {
		t.Fatalf("wrapped executable lines = %v, want only the body line 17", got)
	}
}
