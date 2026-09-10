package pythonadapter

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danielcsee/sciterm/testledger/internal/config"
	"github.com/danielcsee/sciterm/testledger/internal/model"
)

//go:embed scripts/*.py
var scripts embed.FS

type Adapter struct{}

type discoveryEvent struct {
	Type     string       `json:"type"`
	Symbol   model.Symbol `json:"symbol"`
	Severity string       `json:"severity"`
	Path     string       `json:"path"`
	Message  string       `json:"message"`
}

func (Adapter) Discover(ctx context.Context, root string, lang config.Language) ([]model.Symbol, error) {
	temp, cleanup, err := extractScripts()
	if err != nil {
		return nil, err
	}
	defer cleanup()
	args := []string{filepath.Join(temp, "discover.py"), "--root", root}
	for _, pattern := range lang.Include {
		args = append(args, "--include", pattern)
	}
	for _, pattern := range lang.Exclude {
		args = append(args, "--exclude", pattern)
	}
	cmd := exec.CommandContext(ctx, lang.Python, args...)
	cmd.Dir = root
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Python adapter: %w", err)
	}
	var symbols []model.Symbol
	var diagnostics []string
	scanner := bufio.NewScanner(stdout)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 4*1024*1024)
	for scanner.Scan() {
		var event discoveryEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("decode adapter event: %w", err)
		}
		if event.Type == "symbol" {
			symbols = append(symbols, event.Symbol)
		} else if event.Severity == "error" {
			diagnostics = append(diagnostics, event.Path+": "+event.Message)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("Python adapter failed: %w: %s", err, stderr.String())
	}
	if len(diagnostics) > 0 {
		return nil, errors.New(strings.Join(diagnostics, "; "))
	}
	sort.Slice(symbols, func(i, j int) bool { return symbols[i].Key() < symbols[j].Key() })
	return symbols, nil
}

func ExtractTestPlugin() (string, func(), error) {
	temp, cleanup, err := extractScripts()
	if err != nil {
		return "", nil, err
	}
	return temp, cleanup, nil
}

func extractScripts() (string, func(), error) {
	temp, err := os.MkdirTemp("", "testledger-python-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(temp) }
	entries, err := scripts.ReadDir("scripts")
	if err != nil {
		cleanup()
		return "", nil, err
	}
	for _, entry := range entries {
		contents, err := scripts.ReadFile("scripts/" + entry.Name())
		if err != nil {
			cleanup()
			return "", nil, err
		}
		if err := os.WriteFile(filepath.Join(temp, entry.Name()), contents, 0o600); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return temp, cleanup, nil
}
