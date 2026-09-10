package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCreatesEmbeddedDefaultOnFirstRun(t *testing.T) {
	root := t.TempDir()
	cfg, err := Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "testledger.toml")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, EmbeddedDefault()) {
		t.Fatal("created config differs from embedded default")
	}
	if cfg.Database != ".testledger/testledger.db" || len(cfg.Languages) != 1 {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
}

func TestExistingConfigIsAuthoritative(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "testledger.toml")
	contents := []byte("schema_version=1\ndatabase='custom.db'\nartifact_directory='custom-artifacts'\n[[languages]]\nname='go'\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database != "custom.db" || cfg.Languages[0].Name != "go" {
		t.Fatalf("on-disk config was not authoritative: %#v", cfg)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, contents) {
		t.Fatal("load changed the existing config")
	}
}

func TestResetBacksUpAndRestoresDefault(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "custom.toml")
	original := []byte("schema_version=1\n[[languages]]\nname='go'\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, backup, err := Reset(root, "custom.toml")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != path || backup != path+".bak" {
		t.Fatalf("unexpected paths: %s %s", resolved, backup)
	}
	gotBackup, _ := os.ReadFile(backup)
	gotDefault, _ := os.ReadFile(path)
	if !bytes.Equal(gotBackup, original) || !bytes.Equal(gotDefault, EmbeddedDefault()) {
		t.Fatal("reset did not preserve old config and restore embedded default")
	}
}

func TestLegacyConfigLocationRemainsAuthoritative(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "testledger", "testledger.toml")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("schema_version=1\n[[languages]]\nname='go'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ConfigPath(root, ""); got != legacy {
		t.Fatalf("ConfigPath() = %s, want %s", got, legacy)
	}
}
