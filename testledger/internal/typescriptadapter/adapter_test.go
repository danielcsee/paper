package typescriptadapter

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/danielcsee/sciterm/testledger/internal/config"
)

func TestDiscoverTypeScriptFunctions(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is unavailable")
	}
	ui, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "ui"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ui, "node_modules", "typescript")); err != nil {
		t.Skip("project TypeScript compiler is unavailable")
	}
	root := t.TempDir()
	source := `export function add(a: number, b: number) { return a + b }
export class Box { value() { return 1 } }
export const double = (n: number) => n * 2
`
	if err := os.WriteFile(filepath.Join(root, "sample.ts"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	symbols, err := (Adapter{}).Discover(context.Background(), root, config.Language{Node: "node", WorkingDirectory: ui, Include: []string{"**/*.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 3 {
		t.Fatalf("got %d symbols: %+v", len(symbols), symbols)
	}
	got := map[string]bool{}
	for _, symbol := range symbols {
		got[symbol.QualifiedName] = true
	}
	for _, want := range []string{"add", "Box.value", "double"} {
		if !got[want] {
			t.Errorf("missing %s", want)
		}
	}
}
