// Explicit queueing/plan only; the ordinary worker retains stage budgets.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"os"
)

func main() {
	owner := flag.String("owner-id", "", "required owner UUID")
	flag.Parse()
	if !memory.ID(*owner).Valid() || os.Getenv("PCAS_DATABASE_URL") == "" {
		fmt.Fprintln(os.Stderr, "requires -owner-id and PCAS_DATABASE_URL")
		os.Exit(1)
	}
	ctx := context.Background()
	s, e := postgres.Open(ctx, os.Getenv("PCAS_DATABASE_URL"))
	if e != nil {
		fail(e)
	}
	defer s.Close()
	if e = s.CheckSchema(ctx); e != nil {
		fail(e)
	}
	n, e := s.QueueRuleReorganize(ctx, memory.Scope{OwnerID: memory.ID(*owner), IsOwner: true, PrincipalID: "owner"})
	if e != nil {
		fail(e)
	}
	fmt.Printf("pending_rule_memories=%d expected_successful_calls=%d batch_limit=40 organize_budget_per_hour=40\n", n, (n+39)/40)
}
func fail(e error) { fmt.Fprintln(os.Stderr, e); os.Exit(1) }
