// Input structs for the registered operations.
//
// Tags do double duty: `json` names the field on both wires, `desc` becomes
// both the MCP schema description and the CLI help text, `req` marks it
// required on both, `pos` lets the CLI also accept it as a bare argument, and
// `enum` constrains it in the schema and at the command line alike.
package ops

import "github.com/danielcsee/sciterm/testledger/internal/model"

type emptyInput struct{}

type nextInput struct {
	Limit int `json:"limit" desc:"Maximum actions to return; default 10, maximum 50."`
}

type languageInput struct {
	Language string `json:"language" pos:"1" desc:"Configured language name; required when more than one is configured."`
}

type checkInput struct {
	All   bool `json:"all" desc:"Report every outstanding gap, not only those changed since the last inventory."`
	Limit int  `json:"limit" desc:"Maximum gaps to return; default 20, maximum 100."`
}

type gapsInput struct {
	ChangedOnly bool `json:"changed_only" desc:"Only functions changed since the previous inventory."`
	Page
}

type symbolInput struct {
	SymbolKey     string `json:"symbol_key" pos:"1" req:"true" desc:"Stable symbol key, as reported by a coverage gap."`
	IncludeSource bool   `json:"include_source" desc:"Include the function's source span."`
}

type testInput struct {
	Language   string   `json:"language" desc:"Configured language name; required when more than one is configured."`
	Affected   bool     `json:"affected" desc:"Select tests from prior coverage; falls back to the full suite when unsafe."`
	Async      bool     `json:"async" desc:"Queue the run and return a job id instead of waiting. Always true over MCP, where hosts impose tool timeouts."`
	ProposalID string   `json:"proposal_id" desc:"Optional implemented proposal whose links this run should verify."`
	TestKeys   []string `json:"test_keys" pos:"1" desc:"Exact native test IDs or configured test paths. Runner flags are rejected."`
}

type runInput struct {
	RunID        string `json:"run_id" pos:"1" desc:"Test run identifier."`
	JobID        string `json:"job_id" desc:"Asynchronous job identifier; poll this instead of run_id after an async run."`
	FailuresOnly bool   `json:"failures_only" desc:"Return only failed and errored cases."`
	Page
}

type failureInput struct {
	RunID   string `json:"run_id" req:"true" desc:"Test run identifier."`
	TestKey string `json:"test_key" pos:"1" req:"true" desc:"Exact native test ID, from a failures page."`
}

type diagnosisInput struct {
	RunID       string `json:"run_id" req:"true" desc:"Test run identifier."`
	TestKey     string `json:"test_key" pos:"1" req:"true" desc:"Exact failing native test ID."`
	Category    string `json:"category" req:"true" enum:"product_bug,incorrect_test_expectation,test_setup_defect,environment_problem,flaky,unknown" desc:"Root-cause category."`
	Explanation string `json:"explanation" req:"true" desc:"Evidence-based reasoning for the category."`
	CreatedBy   string `json:"created_by" desc:"Agent or actor identifier."`
}

type proposeInput struct {
	Targets   []string             `json:"targets" req:"true" desc:"Current symbol keys this proposal covers."`
	Rationale string               `json:"rationale" req:"true" desc:"Why these cases are the right ones."`
	CreatedBy string               `json:"created_by" req:"true" desc:"Agent or actor identifier."`
	Cases     []model.ProposalCase `json:"cases" req:"true" desc:"Proposed test cases. On the CLI, a JSON file."`
}

type proposalsInput struct {
	ProposalID string `json:"proposal_id" pos:"1" desc:"Read one proposal in full, including its link resolutions."`
	Status     string `json:"status" enum:"proposed,approved,rejected,implemented,verified,obsolete" desc:"Filter the listing by status."`
	Page
}

type implementedInput struct {
	ProposalID string                   `json:"proposal_id" pos:"1" req:"true" desc:"Approved proposal identifier."`
	Links      []model.IntendedTestLink `json:"links" req:"true" desc:"Intended symbol-to-test links. On the CLI, a JSON file."`
}

type decideInput struct {
	ProposalID string `json:"proposal_id" pos:"1" req:"true" desc:"Proposal identifier."`
	Decision   string `json:"decision" req:"true" enum:"approved,rejected" desc:"The human's decision."`
	DecidedBy  string `json:"decided_by" req:"true" desc:"Human actor identifier."`
	Reason     string `json:"reason" req:"true" desc:"The human's stated rationale."`
	Confirm
}

type skipInput struct {
	SymbolKey  string `json:"symbol_key" pos:"1" req:"true" desc:"Current symbol key."`
	Reason     string `json:"reason" req:"true" desc:"The human's stated rationale."`
	ApprovedBy string `json:"approved_by" desc:"Human actor identifier; defaults to \"human\"."`
	Durable    bool   `json:"durable" desc:"Apply to future versions of this symbol too. Version-scoped is safer and is the default."`
	ExpiresAt  string `json:"expires_at" desc:"Optional RFC3339 expiry."`
	Confirm
}

// ForceAsync makes a test run return a job id instead of blocking. The MCP
// transport calls it on every run because hosts impose tool timeouts; the CLI
// leaves the choice to --async.
func (t *testInput) ForceAsync() { t.Async = true }
