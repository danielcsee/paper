package goadapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danielcsee/sciterm/testledger/internal/config"
	"github.com/danielcsee/sciterm/testledger/internal/model"
	"github.com/danielcsee/sciterm/testledger/internal/pathmatch"
)

type Adapter struct{}

func (Adapter) Discover(ctx context.Context, root string, cfg config.Language) ([]model.Symbol, error) {
	var result []model.Symbol
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !pathmatch.Included(rel, cfg.Include, cfg.Exclude) || !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse Go %s: %w", rel, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			qualified := fn.Name.Name
			kind := "function"
			if fn.Recv != nil && len(fn.Recv.List) > 0 {
				qualified = receiverName(fn.Recv.List[0].Type) + "." + qualified
				kind = "method"
			}
			sig := printNode(fset, fn.Type)
			body := printNode(fset, fn.Body)
			lines := executableLines(fset, fn.Body)
			result = append(result, model.Symbol{Language: "go", Path: rel, QualifiedName: qualified, Kind: kind, StartLine: fset.Position(fn.Pos()).Line, EndLine: fset.Position(fn.End()).Line, ExecutableLines: lines, SignatureHash: hash(sig), BodyHash: hash(body), SemanticHash: hash(sig + "\x00" + body)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key() < result[j].Key() })
	return result, nil
}

func receiverName(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		return receiverName(v.X)
	case *ast.IndexExpr:
		return receiverName(v.X)
	case *ast.IndexListExpr:
		return receiverName(v.X)
	default:
		return "receiver"
	}
}
func printNode(fset *token.FileSet, node any) string {
	var b bytes.Buffer
	_ = printer.Fprint(&b, fset, node)
	return b.String()
}
func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func executableLines(fset *token.FileSet, body *ast.BlockStmt) []int {
	set := map[int]bool{}
	ast.Inspect(body, func(node ast.Node) bool {
		if node == nil {
			return true
		}
		if _, nested := node.(*ast.FuncLit); nested {
			return false
		}
		if _, ok := node.(ast.Stmt); ok {
			line := fset.Position(node.Pos()).Line
			if line > 0 {
				set[line] = true
			}
		}
		return true
	})
	lines := make([]int, 0, len(set))
	for line := range set {
		lines = append(lines, line)
	}
	sort.Ints(lines)
	return lines
}
