package app_test

import (
	"os/exec"
	"strings"
	"testing"
)

// A domain package holds business rules. It reaches storage through its own
// interface, so it cannot depend on the PostgreSQL adapter or its driver.
// Add each package here when its domain leaves the storage package.
var domainPackages = []string{
	"./internal/workspace/...",
	"./internal/currentstate/...",
	"./internal/modelcall",
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
