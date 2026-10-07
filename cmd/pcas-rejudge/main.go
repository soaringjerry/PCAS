// Explicit queueing only. The normal worker performs budgeted comparisons.
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
	owner := flag.String("owner-id", "", "owner to rejudge; required")
	flag.Parse()
	if !memory.ID(*owner).Valid() || os.Getenv("PCAS_DATABASE_URL") == "" {
		fmt.Fprintln(os.Stderr, "requires -owner-id and PCAS_DATABASE_URL")
		os.Exit(1)
	}
	ctx := context.Background()
	s, err := postgres.Open(ctx, os.Getenv("PCAS_DATABASE_URL"))
	if err != nil {
		fail(err)
	}
	defer s.Close()
	if err = s.CheckSchema(ctx); err != nil {
		fail(err)
	}
	n, err := s.QueueSupersededRejudge(ctx, memory.Scope{OwnerID: memory.ID(*owner), IsOwner: true, PrincipalID: "owner"})
	if err != nil {
		fail(err)
	}
	fmt.Printf("queued_pairs=%d expected_model_calls=%d compare_budget_per_hour=40\n", n, n)
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
