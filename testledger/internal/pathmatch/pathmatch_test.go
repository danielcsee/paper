package pathmatch

import "testing"

func TestDoubleStar(t *testing.T) {
	for _, name := range []string{"main.go", "cmd/tool/main.go"} {
		if !Match("**/*.go", name) {
			t.Fatalf("expected match for %s", name)
		}
	}
	if Match("src/*.go", "src/internal/x.go") {
		t.Fatal("single star crossed directory")
	}
}
