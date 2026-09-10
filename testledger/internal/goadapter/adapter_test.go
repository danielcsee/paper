package goadapter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/danielcsee/sciterm/testledger/internal/config"
)

func TestDiscoverFunctionsMethodsAndSemanticHash(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "pkg", "sample.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	source := "package pkg\n\ntype Counter struct{}\nfunc Add(a,b int) int { return a+b }\nfunc (Counter) Value() int { return 1 }\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	symbols, err := (Adapter{}).Discover(context.Background(), root, config.Language{Include: []string{"**/*.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 2 {
		t.Fatalf("got %d symbols: %+v", len(symbols), symbols)
	}
	if symbols[0].QualifiedName != "Add" || symbols[1].QualifiedName != "Counter.Value" {
		t.Fatalf("unexpected names: %+v", symbols)
	}
	first := symbols[0].SemanticHash
	if err := os.WriteFile(path, []byte("package pkg\n\n// comment\ntype Counter struct{}\nfunc Add(a, b int) int { return a + b }\nfunc (Counter) Value() int{return 1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	again, err := (Adapter{}).Discover(context.Background(), root, config.Language{Include: []string{"**/*.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if again[0].SemanticHash != first {
		t.Fatal("formatting/comment changed Go semantic hash")
	}
}
