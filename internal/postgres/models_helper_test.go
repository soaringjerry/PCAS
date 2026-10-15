package postgres

import "github.com/soaringjerry/PCAS/internal/ai"

// registryOf returns the provider registry that a test gave the store.
func registryOf(s *Store) *ai.Registry {
	r, _ := s.Models.(*ai.Registry)
	return r
}
