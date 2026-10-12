package app_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A domain package holds business rules. It reaches storage through its own
// interface, so it cannot depend on the PostgreSQL adapter or its driver.
// Add each package here when its domain leaves the storage package.
var domainPackages = []string{
	"./internal/workspace/...",
	"./internal/currentstate/...",
	"./internal/interpretation/...",
	"./internal/input/...",
	"./internal/contextbuilder",
	"./internal/retrieval",
	"./internal/modelcall/...",
	"./internal/prompts",
}

func TestDomainPackagesDoNotDependOnThePostgreSQLAdapter(t *testing.T) {
	for _, pattern := range domainPackages {
		command := exec.Command("go", "list", "-deps", pattern)
		command.Dir = "../.."
		output, err := command.Output()
		if err != nil {
			t.Fatal(pattern, err)
		}
		for _, dependency := range strings.Fields(string(output)) {
			if dependency == "github.com/soaringjerry/PCAS/internal/postgres" || strings.HasPrefix(dependency, "github.com/jackc/pgx") {
				t.Errorf("%s depends on %s", pattern, dependency)
			}
		}
	}
}

// A storage package keeps the records of model calls. It must not make a
// model call: the domain package that owns a flow builds the request and
// calls the gateway, and internal/app builds the gateway. Add each storage
// package here when the PostgreSQL adapter is split.
var storagePackages = []string{"../postgres"}

// gatewayCalls are the methods of the gateway that submit a model call.
var gatewayCalls = map[string]bool{"Call": true, "CallEmbedding": true, "CallVision": true, "CallTranscription": true}

func TestStoragePackagesDoNotMakeModelCalls(t *testing.T) {
	for _, dir := range storagePackages {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatal(dir, err, len(files))
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			source, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			set := token.NewFileSet()
			parsed, err := parser.ParseFile(set, file, source, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(parsed, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				// The gateway type and its constructor: a storage package
				// neither holds nor builds a gateway.
				if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "modelcall" && (selector.Sel.Name == "Gateway" || selector.Sel.Name == "New") {
					t.Errorf("%s: storage refers to modelcall.%s", set.Position(selector.Pos()), selector.Sel.Name)
				}
				return true
			})
			ast.Inspect(parsed, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok && gatewayCalls[selector.Sel.Name] {
					t.Errorf("%s: storage calls %s, a method that submits a model call", set.Position(call.Pos()), selector.Sel.Name)
				}
				return true
			})
		}
	}
}
