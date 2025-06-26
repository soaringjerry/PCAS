//go:build cgo && !no_cgo

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	
	_ "github.com/mattn/go-sqlite3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
	"github.com/soaringjerry/pcas/internal/storage"
)

// Provider implements the Storage interface using SQLite
type Provider struct {
	db *sql.DB
}

// NewProvider creates a new SQLite storage provider
func NewProvider(path string) (storage.Storage, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}
	
	provider := &Provider{db: db}
	
	// Initialize the schema
	if err := provider.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}
	
	return provider, nil
}

// initSchema creates the events table if it doesn't exist
func (p *Provider) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS events (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		source TEXT NOT NULL,
		subject TEXT,
		specversion TEXT NOT NULL,
		time DATETIME NOT NULL,
		data TEXT,
		trace_id TEXT,
		correlation_id TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE INDEX IF NOT EXISTS idx_events_type ON events(type);
	CREATE INDEX IF NOT EXISTS idx_events_time ON events(time);
	CREATE INDEX IF NOT EXISTS idx_events_source ON events(source);
	CREATE INDEX IF NOT EXISTS idx_events_trace_id ON events(trace_id);
	CREATE INDEX IF NOT EXISTS idx_events_correlation_id ON events(correlation_id);
	`
	
	_, err := p.db.Exec(schema)
	return err
}

// StoreEvent persists an event to the SQLite database
func (p *Provider) StoreEvent(ctx context.Context, event *eventsv1.Event) error {
	// Serialize event data to JSON if present
	var dataJSON string
	if event.Data != nil {
		// Try to unmarshal as structpb.Value
		value := &structpb.Value{}
		if event.Data.MessageIs(value) {
			if err := event.Data.UnmarshalTo(value); err == nil {
				// Convert to JSON
				jsonBytes, err := json.Marshal(value.AsInterface())
				if err == nil {
					dataJSON = string(jsonBytes)
				}
			}
		}
		// If not a Value or failed to unmarshal, store the type URL
		if dataJSON == "" {
			dataJSON = fmt.Sprintf(`{"_type": "%s"}`, event.Data.TypeUrl)
		}
	}
	
	// Convert timestamp
	eventTime := time.Now()
	if event.Time != nil {
		eventTime = event.Time.AsTime()
	}
	
	// Insert the event
	query := `
		INSERT INTO events (id, type, source, subject, specversion, time, data, trace_id, correlation_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	
	_, err := p.db.ExecContext(ctx, query,
		event.Id,
		event.Type,
		event.Source,
		event.Subject,
		event.Specversion,
		eventTime,
		dataJSON,
		event.TraceId,
		event.CorrelationId,
	)
	
	if err != nil {
		return fmt.Errorf("failed to store event: %w", err)
	}
	
	return nil
}

// GetEventByID retrieves a single event by its ID
func (p *Provider) GetEventByID(ctx context.Context, eventID string) (*eventsv1.Event, error) {
	query := `
		SELECT id, type, source, subject, specversion, time, data, trace_id, correlation_id
		FROM events
		WHERE id = ?
	`
	
	var event eventsv1.Event
	var eventTime time.Time
	var dataJSON sql.NullString
	var traceID sql.NullString
	var correlationID sql.NullString
	var subject sql.NullString
	
	err := p.db.QueryRowContext(ctx, query, eventID).Scan(
		&event.Id,
		&event.Type,
		&event.Source,
		&subject,
		&event.Specversion,
		&eventTime,
		&dataJSON,
		&traceID,
		&correlationID,
	)
	
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("event not found: %s", eventID)
	} else if err != nil {
		return nil, fmt.Errorf("failed to retrieve event: %w", err)
	}
	
	// Set optional fields
	if subject.Valid {
		event.Subject = subject.String
	}
	if traceID.Valid {
		event.TraceId = traceID.String
	}
	if correlationID.Valid {
		event.CorrelationId = correlationID.String
	}
	
	// Convert time
	event.Time = timestamppb.New(eventTime)
	
	// Parse data if present
	if dataJSON.Valid && dataJSON.String != "" {
		var data interface{}
		if err := json.Unmarshal([]byte(dataJSON.String), &data); err == nil {
			// Convert to structpb.Value and then to Any
			if value, err := structpb.NewValue(data); err == nil {
				if anyData, err := anypb.New(value); err == nil {
					event.Data = anyData
				}
			}
		}
	}
	
	return &event, nil
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
		SELECT id, type, source, subject, specversion, time, data, trace_id, correlation_id
		FROM events
		WHERE id IN (%s)
	`, strings.Join(placeholders, ","))
	
	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()
	
	events := make([]*eventsv1.Event, 0, len(ids))
	
	for rows.Next() {
		var event eventsv1.Event
		var eventTime time.Time
		var dataJSON sql.NullString
		var traceID sql.NullString
		var correlationID sql.NullString
		var subject sql.NullString
		
		err := rows.Scan(
			&event.Id,
			&event.Type,
			&event.Source,
			&subject,
			&event.Specversion,
			&eventTime,
			&dataJSON,
			&traceID,
			&correlationID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}
		
		// Set optional fields
		if subject.Valid {
			event.Subject = subject.String
		}
		if traceID.Valid {
			event.TraceId = traceID.String
		}
		if correlationID.Valid {
			event.CorrelationId = correlationID.String
		}
		
		// Convert time
		event.Time = timestamppb.New(eventTime)
		
		// Parse data if present
		if dataJSON.Valid && dataJSON.String != "" {
			var data interface{}
			if err := json.Unmarshal([]byte(dataJSON.String), &data); err == nil {
				// Convert to structpb.Value and then to Any
				if value, err := structpb.NewValue(data); err == nil {
					if anyData, err := anypb.New(value); err == nil {
						event.Data = anyData
					}
				}
			}
		}
		
		events = append(events, &event)
	}
	
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}
	
	return events, nil
}

// Close closes the database connection
func (p *Provider) Close() error {
	return p.db.Close()
}