package config

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

//go:embed default.toml
var embeddedDefault []byte

type Config struct {
	SchemaVersion     int             `toml:"schema_version"`
	Database          string          `toml:"database"`
	ArtifactDirectory string          `toml:"artifact_directory"`
	Languages         []Language      `toml:"languages"`
	Execution         ExecutionConfig `toml:"execution"`
}

type Language struct {
	Name             string         `toml:"name"`
	Python           string         `toml:"python"`
	Node             string         `toml:"node"`
	WorkingDirectory string         `toml:"working_directory"`
	Include          []string       `toml:"include"`
	Exclude          []string       `toml:"exclude"`
	Test             TestConfig     `toml:"test"`
	Coverage         CoverageConfig `toml:"coverage"`
	Mutation         MutationConfig `toml:"mutation"`
}

type TestConfig struct {
	Runner    string   `toml:"runner"`
	Command   []string `toml:"command"`
	TestRoots []string `toml:"test_roots"`
}

type CoverageConfig struct {
	Source             []string `toml:"source"`
	Branch             bool     `toml:"branch"`
	MinimumLinePercent float64  `toml:"minimum_line_percent"`
	Format             string   `toml:"format"`
	File               string   `toml:"file"`
}

type MutationConfig struct {
	Command []string `toml:"command"`
	Format  string   `toml:"format"`
}

type ExecutionConfig struct {
	TimeoutSeconds       int      `toml:"timeout_seconds"`
	MaxOutputBytes       int64    `toml:"max_output_bytes"`
	MaxParallelRuns      int      `toml:"max_parallel_runs"`
	EnvironmentAllowlist []string `toml:"environment_allowlist"`
}

func Load(root, path string) (Config, error) {
	path, _, err := Ensure(root, path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	if cfg.SchemaVersion != 1 {
		return Config{}, fmt.Errorf("unsupported schema_version %d", cfg.SchemaVersion)
	}
	if cfg.Database == "" {
		cfg.Database = ".testledger/testledger.db"
	}
	if cfg.ArtifactDirectory == "" {
		cfg.ArtifactDirectory = ".testledger/artifacts"
	}
	if cfg.Execution.TimeoutSeconds <= 0 {
		cfg.Execution.TimeoutSeconds = 600
	}
	if cfg.Execution.MaxOutputBytes <= 0 {
		cfg.Execution.MaxOutputBytes = 1 << 20
	}
	if cfg.Execution.MaxParallelRuns <= 0 {
		cfg.Execution.MaxParallelRuns = 1
	}
	if len(cfg.Languages) == 0 {
		return Config{}, errors.New("config must define at least one language")
	}
	for i := range cfg.Languages {
		lang := &cfg.Languages[i]
		if lang.Name == "python" && lang.Python == "" {
			lang.Python = "python3"
		}
		if lang.Name == "typescript" && lang.Node == "" {
			lang.Node = "node"
		}
		if lang.Test.Runner == "" {
			switch lang.Name {
			case "python":
				lang.Test.Runner = "pytest"
			case "go":
				lang.Test.Runner = "go-test-json"
			case "typescript":
				lang.Test.Runner = "vitest-json"
			}
		}
		if lang.Coverage.MinimumLinePercent <= 0 {
			lang.Coverage.MinimumLinePercent = 80
		}
	}
	return cfg, nil
}

// ConfigPath resolves an explicit path or chooses the project-root default. The
// old testledger/testledger.toml location is recognized for existing installs.
func ConfigPath(root, path string) string {
	if path != "" {
		return Resolve(root, path)
	}
	preferred := filepath.Join(root, "testledger.toml")
	legacy := filepath.Join(root, "testledger", "testledger.toml")
	if _, err := os.Stat(preferred); err == nil {
		return preferred
	}
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return preferred
}

// Ensure writes the embedded, versioned default on first use. Once present,
// the on-disk file is authoritative and is never merged with new defaults.
func Ensure(root, path string) (string, bool, error) {
	resolved := ConfigPath(root, path)
	if _, err := os.Stat(resolved); err == nil {
		return resolved, false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return resolved, false, fmt.Errorf("inspect config %s: %w", resolved, err)
	}
	if err := writeDefault(resolved); err != nil {
		return resolved, false, err
	}
	return resolved, true, nil
}

// Reset backs up an existing authoritative file to <path>.bak and replaces it
// byte-for-byte with the embedded default. A previous .bak is replaced.
func Reset(root, path string) (string, string, error) {
	resolved := ConfigPath(root, path)
	backup := resolved + ".bak"
	if contents, err := os.ReadFile(resolved); err == nil {
		info, statErr := os.Stat(resolved)
		if statErr != nil {
			return resolved, backup, statErr
		}
		if err := atomicWrite(backup, contents, info.Mode().Perm()); err != nil {
			return resolved, backup, fmt.Errorf("back up config: %w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return resolved, backup, fmt.Errorf("read config for backup: %w", err)
	} else {
		backup = ""
	}
	if err := writeDefault(resolved); err != nil {
		return resolved, backup, err
	}
	return resolved, backup, nil
}

func EmbeddedDefault() []byte { return append([]byte(nil), embeddedDefault...) }

func writeDefault(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := atomicWrite(path, embeddedDefault, 0o644); err != nil {
		return fmt.Errorf("write default config %s: %w", path, err)
	}
	return nil
}

func atomicWrite(path string, contents []byte, mode fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".testledger-config-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(contents); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func Resolve(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func EnsureDirectories(root string, cfg Config) error {
	for _, path := range []string{filepath.Dir(Resolve(root, cfg.Database)), Resolve(root, cfg.ArtifactDirectory)} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (e ExecutionConfig) Timeout() time.Duration {
	return time.Duration(e.TimeoutSeconds) * time.Second
}
