package vectorpg

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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

	// Add generated columns for efficient filtering
	alterTableSQL := `
		ALTER TABLE pcas_vectors
		ADD COLUMN IF NOT EXISTS user_id text GENERATED ALWAYS AS (metadata->>'user_id') STORED,
		ADD COLUMN IF NOT EXISTS session_id text GENERATED ALWAYS AS (metadata->>'session_id') STORED,
		ADD COLUMN IF NOT EXISTS event_type text GENERATED ALWAYS AS (metadata->>'event_type') STORED,
		ADD COLUMN IF NOT EXISTS event_ts timestamptz GENERATED ALWAYS AS (to_timestamp((metadata->>'timestamp_unix')::bigint)) STORED
	`
	if _, err := p.pool.Exec(ctx, alterTableSQL); err != nil {
		return fmt.Errorf("failed to add generated columns: %w", err)
	}

	// Create composite B-tree index for user_id and event_type
	createCompositeIndexSQL := `
		CREATE INDEX IF NOT EXISTS idx_pcas_vectors_user_event_type 
		ON pcas_vectors USING btree (user_id, event_type)
	`
	if _, err := p.pool.Exec(ctx, createCompositeIndexSQL); err != nil {
		return fmt.Errorf("failed to create composite index: %w", err)
	}

	// Create BRIN index for event_ts (optimized for time-series data)
	createBrinIndexSQL := `
		CREATE INDEX IF NOT EXISTS idx_pcas_vectors_event_ts 
		ON pcas_vectors USING brin (event_ts)
	`
	if _, err := p.pool.Exec(ctx, createBrinIndexSQL); err != nil {
		return fmt.Errorf("failed to create BRIN index: %w", err)
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

// QuerySimilar finds the most similar events based on vector similarity with optional filtering
func (p *Provider) QuerySimilar(ctx context.Context, queryEmbedding []float32, topK int, filters map[string]interface{}) ([]storage.QueryResult, error) {
	// Convert query embedding to pgvector type
	queryVec := pgvector.NewVector(queryEmbedding)

	// Build the base query
	baseQuery := `
		SELECT id, 1 - (embedding <=> $1) AS similarity_score
		FROM pcas_vectors`
	
	// Build WHERE clause dynamically
	var whereConditions []string
	var args []interface{}
	args = append(args, queryVec) // $1 is the query vector
	
	argCounter := 2 // Start from $2 for filter parameters
	
	// Process filters
	if filters != nil && len(filters) > 0 {
		for key, value := range filters {
			// Validate that the key is one of our indexed columns
			switch key {
			case "user_id", "session_id", "event_type":
				whereConditions = append(whereConditions, fmt.Sprintf("%s = $%d", key, argCounter))
				args = append(args, value)
				argCounter++
			case "event_ts_after":
				whereConditions = append(whereConditions, fmt.Sprintf("event_ts >= $%d", argCounter))
				args = append(args, value)
				argCounter++
			case "event_ts_before":
				whereConditions = append(whereConditions, fmt.Sprintf("event_ts <= $%d", argCounter))
				args = append(args, value)
				argCounter++
			default:
				// Ignore unsupported filter keys
				continue
			}
		}
	}
	
	// Construct the full query
	fullQuery := baseQuery
	if len(whereConditions) > 0 {
		fullQuery += " WHERE " + strings.Join(whereConditions, " AND ")
	}
	fullQuery += " ORDER BY embedding <=> $1 LIMIT $" + fmt.Sprintf("%d", argCounter)
	args = append(args, topK)

	// Execute the query
	rows, err := p.pool.Query(ctx, fullQuery, args...)
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