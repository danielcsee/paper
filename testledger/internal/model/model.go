package model

import "time"

type Symbol struct {
	Language        string `json:"language"`
	Path            string `json:"path"`
	QualifiedName   string `json:"qualified_name"`
	Kind            string `json:"kind"`
	StartLine       int    `json:"start_line"`
	EndLine         int    `json:"end_line"`
	ExecutableLines []int  `json:"executable_lines"`
	SemanticHash    string `json:"semantic_hash"`
	SignatureHash   string `json:"signature_hash"`
	BodyHash        string `json:"body_hash"`
}

func (s Symbol) Key() string {
	return s.Language + ":" + s.Path + ":" + s.QualifiedName
}

type Gap struct {
	SymbolKey         string   `json:"symbol_key"`
	Path              string   `json:"path"`
	QualifiedName     string   `json:"qualified_name"`
	Kind              string   `json:"kind"`
	StartLine         int      `json:"start_line"`
	EndLine           int      `json:"end_line"`
	SemanticHash      string   `json:"semantic_hash"`
	Changed           bool     `json:"changed"`
	Reason            string   `json:"reason"`
	BestCoverage      float64  `json:"best_coverage_percent"`
	CoveringTestCount int      `json:"covering_test_count"`
	MissingLines      []int    `json:"missing_lines,omitempty"`
	MissingBranches   []Branch `json:"missing_branches,omitempty"`
}

type Branch struct {
	From        int    `json:"from"`
	To          int    `json:"to"`
	Description string `json:"description"`
}

type AffectedSymbol struct {
	SymbolKey       string   `json:"symbol_key"`
	PreviousHash    string   `json:"previous_semantic_hash,omitempty"`
	CurrentHash     string   `json:"current_semantic_hash"`
	Tests           []string `json:"tests"`
	SelectionReason string   `json:"selection_reason"`
}

type AffectedTests struct {
	InventoryRunID string           `json:"inventory_run_id"`
	Symbols        []AffectedSymbol `json:"symbols"`
	TestKeys       []string         `json:"test_keys"`
	Fallback       bool             `json:"fallback_to_full_suite"`
	Reason         string           `json:"reason,omitempty"`
}

type MutationResult struct {
	SymbolKey string `json:"symbol_key"`
	Operator  string `json:"operator"`
	Status    string `json:"status"`
	TestKey   string `json:"test_key,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type MutationRun struct {
	RunID             string           `json:"run_id"`
	Language          string           `json:"language"`
	Status            string           `json:"status"`
	StartedAt         time.Time        `json:"started_at"`
	FinishedAt        time.Time        `json:"finished_at"`
	ExitCode          int              `json:"exit_code"`
	Results           []MutationResult `json:"results,omitempty"`
	InfrastructureErr string           `json:"infrastructure_error,omitempty"`
	ArtifactDirectory string           `json:"artifact_directory"`
}

type CheckResult struct {
	InventoryRunID string   `json:"inventory_run_id"`
	Scanned        int      `json:"scanned"`
	Changed        int      `json:"changed"`
	Deleted        []string `json:"deleted"`
	ChangedGaps    []Gap    `json:"changed_gaps"`
	AllGaps        []Gap    `json:"all_gaps"`
	Skipped        int      `json:"skipped"`
	GeneratedAt    string   `json:"generated_at"`
}

type TestCaseResult struct {
	TestKey         string  `json:"test_key"`
	Outcome         string  `json:"outcome"`
	Phase           string  `json:"phase"`
	DurationSeconds float64 `json:"duration_seconds"`
	FailureCategory string  `json:"failure_category,omitempty"`
	Message         string  `json:"message,omitempty"`
	Traceback       string  `json:"traceback,omitempty"`
	Stdout          string  `json:"stdout,omitempty"`
	Stderr          string  `json:"stderr,omitempty"`
}

type TestRunResult struct {
	RunID             string           `json:"run_id"`
	Status            string           `json:"status"`
	StartedAt         time.Time        `json:"started_at"`
	FinishedAt        time.Time        `json:"finished_at"`
	ExitCode          int              `json:"exit_code"`
	Passed            int              `json:"passed"`
	Failed            int              `json:"failed"`
	Errors            int              `json:"errors"`
	Skipped           int              `json:"skipped"`
	Cases             []TestCaseResult `json:"cases,omitempty"`
	InfrastructureErr string           `json:"infrastructure_error,omitempty"`
	ArtifactDirectory string           `json:"artifact_directory"`
	CoverageMappings  int              `json:"coverage_mappings"`
}

type Status struct {
	LatestInventoryID string         `json:"latest_inventory_id,omitempty"`
	LatestInventoryAt string         `json:"latest_inventory_at,omitempty"`
	SymbolCount       int            `json:"symbol_count"`
	OutstandingGaps   int            `json:"outstanding_gaps"`
	ActiveSkips       int            `json:"active_skips"`
	LatestTestRun     *TestRunResult `json:"latest_test_run,omitempty"`
}

type ProposalCase struct {
	Name        string   `json:"name"`
	TestFile    string   `json:"test_file"`
	Description string   `json:"description"`
	Assertions  []string `json:"assertions"`
}

type ProposalTarget struct {
	SymbolKey    string `json:"symbol_key"`
	SemanticHash string `json:"semantic_hash"`
}

type ProposalDecision struct {
	Decision  string `json:"decision"`
	DecidedBy string `json:"decided_by"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"created_at"`
}

type TestProposal struct {
	ID        string            `json:"id"`
	Status    string            `json:"status"`
	Rationale string            `json:"rationale"`
	Cases     []ProposalCase    `json:"cases"`
	Targets   []ProposalTarget  `json:"targets"`
	CreatedBy string            `json:"created_by"`
	CreatedAt string            `json:"created_at"`
	UpdatedAt string            `json:"updated_at"`
	Decision  *ProposalDecision `json:"decision,omitempty"`
	// Links are the intended symbol-to-test links recorded at implementation.
	// Exposed so a caller can see which links still block the proposal instead
	// of having to re-run tests to find out.
	Links []IntendedTestLink `json:"links,omitempty"`
}

// Resolution values for an IntendedTestLink. A link must reach one of the two
// terminal values before its proposal can close.
const (
	// LinkUnresolved: the named test has not yet covered the symbol to the
	// configured threshold. This is the only value that blocks a proposal.
	LinkUnresolved = "unresolved"
	// LinkVerified: a passing test reached the symbol version at or above the
	// threshold. Coverage evidence, produced by a run.
	LinkVerified = "verified"
	// LinkDispositioned: a human recorded an active disposition for the symbol,
	// so no coverage will ever be observed for it. A decision of record, kept
	// distinct from LinkVerified so the ledger never claims unproven evidence.
	LinkDispositioned = "dispositioned"
)

type IntendedTestLink struct {
	SymbolKey string `json:"symbol_key"`
	TestKey   string `json:"test_key"`
	Verified  bool   `json:"verified"`
	RunID     string `json:"verified_run_id,omitempty"`
	// Resolution is one of LinkUnresolved, LinkVerified or LinkDispositioned.
	Resolution string `json:"resolution"`
	// Reason explains a resolution the agent cannot infer, chiefly the
	// disposition rationale behind LinkDispositioned.
	Reason string `json:"reason,omitempty"`
}

type FailureDiagnosis struct {
	RunID       string `json:"run_id"`
	TestKey     string `json:"test_key"`
	Category    string `json:"category"`
	Explanation string `json:"explanation"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
}

type FailureContext struct {
	Result    TestCaseResult    `json:"result"`
	Diagnosis *FailureDiagnosis `json:"diagnosis,omitempty"`
}

type AsyncJob struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	Status      string         `json:"status"`
	Arguments   map[string]any `json:"arguments,omitempty"`
	CreatedAt   string         `json:"created_at"`
	StartedAt   string         `json:"started_at,omitempty"`
	FinishedAt  string         `json:"finished_at,omitempty"`
	ResultRunID string         `json:"result_run_id,omitempty"`
	Error       string         `json:"error,omitempty"`
}

type Page[T any] struct {
	Items      []T `json:"items"`
	NextCursor int `json:"next_cursor,omitempty"`
	Total      int `json:"total"`
}

type SymbolContext struct {
	Symbol        Symbol   `json:"symbol"`
	Source        string   `json:"source"`
	CoveringTests []string `json:"covering_tests"`
}

type NextAction struct {
	Type        string `json:"type"`
	ID          string `json:"id,omitempty"`
	SymbolKey   string `json:"symbol_key,omitempty"`
	TestKey     string `json:"test_key,omitempty"`
	Description string `json:"description"`
}

type NextActions struct {
	State   string         `json:"state"`
	Summary map[string]int `json:"summary"`
	Actions []NextAction   `json:"actions"`
}
