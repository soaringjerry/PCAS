package postgres

import "github.com/soaringjerry/PCAS/internal/interpretation/comparison"

// The comparison rules are in the interpretation domain. Storage applies
// their result.
type compareEdge = comparison.Edge

func parseCompareOutput(text string, count int) ([]compareEdge, bool) {
	return comparison.ParseOutput(text, count)
}

func resolveCompareEdges(edges []compareEdge, eligible, protected map[int]bool) []compareEdge {
	return comparison.ResolveEdges(edges, eligible, protected)
}
