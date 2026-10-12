package interpstore

import "github.com/soaringjerry/PCAS/internal/interpretation/comparison"

// The comparison rules are in the interpretation domain. Storage applies
// their result.
type CompareEdge = comparison.Edge

func ParseCompareOutput(text string, count int) ([]CompareEdge, bool) {
	return comparison.ParseOutput(text, count)
}

func resolveCompareEdges(edges []CompareEdge, eligible, protected map[int]bool) []CompareEdge {
	return comparison.ResolveEdges(edges, eligible, protected)
}
