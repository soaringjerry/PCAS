package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"
	
	_ "modernc.org/sqlite"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"github.com/coder/hnsw"
	
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
	"github.com/soaringjerry/pcas/internal/storage"
)

// Provider implements the Storage interface using SQLite
type Provider struct {
	db        *sql.DB
	hnswIndex *hnsw.Graph[string] // Using string as key type for event IDs
	indexPath string              // Path to persist the HNSW index
	indexMu   sync.RWMutex        // Mutex to protect concurrent access to the index
}

// NewProvider creates a new SQLite storage provider
func NewProvider(path string) (storage.Storage, error) {
	// modernc.org/sqlite uses standard connection string
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}
	
	// Derive HNSW index path from database path
	indexPath := strings.TrimSuffix(path, ".db") + ".hnsw"
	
	provider := &Provider{
		db:        db,
		indexPath: indexPath,
	}
	
	// Initialize the schema
	if err := provider.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}
	
	// Initialize HNSW index
	if err := provider.initHNSWIndex(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize HNSW index: %w", err)
	}
	
	return provider, nil
}

// initSchema creates the nodes and edges tables for the graph model
func (p *Provider) initSchema() error {
	// Create nodes table
	createNodesTableSQL := `
	CREATE TABLE IF NOT EXISTS nodes (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		content TEXT, -- Used for storing JSON-serialized events or binary-serialized vectors
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	
	if _, err := p.db.Exec(createNodesTableSQL); err != nil {
		return fmt.Errorf("failed to create nodes table: %w", err)
	}
	
	// Create edges table
	createEdgesTableSQL := `
	CREATE TABLE IF NOT EXISTS edges (
		id TEXT PRIMARY KEY,
		source_node_id TEXT NOT NULL,
		target_node_id TEXT NOT NULL,
		label TEXT NOT NULL, -- Relationship type, e.g., "embedding_of"
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (source_node_id) REFERENCES nodes(id),
		FOREIGN KEY (target_node_id) REFERENCES nodes(id)
	);
	`
	
	if _, err := p.db.Exec(createEdgesTableSQL); err != nil {
		return fmt.Errorf("failed to create edges table: %w", err)
	}
	
	// Create indexes for nodes
	nodesIndexSQL := `
	CREATE INDEX IF NOT EXISTS idx_nodes_type ON nodes(type);
	CREATE INDEX IF NOT EXISTS idx_nodes_created_at ON nodes(created_at);
	`
	
	if _, err := p.db.Exec(nodesIndexSQL); err != nil {
		return fmt.Errorf("failed to create nodes indexes: %w", err)
	}
	
	// Create indexes for edges
	edgesIndexSQL := `
	CREATE INDEX IF NOT EXISTS idx_edges_source_node_id ON edges(source_node_id);
	CREATE INDEX IF NOT EXISTS idx_edges_target_node_id ON edges(target_node_id);
	CREATE INDEX IF NOT EXISTS idx_edges_label ON edges(label);
	CREATE INDEX IF NOT EXISTS idx_edges_source_label ON edges(source_node_id, label);
	`
	
	if _, err := p.db.Exec(edgesIndexSQL); err != nil {
		return fmt.Errorf("failed to create edges indexes: %w", err)
	}
	
	return nil
}

// initHNSWIndex initializes the HNSW index by loading from disk or rebuilding from database
func (p *Provider) initHNSWIndex() error {
	// For in-memory databases, skip trying to load from disk
	if p.indexPath == ".hnsw" || strings.HasPrefix(p.indexPath, ":memory:") {
		// Create a new HNSW index directly
		p.hnswIndex = hnsw.NewGraph[string]()
		p.hnswIndex.M = 16
		p.hnswIndex.EfSearch = 200
		p.hnswIndex.Distance = hnsw.CosineDistance
		return nil
	}
	
	// First, create a new HNSW index
	p.hnswIndex = hnsw.NewGraph[string]()
	p.hnswIndex.M = 16
	p.hnswIndex.EfSearch = 200
	p.hnswIndex.Distance = hnsw.CosineDistance
	
	// Try to load existing index from disk
	savedGraph, err := hnsw.LoadSavedGraph[string](p.indexPath)
	if err == nil {
		// Successfully loaded existing index
		p.hnswIndex = savedGraph.Graph
		log.Printf("Successfully loaded HNSW index from %s", p.indexPath)
		return nil
	}
	
	log.Printf("No existing HNSW index found at %s, rebuilding from database", p.indexPath)
	
	// Create a new SavedGraph for persisting later
	newSavedGraph := &hnsw.SavedGraph[string]{
		Path:  p.indexPath,
		Graph: p.hnswIndex,
	}
	
	// Index doesn't exist or failed to load, rebuild from database
	// Query vector nodes from the nodes table
	rows, err := p.db.Query("SELECT id, content FROM nodes WHERE type = 'vector'")
	if err != nil {
		return fmt.Errorf("failed to query vector nodes: %w", err)
	}
	defer rows.Close()
	
	var rebuildCount int
	for rows.Next() {
		var id string
		var contentBlob []byte
		if err := rows.Scan(&id, &contentBlob); err != nil {
			return fmt.Errorf("failed to scan vector node: %w", err)
		}
		
		// Deserialize embedding
		embedding := deserializeVector(contentBlob)
		if len(embedding) > 0 {
			node := hnsw.MakeNode(id, embedding)
			p.hnswIndex.Add(node)
			rebuildCount++
		}
	}
	
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate embeddings: %w", err)
	}
	
	log.Printf("Rebuilt HNSW index with %d vectors", rebuildCount)
	
	// Save the rebuilt index
	if rebuildCount > 0 {
		if err := newSavedGraph.Save(); err != nil {
			// Non-fatal: we can continue without persisting
			log.Printf("[ERROR] Failed to save HNSW index: %v", err)
		} else {
			log.Printf("Successfully saved rebuilt HNSW index to %s", p.indexPath)
		}
	}
	
	return nil
}

// serializeVector converts a float32 slice to bytes
func serializeVector(vec []float32) []byte {
	if len(vec) == 0 {
		return nil
	}
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		bits := math.Float32bits(v)
		binary.LittleEndian.PutUint32(buf[i*4:], bits)
	}
	return buf
}

// deserializeVector converts bytes back to a float32 slice
func deserializeVector(data []byte) []float32 {
	if len(data) == 0 || len(data)%4 != 0 {
		return nil
	}
	vec := make([]float32, len(data)/4)
	for i := range vec {
		bits := binary.LittleEndian.Uint32(data[i*4:])
		vec[i] = math.Float32frombits(bits)
	}
	return vec
}

// StoreEvent persists an event as a node in the graph database
func (p *Provider) StoreEvent(ctx context.Context, event *eventsv1.Event, embedding []float32) error {
	// Create a map to store all event fields
	eventMap := map[string]interface{}{
		"id":          event.Id,
		"type":        event.Type,
		"source":      event.Source,
		"specversion": event.Specversion,
	}
	
	// Add optional fields
	if event.Subject != "" {
		eventMap["subject"] = event.Subject
	}
	if event.Time != nil {
		eventMap["time"] = event.Time.AsTime().Format(time.RFC3339)
	}
	if event.TraceId != "" {
		eventMap["trace_id"] = event.TraceId
	}
	if event.CorrelationId != "" {
		eventMap["correlation_id"] = event.CorrelationId
	}
	if event.UserId != "" {
		eventMap["user_id"] = event.UserId
	}
	if event.SessionId != "" {
		eventMap["session_id"] = event.SessionId
	}
	
	// Handle event data
	if event.Data != nil {
		// Try to unmarshal as structpb.Value
		value := &structpb.Value{}
		if event.Data.MessageIs(value) {
			if err := event.Data.UnmarshalTo(value); err == nil {
				eventMap["data"] = value.AsInterface()
			}
		}
		// If not a Value or failed to unmarshal, store the type URL
		if _, ok := eventMap["data"]; !ok {
			eventMap["data"] = map[string]string{"_type": event.Data.TypeUrl}
		}
	}
	
	// Serialize the event map to JSON
	eventJSON, err := json.Marshal(eventMap)
	if err != nil {
		return fmt.Errorf("failed to serialize event: %w", err)
	}
	
	// Insert the event as a node
	query := `INSERT INTO nodes (id, type, content) VALUES (?, ?, ?)`
	_, err = p.db.ExecContext(ctx, query, event.Id, "event", string(eventJSON))
	if err != nil {
		return fmt.Errorf("failed to store event node: %w", err)
	}
	
	// If embedding is provided, store it separately
	if embedding != nil && len(embedding) > 0 {
		vectorID, err := p.StoreVector(ctx, embedding)
		if err != nil {
			return fmt.Errorf("failed to store vector: %w", err)
		}
		
		// Create edge linking vector to event
		err = p.CreateEdge(ctx, vectorID, event.Id, "embedding_of")
		if err != nil {
			return fmt.Errorf("failed to create edge: %w", err)
		}
	}
	
	return nil
}

// StoreVector stores a vector as a node and returns its ID
func (p *Provider) StoreVector(ctx context.Context, vector []float32) (string, error) {
	// Generate a unique ID for the vector node
	vectorID := fmt.Sprintf("vec_%d_%d", time.Now().UnixNano(), len(vector))
	
	// Serialize the vector to bytes
	vectorBytes := serializeVector(vector)
	
	// Insert the vector as a node
	query := `INSERT INTO nodes (id, type, content) VALUES (?, ?, ?)`
	_, err := p.db.ExecContext(ctx, query, vectorID, "vector", vectorBytes)
	if err != nil {
		return "", fmt.Errorf("failed to store vector node: %w", err)
	}
	
	// Add to HNSW index
	p.indexMu.Lock()
	defer p.indexMu.Unlock()
	
	node := hnsw.MakeNode(vectorID, vector)
	p.hnswIndex.Add(node)
	
	return vectorID, nil
}

// CreateEdge creates an edge between two nodes
func (p *Provider) CreateEdge(ctx context.Context, sourceID, targetID, label string) error {
	// Generate a unique ID for the edge
	edgeID := fmt.Sprintf("edge_%s_%s_%d", sourceID, targetID, time.Now().UnixNano())
	
	// Insert the edge
	query := `INSERT INTO edges (id, source_node_id, target_node_id, label) VALUES (?, ?, ?, ?)`
	_, err := p.db.ExecContext(ctx, query, edgeID, sourceID, targetID, label)
	if err != nil {
		return fmt.Errorf("failed to create edge: %w", err)
	}
	
	return nil
}

// AddEmbeddingToEvent adds an embedding to an existing event
func (p *Provider) AddEmbeddingToEvent(ctx context.Context, eventID string, embedding []float32) error {
	// First, verify the event exists
	var exists bool
	err := p.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM nodes WHERE id = ? AND type = 'event')", eventID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check event existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("event with ID %s not found", eventID)
	}
	
	// Store the vector as a new node
	vectorID, err := p.StoreVector(ctx, embedding)
	if err != nil {
		return fmt.Errorf("failed to store vector: %w", err)
	}
	
	// Create edge linking vector to event
	err = p.CreateEdge(ctx, vectorID, eventID, "embedding_of")
	if err != nil {
		return fmt.Errorf("failed to create edge: %w", err)
	}
	
	return nil
}

// GetEventByID retrieves a single event by its ID
func (p *Provider) GetEventByID(ctx context.Context, eventID string) (*eventsv1.Event, error) {
	query := `
		SELECT content
		FROM nodes
		WHERE id = ? AND type = 'event'
	`
	
	var content string
	err := p.db.QueryRowContext(ctx, query, eventID).Scan(&content)
	
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("event not found: %s", eventID)
	} else if err != nil {
		return nil, fmt.Errorf("failed to retrieve event: %w", err)
	}
	
	// Deserialize the JSON content
	var eventMap map[string]interface{}
	if err := json.Unmarshal([]byte(content), &eventMap); err != nil {
		return nil, fmt.Errorf("failed to deserialize event: %w", err)
	}
	
	// Reconstruct the event
	event := &eventsv1.Event{}
	
	// Required fields
	if id, ok := eventMap["id"].(string); ok {
		event.Id = id
	}
	if typ, ok := eventMap["type"].(string); ok {
		event.Type = typ
	}
	if source, ok := eventMap["source"].(string); ok {
		event.Source = source
	}
	if specversion, ok := eventMap["specversion"].(string); ok {
		event.Specversion = specversion
	}
	
	// Optional fields
	if subject, ok := eventMap["subject"].(string); ok {
		event.Subject = subject
	}
	if traceId, ok := eventMap["trace_id"].(string); ok {
		event.TraceId = traceId
	}
	if correlationId, ok := eventMap["correlation_id"].(string); ok {
		event.CorrelationId = correlationId
	}
	if userId, ok := eventMap["user_id"].(string); ok {
		event.UserId = userId
	}
	if sessionId, ok := eventMap["session_id"].(string); ok {
		event.SessionId = sessionId
	}
	
	// Parse time
	if timeStr, ok := eventMap["time"].(string); ok {
		if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
			event.Time = timestamppb.New(t)
		}
	}
	
	// Parse data
	if data, ok := eventMap["data"]; ok {
		// Convert to structpb.Value and then to Any
		if value, err := structpb.NewValue(data); err == nil {
			if anyData, err := anypb.New(value); err == nil {
				event.Data = anyData
			}
		}
	}
	
	return event, nil
}

// BatchGetEvents retrieves multiple events by their IDs in a single query
func (p *Provider) BatchGetEvents(ctx context.Context, ids []string) ([]*eventsv1.Event, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	
	// Build placeholders for SQL IN clause
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	
	query := fmt.Sprintf(`
		SELECT id, content
		FROM nodes
		WHERE id IN (%s) AND type = 'event'
	`, strings.Join(placeholders, ","))
	
	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()
	
	events := make([]*eventsv1.Event, 0, len(ids))
	
	for rows.Next() {
		var id string
		var content string
		
		if err := rows.Scan(&id, &content); err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}
		
		// Deserialize the JSON content
		var eventMap map[string]interface{}
		if err := json.Unmarshal([]byte(content), &eventMap); err != nil {
			continue // Skip malformed events
		}
		
		// Reconstruct the event
		event := &eventsv1.Event{}
		
		// Required fields
		if id, ok := eventMap["id"].(string); ok {
			event.Id = id
		}
		if typ, ok := eventMap["type"].(string); ok {
			event.Type = typ
		}
		if source, ok := eventMap["source"].(string); ok {
			event.Source = source
		}
		if specversion, ok := eventMap["specversion"].(string); ok {
			event.Specversion = specversion
		}
		
		// Optional fields
		if subject, ok := eventMap["subject"].(string); ok {
			event.Subject = subject
		}
		if traceId, ok := eventMap["trace_id"].(string); ok {
			event.TraceId = traceId
		}
		if correlationId, ok := eventMap["correlation_id"].(string); ok {
			event.CorrelationId = correlationId
		}
		if userId, ok := eventMap["user_id"].(string); ok {
			event.UserId = userId
		}
		if sessionId, ok := eventMap["session_id"].(string); ok {
			event.SessionId = sessionId
		}
		
		// Parse time
		if timeStr, ok := eventMap["time"].(string); ok {
			if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
				event.Time = timestamppb.New(t)
			}
		}
		
		// Parse data
		if data, ok := eventMap["data"]; ok {
			// Convert to structpb.Value and then to Any
			if value, err := structpb.NewValue(data); err == nil {
				if anyData, err := anypb.New(value); err == nil {
					event.Data = anyData
				}
			}
		}
		
		events = append(events, event)
	}
	
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}
	
	return events, nil
}

// GetAllEvents retrieves all events with pagination support
func (p *Provider) GetAllEvents(ctx context.Context, offset, limit int) ([]*eventsv1.Event, error) {
	query := `
		SELECT id, content
		FROM nodes
		WHERE type = 'event'
		ORDER BY created_at ASC
		LIMIT ? OFFSET ?
	`
	
	rows, err := p.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()
	
	events := make([]*eventsv1.Event, 0, limit)
	
	for rows.Next() {
		var id string
		var content string
		
		if err := rows.Scan(&id, &content); err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}
		
		// Deserialize the JSON content
		var eventMap map[string]interface{}
		if err := json.Unmarshal([]byte(content), &eventMap); err != nil {
			continue // Skip malformed events
		}
		
		// Reconstruct the event
		event := &eventsv1.Event{}
		
		// Required fields
		if id, ok := eventMap["id"].(string); ok {
			event.Id = id
		}
		if typ, ok := eventMap["type"].(string); ok {
			event.Type = typ
		}
		if source, ok := eventMap["source"].(string); ok {
			event.Source = source
		}
		if specversion, ok := eventMap["specversion"].(string); ok {
			event.Specversion = specversion
		}
		
		// Optional fields
		if subject, ok := eventMap["subject"].(string); ok {
			event.Subject = subject
		}
		if traceId, ok := eventMap["trace_id"].(string); ok {
			event.TraceId = traceId
		}
		if correlationId, ok := eventMap["correlation_id"].(string); ok {
			event.CorrelationId = correlationId
		}
		if userId, ok := eventMap["user_id"].(string); ok {
			event.UserId = userId
		}
		if sessionId, ok := eventMap["session_id"].(string); ok {
			event.SessionId = sessionId
		}
		
		// Parse time
		if timeStr, ok := eventMap["time"].(string); ok {
			if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
				event.Time = timestamppb.New(t)
			}
		}
		
		// Parse data
		if data, ok := eventMap["data"]; ok {
			// Convert to structpb.Value and then to Any
			if value, err := structpb.NewValue(data); err == nil {
				if anyData, err := anypb.New(value); err == nil {
					event.Data = anyData
				}
			}
		}
		
		events = append(events, event)
	}
	
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}
	
	return events, nil
}

// QuerySimilar finds the most similar events based on vector similarity
func (p *Provider) QuerySimilar(ctx context.Context, embedding []float32, topK int, filter *storage.Filter) ([]storage.QueryResult, error) {
	if embedding == nil || len(embedding) == 0 {
		return nil, fmt.Errorf("embedding cannot be empty")
	}
	
	if topK <= 0 {
		return nil, fmt.Errorf("topK must be positive")
	}
	
	// If filter is provided, we need to first find eligible event nodes
	var eligibleEventIDs map[string]bool
	if filter != nil {
		var err error
		eligibleEventIDs, err = p.findFilteredEventIDs(ctx, filter)
		if err != nil {
			return nil, fmt.Errorf("failed to filter events: %w", err)
		}
		if len(eligibleEventIDs) == 0 {
			// No events match the filter
			return []storage.QueryResult{}, nil
		}
	}
	
	// Lock for reading
	p.indexMu.RLock()
	defer p.indexMu.RUnlock()
	
	// If we have a filter, we need to find vectors associated with eligible events
	var vectorNodes []hnsw.Node[string]
	if filter != nil {
		// Query to find vector nodes associated with filtered events
		eventIDs := make([]string, 0, len(eligibleEventIDs))
		for id := range eligibleEventIDs {
			eventIDs = append(eventIDs, id)
		}
		
		placeholders := make([]string, len(eventIDs))
		args := make([]interface{}, len(eventIDs))
		for i, id := range eventIDs {
			placeholders[i] = "?"
			args[i] = id
		}
		
		query := fmt.Sprintf(`
			SELECT source_node_id
			FROM edges
			WHERE target_node_id IN (%s) AND label = 'embedding_of'
		`, strings.Join(placeholders, ","))
		
		rows, err := p.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to query vector nodes: %w", err)
		}
		defer rows.Close()
		
		vectorIDs := make([]string, 0)
		for rows.Next() {
			var vectorID string
			if err := rows.Scan(&vectorID); err != nil {
				continue
			}
			vectorIDs = append(vectorIDs, vectorID)
		}
		
		// Search all vectors, then filter results
		// Note: This is not optimal but HNSW doesn't support filtered search
		// In production, consider using a specialized vector DB with filtering support
		allNodes := p.hnswIndex.Search(embedding, topK*10) // Search more to account for filtering
		
		// Filter to only include vectors associated with eligible events
		vectorIDSet := make(map[string]bool)
		for _, id := range vectorIDs {
			vectorIDSet[id] = true
		}
		
		vectorNodes = make([]hnsw.Node[string], 0, topK)
		for _, node := range allNodes {
			if vectorIDSet[node.Key] {
				vectorNodes = append(vectorNodes, node)
				if len(vectorNodes) >= topK {
					break
				}
			}
		}
	} else {
		// No filter, search all vectors
		vectorNodes = p.hnswIndex.Search(embedding, topK)
	}
	
	// Collect vector node IDs and scores
	vectorIDs := make([]string, len(vectorNodes))
	scoreMap := make(map[string]float32)
	
	for i, node := range vectorNodes {
		vectorIDs[i] = node.Key
		// HNSW returns distance, convert to similarity score
		// For cosine distance, similarity = 1 - distance
		scoreMap[node.Key] = 1.0 - hnsw.CosineDistance(embedding, node.Value)
	}
	
	if len(vectorIDs) == 0 {
		return []storage.QueryResult{}, nil
	}
	
	// Query edges to find event nodes
	placeholders := make([]string, len(vectorIDs))
	args := make([]interface{}, len(vectorIDs))
	for i, id := range vectorIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	
	query := fmt.Sprintf(`
		SELECT source_node_id, target_node_id
		FROM edges
		WHERE source_node_id IN (%s) AND label = 'embedding_of'
	`, strings.Join(placeholders, ","))
	
	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query edges: %w", err)
	}
	defer rows.Close()
	
	// Build results
	results := make([]storage.QueryResult, 0, topK)
	for rows.Next() {
		var vectorID, eventID string
		if err := rows.Scan(&vectorID, &eventID); err != nil {
			continue
		}
		
		if score, ok := scoreMap[vectorID]; ok {
			results = append(results, storage.QueryResult{
				ID:    eventID,
				Score: score,
			})
		}
	}
	
	return results, nil
}

// findFilteredEventIDs finds event node IDs that match the given filter
func (p *Provider) findFilteredEventIDs(ctx context.Context, filter *storage.Filter) (map[string]bool, error) {
	var whereConditions []string
	var args []interface{}
	
	// Build WHERE conditions based on filter
	if filter.UserID != nil {
		whereConditions = append(whereConditions, "json_extract(content, '$.user_id') = ?")
		args = append(args, *filter.UserID)
	}
	
	if filter.SessionID != nil {
		whereConditions = append(whereConditions, "json_extract(content, '$.session_id') = ?")
		args = append(args, *filter.SessionID)
	}
	
	if len(filter.EventTypes) > 0 {
		placeholders := make([]string, len(filter.EventTypes))
		for i, eventType := range filter.EventTypes {
			placeholders[i] = "?"
			args = append(args, eventType)
		}
		whereConditions = append(whereConditions, fmt.Sprintf("json_extract(content, '$.type') IN (%s)", strings.Join(placeholders, ",")))
	}
	
	if filter.TimeFrom != nil {
		whereConditions = append(whereConditions, "datetime(json_extract(content, '$.time')) >= datetime(?)")
		args = append(args, filter.TimeFrom.Format(time.RFC3339))
	}
	
	if filter.TimeTo != nil {
		whereConditions = append(whereConditions, "datetime(json_extract(content, '$.time')) <= datetime(?)")
		args = append(args, filter.TimeTo.Format(time.RFC3339))
	}
	
	// Handle attribute filters
	if len(filter.AttributeFilters) > 0 {
		for key, value := range filter.AttributeFilters {
			// Use json_extract to query nested attributes in the content JSON
			// The key might contain dots for nested attributes (e.g., "course.id")
			jsonPath := fmt.Sprintf("$.attributes.\"%s\"", key)
			whereConditions = append(whereConditions, fmt.Sprintf("json_extract(content, '%s') = ?", jsonPath))
			args = append(args, value)
		}
	}
	
	// Build query
	query := "SELECT id FROM nodes WHERE type = 'event'"
	if len(whereConditions) > 0 {
		query += " AND " + strings.Join(whereConditions, " AND ")
	}
	
	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query filtered events: %w", err)
	}
	defer rows.Close()
	
	eligibleIDs := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		eligibleIDs[id] = true
	}
	
	return eligibleIDs, nil
}

// Close closes the database connection
func (p *Provider) Close() error {
	// Save HNSW index before closing
	p.indexMu.Lock()
	defer p.indexMu.Unlock()
	
	if p.hnswIndex != nil && p.indexPath != ".hnsw" && !strings.HasPrefix(p.indexPath, ":memory:") {
		log.Printf("Saving HNSW index to %s before closing...", p.indexPath)
		savedGraph := &hnsw.SavedGraph[string]{
			Path:  p.indexPath,
			Graph: p.hnswIndex,
		}
		
		if err := savedGraph.Save(); err != nil {
			// Log the error but don't fail the close operation
			log.Printf("[ERROR] Failed to save HNSW index on close: %v", err)
		} else {
			log.Printf("Successfully saved HNSW index")
		}
	}
	
	return p.db.Close()
}