package app_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	"./internal/contextbuilder/...",
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
			if dependency == "github.com/soaringjerry/PCAS/internal/postgres" || strings.HasPrefix(dependency, "github.com/soaringjerry/PCAS/internal/postgres/") || strings.HasPrefix(dependency, "github.com/jackc/pgx") {
				t.Errorf("%s depends on %s", pattern, dependency)
			}
		}
	}
}

// A storage package keeps the records of model calls. It must not make a
// model call: the domain package that owns a flow builds the request and
// calls the gateway, and internal/app builds the gateway. Add each storage
// package here when the PostgreSQL adapter is split.
var storagePackages = []string{"../postgres", "../postgres/kernel", "../postgres/memoryread", "../postgres/usestore", "../postgres/interpstore", "../postgres/workspacestore", "../postgres/intakestore", "../postgres/callstore", "../postgres/statestore", "../postgres/evidencestore", "../postgres/workstore", "../postgres/budgetstore", "../postgres/claimstore", "../postgres/accessstore", "../postgres/sourcestore", "../postgres/actionlog", "../postgres/itemstore", "../postgres/ownerstore", "../postgres/activitystore", "../postgres/schedulestore", "../postgres/usagelog", "../postgres/memorystore"}

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

// itemWrite matches an SQL statement that writes the items table.
var itemWrite = regexp.MustCompile(`(?i)\b(insert\s+into|update|delete\s+from)\s+work_items\b`)

// The items table has one owner: internal/postgres/itemstore. Each other
// storage package changes an item through that package, so that the plan
// rule, the text blocks, and the checks of an item apply to every write. A
// statement that takes its table name from a variable is not seen by this
// test; the two such statements send the items table to the owner.
func TestOnlyTheItemOwnerWritesTheItemsTable(t *testing.T) {
	for _, dir := range storagePackages {
		if filepath.Base(dir) == "itemstore" {
			continue
		}
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			source, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			for number, line := range strings.Split(string(source), "\n") {
				if itemWrite.MatchString(line) {
					t.Errorf("%s:%d writes work_items outside the item owner", file, number+1)
				}
			}
		}
	}
}
