package typescriptadapter

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/danielcsee/sciterm/testledger/internal/config"
	"github.com/danielcsee/sciterm/testledger/internal/model"
	"github.com/danielcsee/sciterm/testledger/internal/pathmatch"
)

//go:embed discover.js
var discoverScript string

type Adapter struct{}

func (Adapter) Discover(ctx context.Context, root string, cfg config.Language) ([]model.Symbol, error) {
	files := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if (strings.HasSuffix(rel, ".ts") || strings.HasSuffix(rel, ".tsx")) && !strings.HasSuffix(rel, ".d.ts") && pathmatch.Included(rel, cfg.Include, cfg.Exclude) {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]any{"root": root, "working_directory": cfg.WorkingDirectory, "files": files})
	node := cfg.Node
	if node == "" {
		node = "node"
	}
	cmd := exec.CommandContext(ctx, node, "-e", discoverScript)
	cmd.Dir = root
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("TypeScript discovery failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var symbols []model.Symbol
	if err := json.Unmarshal(stdout.Bytes(), &symbols); err != nil {
		return nil, fmt.Errorf("decode TypeScript discovery: %w", err)
	}
	return symbols, nil
}
