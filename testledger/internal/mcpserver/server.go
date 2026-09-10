package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/app"
	"github.com/danielcsee/sciterm/testledger/internal/model"
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
		return map[string]any{"tools": tools()}, nil
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

func object(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}
func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}
func boolean(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}
func integer(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description, "minimum": 0}
}
func array(items any, description string) map[string]any {
	return map[string]any{"type": "array", "items": items, "description": description}
}
func annotations(readOnly bool) map[string]any {
	return map[string]any{"readOnlyHint": readOnly, "destructiveHint": false, "openWorldHint": false}
}

func tools() []tool {
	result := []tool{
		{Name: "get_next_actions", Title: "Get next testing actions", Description: "Return the smallest actionable workflow state. Call this first and after every decision or test run.", InputSchema: object(map[string]any{"limit": integer("Maximum actions, default 10 and maximum 50.")}), Annotations: annotations(true)},
		{Name: "scan_changes", Title: "Scan changed functions", Description: "Create a deterministic inventory snapshot and return a compact changed-coverage summary.", InputSchema: object(map[string]any{"limit": integer("Maximum changed gaps to return.")}), Annotations: annotations(false)},
		{Name: "select_affected_tests", Title: "Select affected tests", Description: "Select tests for changed functions from the prior inventory's dynamic coverage. Requests a full-suite fallback when any changed symbol has no mapping.", InputSchema: object(map[string]any{"language": str("Optional configured language name.")}), Annotations: annotations(false)},
		{Name: "list_coverage_gaps", Title: "List coverage gaps", Description: "Read current uncovered function versions with cursor pagination.", InputSchema: object(map[string]any{"changed_only": boolean("Only return functions changed since the prior inventory."), "limit": integer("Page size, maximum 100."), "cursor": integer("Offset cursor from the prior page.")}), Annotations: annotations(true)},
		{Name: "get_symbol_context", Title: "Get function context", Description: "Read a current symbol, its bounded source span, and prior covering tests.", InputSchema: object(map[string]any{"symbol_key": str("Stable symbol key from a coverage gap."), "include_source": boolean("Include the function source span.")}, "symbol_key"), Annotations: annotations(true)},
		{Name: "propose_tests", Title: "Propose tests", Description: "Record agent-proposed test cases for exact current function versions; this does not approve them.", InputSchema: object(map[string]any{"targets": array(str("Target symbol key."), "Current symbol keys."), "rationale": str("Why these cases are appropriate."), "created_by": str("Agent or actor identifier."), "cases": array(object(map[string]any{"name": str("Proposed test name."), "test_file": str("Proposed repository-relative test file."), "description": str("Behavior and boundary being tested."), "assertions": array(str("Expected assertion."), "Expected assertions.")}, "name", "test_file", "description", "assertions"), "Proposed test cases.")}, "targets", "rationale", "created_by", "cases"), Annotations: annotations(false)},
		{Name: "list_proposals", Title: "List test proposals", Description: "Read test proposals with optional status filtering and cursor pagination.", InputSchema: object(map[string]any{"status": str("Optional proposal status."), "limit": integer("Page size, maximum 100."), "cursor": integer("Offset cursor.")}), Annotations: annotations(true)},
		{Name: "get_proposal", Title: "Get test proposal", Description: "Read one proposal, its exact version targets, decision, and implementation links.", InputSchema: object(map[string]any{"proposal_id": str("Proposal identifier.")}, "proposal_id"), Annotations: annotations(true)},
		{Name: "record_proposal_decision", Title: "Record human proposal decision", Description: "Record an explicit human approval or rejection. Never call without the human's exact decision.", InputSchema: object(map[string]any{"proposal_id": str("Proposal identifier."), "decision": map[string]any{"type": "string", "enum": []string{"approved", "rejected"}}, "decided_by": str("Human actor identifier."), "reason": str("Human rationale."), "human_confirmed": boolean("Must be true only after explicit human confirmation.")}, "proposal_id", "decision", "decided_by", "reason", "human_confirmed"), Annotations: annotations(false)},
		{Name: "record_proposal_implementation", Title: "Record implemented tests", Description: "After editing approved tests, record exact native test IDs intended to cover each target.", InputSchema: object(map[string]any{"proposal_id": str("Approved proposal identifier."), "links": array(object(map[string]any{"symbol_key": str("Proposal target symbol."), "test_key": str("Exact native test ID.")}, "symbol_key", "test_key"), "Intended coverage links.")}, "proposal_id", "links"), Annotations: annotations(false)},
		{Name: "record_disposition", Title: "Record human skip", Description: "Record a human-approved skip for a function version or, explicitly, all future versions.", InputSchema: object(map[string]any{"symbol_key": str("Current symbol key."), "reason": str("Human rationale."), "approved_by": str("Human actor identifier."), "durable": boolean("Apply to future versions; false is safer."), "expires_at": str("Optional RFC3339 expiration."), "human_confirmed": boolean("Must be true only after explicit human confirmation.")}, "symbol_key", "reason", "approved_by", "human_confirmed"), Annotations: annotations(false)},
		{Name: "start_test_run", Title: "Start asynchronous tests", Description: "Queue a configured language test run and return immediately with a job ID.", InputSchema: object(map[string]any{"language": str("Configured language name; required when more than one exists."), "affected": boolean("Select from historical coverage; falls back to the full suite when unsafe."), "proposal_id": str("Optional implemented proposal to verify."), "test_keys": array(str("Exact native test ID or configured test path."), "Optional test selectors. Flags are rejected.")}), Annotations: annotations(false)},
		{Name: "run_mutation", Title: "Run mutation adapter", Description: "Run the optional mutation command and persist normalized killed/survived signals.", InputSchema: object(map[string]any{"language": str("Configured language name; required when more than one exists.")}), Annotations: annotations(false)},
		{Name: "get_async_test_run", Title: "Get asynchronous test run", Description: "Poll a job and read paginated normalized test results after a run ID is available.", InputSchema: object(map[string]any{"job_id": str("Asynchronous job identifier."), "failures_only": boolean("Return only failed/error cases."), "limit": integer("Page size, maximum 100."), "cursor": integer("Offset cursor.")}, "job_id"), Annotations: annotations(true)},
		{Name: "get_test_run", Title: "Get persisted test run", Description: "Read a persisted test run directly with paginated normalized cases.", InputSchema: object(map[string]any{"run_id": str("Test run identifier."), "failures_only": boolean("Return only failed/error cases."), "limit": integer("Page size, maximum 100."), "cursor": integer("Offset cursor.")}, "run_id"), Annotations: annotations(true)},
		{Name: "get_failure_context", Title: "Get test failure context", Description: "Read one normalized failure and the latest agent diagnosis without loading the entire run.", InputSchema: object(map[string]any{"run_id": str("Test run identifier."), "test_key": str("Exact native test ID from a failure page.")}, "run_id", "test_key"), Annotations: annotations(true)},
		{Name: "record_failure_diagnosis", Title: "Record failure diagnosis", Description: "Append an agent interpretation while preserving deterministic failure evidence.", InputSchema: object(map[string]any{"run_id": str("Test run identifier."), "test_key": str("Exact failing native test ID."), "category": map[string]any{"type": "string", "enum": []string{"product_bug", "incorrect_test_expectation", "test_setup_defect", "environment_problem", "flaky", "unknown"}}, "explanation": str("Evidence-based diagnosis."), "created_by": str("Agent or actor identifier.")}, "run_id", "test_key", "category", "explanation", "created_by"), Annotations: annotations(false)},
	}
	for i := range result {
		result[i].OutputSchema = map[string]any{"type": "object", "additionalProperties": true}
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

func (s *Server) callTool(ctx context.Context, name string, raw json.RawMessage) (any, string, error) {
	switch name {
	case "get_next_actions":
		var in struct {
			Limit int `json:"limit"`
		}
		if err := decode(raw, &in); err != nil {
			return nil, "", err
		}
		result, err := s.app.NextActions(ctx, in.Limit)
		return result, "Current workflow state: " + result.State, err
	case "scan_changes":
		var in struct {
			Limit int `json:"limit"`
		}
		if err := decode(raw, &in); err != nil {
			return nil, "", err
		}
		check, err := s.app.Check(ctx)
		if err != nil {
			return nil, "", err
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}
		if limit > 100 {
			limit = 100
		}
		items := check.ChangedGaps
		if len(items) > limit {
			items = items[:limit]
		}
		result := map[string]any{"inventory_run_id": check.InventoryRunID, "scanned": check.Scanned, "changed": check.Changed, "changed_gap_count": len(check.ChangedGaps), "changed_gaps": items, "deleted": check.Deleted, "skipped": check.Skipped}
		return result, fmt.Sprintf("Scanned %d functions; %d changed gaps.", check.Scanned, len(check.ChangedGaps)), nil
	case "select_affected_tests":
		var in struct {
			Language string `json:"language"`
		}
		if err := decode(raw, &in); err != nil {
			return nil, "", err
		}
		result, err := s.app.SelectAffectedTestsForLanguage(ctx, in.Language)
		return result, fmt.Sprintf("Selected %d affected tests; full-suite fallback is %t.", len(result.TestKeys), result.Fallback), err
	case "list_coverage_gaps":
		var in struct {
			Changed bool `json:"changed_only"`
			Limit   int  `json:"limit"`
			Cursor  int  `json:"cursor"`
		}
		if err := decode(raw, &in); err != nil {
			return nil, "", err
		}
		page, err := s.app.ListCoverageGaps(ctx, in.Changed, in.Limit, in.Cursor)
		return page, fmt.Sprintf("Found %d current coverage gaps.", page.Total), err
	case "get_symbol_context":
		var in struct {
			Key    string `json:"symbol_key"`
			Source bool   `json:"include_source"`
		}
		if err := decode(raw, &in); err != nil {
			return nil, "", err
		}
		result, err := s.app.GetSymbolContext(ctx, in.Key, in.Source)
		return result, "Loaded context for " + in.Key, err
	case "propose_tests":
		var in struct {
			Targets   []string             `json:"targets"`
			Rationale string               `json:"rationale"`
			CreatedBy string               `json:"created_by"`
			Cases     []model.ProposalCase `json:"cases"`
		}
		if err := decode(raw, &in); err != nil {
			return nil, "", err
		}
		result, err := s.app.CreateProposal(ctx, in.Targets, in.Cases, in.Rationale, in.CreatedBy)
		return result, "Recorded proposal; human decision is required.", err
	case "list_proposals":
		var in struct {
			Status string `json:"status"`
			Limit  int    `json:"limit"`
			Cursor int    `json:"cursor"`
		}
		if err := decode(raw, &in); err != nil {
			return nil, "", err
		}
		limit, cursor := in.Limit, in.Cursor
		if limit <= 0 {
			limit = 20
		}
		if limit > 100 {
			limit = 100
		}
		page, err := s.app.Store.ListProposals(ctx, in.Status, limit, cursor)
		return page, fmt.Sprintf("Found %d proposals.", page.Total), err
	case "get_proposal":
		var in struct {
			ID string `json:"proposal_id"`
		}
		if err := decode(raw, &in); err != nil {
			return nil, "", err
		}
		proposal, err := s.app.Store.GetProposal(ctx, in.ID)
		if err != nil {
			return nil, "", err
		}
		links, err := s.app.Store.ProposalLinks(ctx, in.ID)
		return map[string]any{"proposal": proposal, "links": links}, "Loaded proposal " + in.ID, err
	case "record_proposal_decision":
		var in struct {
			ID        string `json:"proposal_id"`
			Decision  string `json:"decision"`
			By        string `json:"decided_by"`
			Reason    string `json:"reason"`
			Confirmed bool   `json:"human_confirmed"`
		}
		if err := decode(raw, &in); err != nil {
			return nil, "", err
		}
		if !in.Confirmed {
			return nil, "", fmt.Errorf("human_confirmed must be true after explicit human approval or rejection")
		}
		result, err := s.app.DecideProposal(ctx, in.ID, in.Decision, in.By, in.Reason)
		return result, "Recorded explicit human decision: " + in.Decision, err
	case "record_proposal_implementation":
		var in struct {
			ID    string                   `json:"proposal_id"`
			Links []model.IntendedTestLink `json:"links"`
		}
		if err := decode(raw, &in); err != nil {
			return nil, "", err
		}
		result, err := s.app.MarkProposalImplemented(ctx, in.ID, in.Links)
		return result, "Recorded implementation; run tests to verify coverage links.", err
	case "record_disposition":
		var in struct {
			Key        string `json:"symbol_key"`
			Reason     string `json:"reason"`
			ApprovedBy string `json:"approved_by"`
			Durable    bool   `json:"durable"`
			Expires    string `json:"expires_at"`
			Confirmed  bool   `json:"human_confirmed"`
		}
		if err := decode(raw, &in); err != nil {
			return nil, "", err
		}
		if !in.Confirmed {
			return nil, "", fmt.Errorf("human_confirmed must be true after explicit human instruction")
		}
		var expiry *time.Time
		if in.Expires != "" {
			parsed, err := time.Parse(time.RFC3339, in.Expires)
			if err != nil {
				return nil, "", err
			}
			expiry = &parsed
		}
		err := s.app.AddSkipBy(ctx, in.Key, in.Reason, in.Durable, expiry, in.ApprovedBy)
		return map[string]any{"recorded": err == nil, "symbol_key": in.Key, "durable": in.Durable}, "Recorded human-approved disposition.", err
	case "start_test_run":
		var in struct {
			Language   string   `json:"language"`
			Affected   bool     `json:"affected"`
			ProposalID string   `json:"proposal_id"`
			TestKeys   []string `json:"test_keys"`
		}
		if err := decode(raw, &in); err != nil {
			return nil, "", err
		}
		job, err := s.app.StartAsyncTestLanguage(ctx, in.Language, in.TestKeys, in.ProposalID, in.Affected)
		return job, "Queued test run " + job.ID, err
	case "run_mutation":
		var in struct {
			Language string `json:"language"`
		}
		if err := decode(raw, &in); err != nil {
			return nil, "", err
		}
		result, err := s.app.RunMutation(ctx, in.Language)
		return result, fmt.Sprintf("Mutation run recorded %d signals.", len(result.Results)), err
	case "get_async_test_run":
		var in struct {
			JobID    string `json:"job_id"`
			Failures bool   `json:"failures_only"`
			Limit    int    `json:"limit"`
			Cursor   int    `json:"cursor"`
		}
		if err := decode(raw, &in); err != nil {
			return nil, "", err
		}
		job, run, page, err := s.app.GetAsyncTest(ctx, in.JobID, in.Limit, in.Cursor, in.Failures)
		return map[string]any{"job": job, "run": run, "cases": page}, "Job status: " + job.Status, err
	case "get_test_run":
		var in struct {
			RunID    string `json:"run_id"`
			Failures bool   `json:"failures_only"`
			Limit    int    `json:"limit"`
			Cursor   int    `json:"cursor"`
		}
		if err := decode(raw, &in); err != nil {
			return nil, "", err
		}
		limit, cursor := in.Limit, in.Cursor
		if limit <= 0 {
			limit = 20
		}
		if limit > 100 {
			limit = 100
		}
		run, page, err := s.app.Store.TestRun(ctx, in.RunID, limit, cursor, in.Failures)
		return map[string]any{"run": run, "cases": page}, "Test run status: " + run.Status, err
	case "get_failure_context":
		var in struct {
			RunID   string `json:"run_id"`
			TestKey string `json:"test_key"`
		}
		if err := decode(raw, &in); err != nil {
			return nil, "", err
		}
		result, err := s.app.Store.FailureContext(ctx, in.RunID, in.TestKey)
		return result, "Loaded failure context for " + in.TestKey, err
	case "record_failure_diagnosis":
		var in struct {
			RunID       string `json:"run_id"`
			TestKey     string `json:"test_key"`
			Category    string `json:"category"`
			Explanation string `json:"explanation"`
			CreatedBy   string `json:"created_by"`
		}
		if err := decode(raw, &in); err != nil {
			return nil, "", err
		}
		result, err := s.app.RecordFailureDiagnosis(ctx, in.RunID, in.TestKey, in.Category, in.Explanation, in.CreatedBy)
		return result, "Recorded diagnosis without changing the deterministic failure result.", err
	default:
		return nil, "", fmt.Errorf("unknown tool %q", name)
	}
}

func toolResult(structured any, summary string) map[string]any {
	return map[string]any{"structuredContent": structured, "content": []map[string]any{{"type": "text", "text": summary}}, "isError": false}
}
func toolError(err error) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": err.Error()}}, "isError": true}
}
