package app

import (
	"os"
	"path/filepath"
	"testing"
)

func copyFixture(t *testing.T) string {
	t.Helper()
	source := filepath.Join("testdata", "python_project")
	destination := t.TempDir()
	err := filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, info.Mode())
	})
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

func openFixture(t *testing.T) (*App, string) {
	t.Helper()
	root := copyFixture(t)
	application, err := Open(root, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	return application, root
}
