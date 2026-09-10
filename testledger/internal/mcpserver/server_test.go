package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielcsee/sciterm/testledger/internal/app"
)

func mcpFixture(t *testing.T) *app.App {
	t.Helper()
	source := filepath.Join("..", "app", "testdata", "python_project")
	root := t.TempDir()
	err := filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, relative)
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
	application, err := app.Open(root, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	if _, err := application.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	return application
}

func TestStdioInitializeToolsAndWorkflow(t *testing.T) {
	application := mcpFixture(t)
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"test","version":"1"},"capabilities":{}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_next_actions","arguments":{"limit":2}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"record_disposition","arguments":{"symbol_key":"python:src/mathy.py:add","reason":"no","approved_by":"human","human_confirmed":false}}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := New(application, &output).Serve(context.Background(), strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d responses: %s", len(lines), output.String())
	}
	var initialized map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &initialized); err != nil {
		t.Fatal(err)
	}
	result := initialized["result"].(map[string]any)
	if result["instructions"] == "" {
		t.Fatal("missing server instructions")
	}
	var listed struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Result.Tools) < 10 {
		t.Fatalf("only %d tools", len(listed.Result.Tools))
	}
	var workflow struct {
		Result struct {
			Structured map[string]any `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &workflow); err != nil {
		t.Fatal(err)
	}
	if workflow.Result.Structured["state"] != "proposal_required" {
		t.Fatalf("unexpected workflow: %+v", workflow)
	}
	var denied struct {
		Result struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[3]), &denied); err != nil {
		t.Fatal(err)
	}
	if !denied.Result.IsError {
		t.Fatal("unconfirmed human write was accepted")
	}
}
