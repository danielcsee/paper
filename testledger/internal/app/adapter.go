// The language adapter contract, defined here in the consuming package so a
// new language is a registry entry rather than another arm of a type switch.
package app

import (
	"context"

	"github.com/danielcsee/sciterm/testledger/internal/config"
	"github.com/danielcsee/sciterm/testledger/internal/goadapter"
	"github.com/danielcsee/sciterm/testledger/internal/model"
	"github.com/danielcsee/sciterm/testledger/internal/pythonadapter"
	"github.com/danielcsee/sciterm/testledger/internal/typescriptadapter"
)

// Discoverer enumerates the functions and methods of one configured language.
//
// Implementations run the language's own toolchain -- the Python standard
// library AST, go/ast, the TypeScript compiler API -- rather than reimplementing
// its grammar, so a symbol's boundaries mean what that language says they mean.
// A Discoverer holds no coverage policy and no state; it maps source to symbols.
type Discoverer interface {
	Discover(ctx context.Context, root string, lang config.Language) ([]model.Symbol, error)
}

// discoverers maps a configured language name to its adapter. Registering a
// language here is the only change discovery needs.
var discoverers = map[string]Discoverer{
	"python":     pythonadapter.Adapter{},
	"go":         goadapter.Adapter{},
	"typescript": typescriptadapter.Adapter{},
}

// discovererFor returns the adapter for a configured language name.
func discovererFor(name string) (Discoverer, error) {
	adapter, ok := discoverers[name]
	if !ok {
		return nil, unsupportedLanguageError(name)
	}
	return adapter, nil
}
