// The operation registry: every capability Testledger exposes, declared once.
//
// The CLI and the MCP server are transports over this package. Each operation
// names itself, describes its input as a Go struct, and returns a Result. The
// CLI generates its flags from that struct and the MCP server generates its
// JSON Schema from the same struct, so a capability cannot exist on one surface
// and be missing from the other -- which is how the two hand-written surfaces
// drifted apart in the first place.
package ops

import (
	"context"
	"sort"

	"github.com/danielcsee/sciterm/testledger/internal/app"
)

// Surface identifies which transport is invoking an operation. Operations are
// available on both unless Only says otherwise.
type Surface string

const (
	SurfaceCLI Surface = "cli"
	SurfaceMCP Surface = "mcp"
)

// Result is what an operation returns, shaped for both transports at once.
type Result struct {
	// Value is the structured payload: the CLI prints it under --json, the MCP
	// server returns it as structuredContent.
	Value any
	// Summary is one line of prose. The MCP server sends it as the text
	// content block; the CLI prints it as the headline of human output.
	Summary string
	// Detail is optional extra human-readable output. The CLI prints it below
	// the summary; the MCP server never sends it, because a agent paying by
	// the token wants the structured value instead.
	Detail []string
	// ExitCode, when non-zero, is the process exit status the CLI should use.
	// It carries a failing test run's native code out to CI.
	ExitCode int
}

// Op is one capability. Handlers receive the populated input struct.
type Op struct {
	// Name is canonical and snake_case. It is the MCP tool name.
	Name string
	// Verb is the CLI subcommand. Usually a shorter form of Name.
	Verb string
	// Title is a human label; Summary is one sentence of what it does. Both
	// reach the MCP tool listing and the CLI help.
	Title   string
	Summary string
	// ReadOnly marks an operation that writes nothing, which becomes the MCP
	// readOnlyHint annotation.
	ReadOnly bool
	// Decision marks an operation that records a human's decision -- approving
	// a proposal, dispositioning a symbol. These are gated: see Gate.
	Decision bool
	// Only restricts an operation to one transport. Empty means both.
	Only Surface
	// Order places the operation in workflow sequence for listings, so help
	// output reads as the loop an agent actually walks rather than as an
	// alphabetical pile.
	Order int
	// New returns a pointer to a zero input struct for this operation.
	New func() any
	// Run executes the operation against the populated input.
	Run func(ctx context.Context, a *app.App, in any) (Result, error)
}

// registry is keyed by canonical name. Registration order does not matter;
// callers sort.
var registry = map[string]*Op{}

func register(op *Op) {
	if _, exists := registry[op.Name]; exists {
		panic("duplicate operation " + op.Name)
	}
	registry[op.Name] = op
}

// All returns every operation in workflow order.
func All() []*Op {
	out := make([]*Op, 0, len(registry))
	for _, op := range registry {
		out = append(out, op)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// For returns the operations one transport exposes.
func For(surface Surface) []*Op {
	out := []*Op{}
	for _, op := range All() {
		if op.Only == "" || op.Only == surface {
			out = append(out, op)
		}
	}
	return out
}

// ByName looks an operation up by its canonical name.
func ByName(name string) (*Op, bool) {
	op, ok := registry[name]
	return op, ok
}

// ByVerb looks an operation up by its CLI subcommand.
func ByVerb(verb string) (*Op, bool) {
	for _, op := range registry {
		if op.Verb == verb && (op.Only == "" || op.Only == SurfaceCLI) {
			return op, true
		}
	}
	return nil, false
}
