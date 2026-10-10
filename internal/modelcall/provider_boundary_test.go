package modelcall_test

import (
	"encoding/json"
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These are migration exceptions, not permitted new model entry points.
// Remove each exception when its complete workflow enters the gateway.
func TestProductionProviderCallsUseDeclaredMigrationBoundaries(t *testing.T) {
	allowed := map[string]int{
		"internal/modelcall/gateway.go:Call:GenerateProvider":              1,
		"internal/postgres/attachments.go:parseAttachment:TranscribeUsage": 1,
		"internal/modelcall/embedding.go:CallEmbedding:EmbedProviderUsage": 1,
		"internal/postgres/vision.go:readImage:Vision":                     1,
		"internal/telegram/poller.go:handle:Transcribe":                    1,
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "list", "-deps", "-export", "-json", "./cmd/...", "./internal/...")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	type pkg struct {
		ImportPath, Export, Dir string
		GoFiles, Imports        []string
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	exports := map[string]string{}
	var packages []pkg
	for {
		var p pkg
		err := decoder.Decode(&p)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		exports[p.ImportPath] = p.Export
		if strings.HasPrefix(p.ImportPath, "github.com/soaringjerry/PCAS/") {
			packages = append(packages, p)
		}
	}
	methods := map[string]bool{"Generate": true, "GenerateProvider": true, "GenerateSchema": true, "GenerateWithSearch": true, "GenerateWithSearchSchema": true, "Embed": true, "EmbedQuery": true, "EmbedProvider": true, "EmbedProviderUsage": true, "Transcribe": true, "TranscribeUsage": true, "Vision": true, "Route": true}
	journalWrite := regexp.MustCompile(`(?i)\b(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+"?model_calls"?\b`)
	for _, p := range packages {
		if strings.Contains(p.ImportPath, "/internal/ai") || strings.Contains(p.ImportPath, "/cmd/pcas-eval") {
			continue
		}
		if strings.HasSuffix(p.ImportPath, "/internal/modelcall") || strings.HasSuffix(p.ImportPath, "/internal/prompts") {
			for _, path := range p.Imports {
				if strings.HasSuffix(path, "/internal/postgres") {
					t.Error("gateway and registry cannot import concrete storage")
				}
			}
		}
		fs := token.NewFileSet()
		var files []*ast.File
		for _, name := range p.GoFiles {
			f, err := parser.ParseFile(fs, filepath.Join(p.Dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
		}
		info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}}
		config := types.Config{Importer: importer.ForCompiler(fs, "gc", func(path string) (io.ReadCloser, error) { return os.Open(exports[path]) })}
		if _, err := config.Check(p.ImportPath, fs, files, info); err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			name, _ := filepath.Rel(root, fs.Position(file.Pos()).Filename)
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok {
						return true
					}
					selector, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					method, ok := info.Uses[selector.Sel].(*types.Func)
					if !ok || method.Pkg() == nil {
						return true
					}
					path := method.Pkg().Path()
					if strings.HasPrefix(path, "github.com/jackc/pgx/") && (method.Name() == "Exec" || method.Name() == "Query" || method.Name() == "QueryRow") && len(call.Args) > 1 {
						// This first table guard covers resolved constant SQL,
						// including named constants and constant concatenation.
						// Complete domain and dynamic-SQL ownership remains in
						// the foundation's later owner-boundary migration.
						value := info.Types[call.Args[1]].Value
						if value != nil && value.Kind() == constant.String && journalWrite.MatchString(constant.StringVal(value)) && filepath.ToSlash(name) != "internal/postgres/model_calls.go" && filepath.ToSlash(name) != "internal/postgres/model_calls_interactive.go" && filepath.ToSlash(name) != "internal/postgres/model_calls_interactive_recovery.go" && filepath.ToSlash(name) != "internal/postgres/model_calls_embedding.go" {
							t.Errorf("call journal mutation outside its storage owner: %s:%s", name, fn.Name.Name)
						}
					}
					if !methods[method.Name()] {
						return true
					}
					if !strings.Contains(path, "/internal/ai") && path != "github.com/soaringjerry/PCAS/internal/modelcall" && !(path == "github.com/soaringjerry/PCAS/internal/telegram" && method.Name() == "Transcribe") {
						return true
					}
					key := filepath.ToSlash(name) + ":" + fn.Name.Name + ":" + method.Name()
					if allowed[key] == 0 {
						t.Errorf("unapproved provider entry: %s", key)
					} else {
						allowed[key]--
					}
					return true
				})
			}
		}
	}
	for site, count := range allowed {
		if count != 0 {
			t.Errorf("remove or update stale migration exception: %s (%d)", site, count)
		}
	}
}
