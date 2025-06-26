package vectorpg

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/soaringjerry/pcas/internal/storage"
)

// Provider implements the VectorStorage interface using PostgreSQL with pgvector
type Provider struct {
	pool *pgxpool.Pool
}

// New creates a new PostgreSQL vector storage provider
func New(ctx context.Context, dsn string) (*Provider, error) {
	// Create connection pool
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify database connectivity
	if err := pool.Ping(ctx); err != nil {
		// Clean up the pool if ping fails
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	provider := &Provider{
		pool: pool,
	}

	// Initialize database schema
	if err := provider.setupSchema(ctx); err != nil {
		// Clean up on failure
		pool.Close()
		return nil, fmt.Errorf("failed to setup schema: %w", err)
	}

	return provider, nil
}

// setupSchema initializes the database schema
func (p *Provider) setupSchema(ctx context.Context) error {
	// Enable pgvector extension
	if _, err := p.pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("failed to create vector extension: %w", err)
	}

	// Create the vectors table
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS pcas_vectors (
			id TEXT PRIMARY KEY,
			embedding VECTOR(3072) NOT NULL,
			metadata JSONB,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)
	`
	if _, err := p.pool.Exec(ctx, createTableSQL); err != nil {
		return fmt.Errorf("failed to create vectors table: %w", err)
	}

	return nil
}

// StoreEmbedding stores a vector embedding for an event
func (p *Provider) StoreEmbedding(ctx context.Context, eventID string, embedding []float32, metadata map[string]string) error {
	// Convert embedding to pgvector type
	vec := pgvector.NewVector(embedding)

	// Convert metadata to JSON
	var metadataJSON []byte
	var err error
	if metadata != nil && len(metadata) > 0 {
		metadataJSON, err = json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}

	// Upsert the embedding
	upsertSQL := `
		INSERT INTO pcas_vectors (id, embedding, metadata)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE
		SET embedding = EXCLUDED.embedding, 
		    metadata = EXCLUDED.metadata,
		    created_at = NOW()
	`
	
	_, err = p.pool.Exec(ctx, upsertSQL, eventID, vec, metadataJSON)
	if err != nil {
		return fmt.Errorf("failed to store embedding: %w", err)
	}

	return nil
}

// QuerySimilar finds the most similar events based on vector similarity
func (p *Provider) QuerySimilar(ctx context.Context, queryEmbedding []float32, topK int) ([]storage.QueryResult, error) {
	// Convert query embedding to pgvector type
	queryVec := pgvector.NewVector(queryEmbedding)

	// Query for similar vectors using cosine distance
	// The <=> operator returns cosine distance (0 = identical, 2 = opposite)
	// We convert it to similarity score (1 = identical, -1 = opposite)
	querySQL := `
		SELECT id, 1 - (embedding <=> $1) AS similarity_score
		FROM pcas_vectors 
		ORDER BY embedding <=> $1 
		LIMIT $2
	`

	rows, err := p.pool.Query(ctx, querySQL, queryVec, topK)
	if err != nil {
		return nil, fmt.Errorf("failed to query similar vectors: %w", err)
	}
	defer rows.Close()

	// Collect the results
	var results []storage.QueryResult
	for rows.Next() {
		var result storage.QueryResult
		if err := rows.Scan(&result.ID, &result.Score); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		results = append(results, result)
	}

	// Check for any errors during iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return results, nil
}

// Close gracefully shuts down the vector storage connection
func (p *Provider) Close() error {
	p.pool.Close()
	return nil
}

// Ensure Provider implements VectorStorage interface
var _ storage.VectorStorage = (*Provider)(nil)