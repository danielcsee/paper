package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/danielcsee/sciterm/testledger/internal/app"
	"github.com/danielcsee/sciterm/testledger/internal/ops"
)

const instructions = `Start with get_next_actions. Deterministic evidence comes from scans, affected-test selection, test runs, branch gaps, and mutation signals; do not infer it. Propose tests only for current symbol versions. Never call record_proposal_decision or record_disposition unless the human explicitly approved that exact action; set human_confirmed=true and preserve their reason. After writing approved tests, record exact native test IDs with record_proposal_implementation, then start_test_run and poll get_async_test_run. Fix failures and rerun until passing.`

type Server struct {
	app *app.App
	out io.Writer
	mu  sync.Mutex
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type tool struct {
	Name         string         `json:"name"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema"`
	Annotations  map[string]any `json:"annotations"`
}

func New(application *app.App, output io.Writer) *Server {
	return &Server{app: application, out: output}
}

func (s *Server) Serve(ctx context.Context, input io.Reader) error {
	if err := s.app.Store.InterruptActiveJobs(ctx); err != nil {
		return err
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			s.write(response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		result, rpcErr := s.handle(ctx, req)
		s.write(response{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr})
	}
	return scanner.Err()
}

func (s *Server) write(value response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	encoded, _ := json.Marshal(value)
	_, _ = s.out.Write(append(encoded, '\n'))
}

func (s *Server) handle(ctx context.Context, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		protocol := negotiateProtocol(params.ProtocolVersion)
		return map[string]any{"protocolVersion": protocol, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]any{"name": "testledger", "version": "0.3.0"}, "instructions": instructions}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.tools()}, nil
	case "tools/call":
		var call struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &call); err != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid tools/call parameters"}
		}
		structured, summary, err := s.callTool(ctx, call.Name, call.Arguments)
		if err != nil {
			return toolError(err), nil
		}
		return toolResult(structured, summary), nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found"}
	}
}

// tools renders the shared registry as MCP tool descriptors. Nothing here
// describes an argument: the schema comes from the operation's input struct,
// which is the same struct the CLI parses flags into.
func (s *Server) tools() []tool {
	result := []tool{}
	for _, operation := range ops.For(ops.SurfaceMCP) {
		if operation.Decision && !s.app.Config.AllowAgentDecisions {
			// Not advertised at all when the gate is closed: a tool an agent
			// cannot successfully call is worse than one it cannot see.
			continue
		}
		result = append(result, tool{
			Name:         operation.Name,
			Title:        operation.Title,
			Description:  operation.Summary,
			InputSchema:  ops.InputSchema(operation),
			OutputSchema: map[string]any{"type": "object", "additionalProperties": true},
			Annotations:  ops.Annotations(operation),
		})
	}
	return result
}

func negotiateProtocol(requested string) string {
	switch requested {
	case "2024-11-05", "2025-03-26", "2025-06-18":
		return requested
	default:
		return "2025-06-18"
	}
}

func decode(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("multiple JSON values are not allowed")
	}
	return nil
}

// callTool dispatches to the shared registry. The only MCP-specific logic is
// the decision gate and forcing test runs to be asynchronous.
func (s *Server) callTool(ctx context.Context, name string, raw json.RawMessage) (any, string, error) {
	operation, known := ops.ByName(name)
	if !known || operation.Only == ops.SurfaceCLI {
		return nil, "", fmt.Errorf("unknown tool %q", name)
	}
	input := operation.New()
	if err := decode(raw, input); err != nil {
		return nil, "", err
	}
	if operation.Decision {
		if !s.app.Config.AllowAgentDecisions {
			return nil, "", fmt.Errorf("%s records a human decision and is disabled for agents; the human runs `testledger %s` themselves, or sets allow_agent_decisions in the configuration", name, operation.Verb)
		}
		if !ops.Confirmed(input) {
			return nil, "", fmt.Errorf("human_confirmed must be true, and only after the human stated this exact decision")
		}
	}
	if runs, ok := input.(interface{ ForceAsync() }); ok {
		// MCP hosts impose tool timeouts, so a run always returns a job id.
		runs.ForceAsync()
	}
	result, err := operation.Run(ctx, s.app, input)
	if err != nil {
		return nil, "", err
	}
	return result.Value, result.Summary, nil
}

func toolResult(structured any, summary string) map[string]any {
	return map[string]any{"structuredContent": structured, "content": []map[string]any{{"type": "text", "text": summary}}, "isError": false}
}
func toolError(err error) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": err.Error()}}, "isError": true}
}
