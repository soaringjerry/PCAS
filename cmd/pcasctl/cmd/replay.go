package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	
	busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

var (
	replayServerPort string
	replayServerAddr string
	replayDBPath     string
)

var replayCmd = &cobra.Command{
	Use:   "replay [event-id]",
	Short: "Replay an event from the database",
	Long: `Replay retrieves a historical event from the database and re-publishes it 
to the event bus. This is useful for testing, debugging, and understanding 
the causality chain of events.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eventID := args[0]
		return replayEvent(eventID)
	},
}

func replayEvent(eventID string) error {
	// Connect to SQLite database
	db, err := sql.Open("sqlite3", replayDBPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer db.Close()

	// Query the event from database
	var id, eventType, source, subject, specversion string
	var traceID, correlationID sql.NullString
	var eventTime time.Time
	var dataJSON sql.NullString

	query := `SELECT id, type, source, subject, specversion, time, data, trace_id, correlation_id 
			  FROM events WHERE id = ?`
	err = db.QueryRow(query, eventID).Scan(
		&id, &eventType, &source, &subject, &specversion,
		&eventTime, &dataJSON, &traceID, &correlationID,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("event not found: %s", eventID)
		}
		return fmt.Errorf("failed to query event: %v", err)
	}

	// Reconstruct the event
	event := &eventsv1.Event{
		Id:          id,
		Type:        eventType,
		Source:      source,
		Subject:     subject,
		Specversion: specversion,
		Time:        timestamppb.New(eventTime),
	}

	// Set trace ID if present
	if traceID.Valid {
		event.TraceId = traceID.String
	}

	// Set correlation ID if present
	if correlationID.Valid {
		event.CorrelationId = correlationID.String
	}

	// Reconstruct data if present
	if dataJSON.Valid && dataJSON.String != "" {
		var data interface{}
		if err := json.Unmarshal([]byte(dataJSON.String), &data); err == nil {
			value, err := structpb.NewValue(data)
			if err == nil {
				event.Data, _ = anypb.New(value)
			}
		}
	}

	// Connect to PCAS server
	if replayServerAddr == "" {
		replayServerAddr = fmt.Sprintf("localhost:%s", replayServerPort)
	}

	conn, err := grpc.NewClient(replayServerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Create the client
	client := busv1.NewEventBusServiceClient(conn)

	// Publish the replayed event
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.Publish(ctx, event)
	if err != nil {
		return fmt.Errorf("failed to publish replayed event: %v", err)
	}

	log.Printf("Event replayed successfully:")
	log.Printf("  ID: %s", event.Id)
	log.Printf("  Type: %s", event.Type)
	log.Printf("  Source: %s", event.Source)
	if event.TraceId != "" {
		log.Printf("  Trace ID: %s", event.TraceId)
	}
	if event.CorrelationId != "" {
		log.Printf("  Correlation ID: %s", event.CorrelationId)
	}
	log.Printf("  Response: %+v", resp)

	return nil
}

func init() {
	rootCmd.AddCommand(replayCmd)

	// Add flags
	replayCmd.Flags().StringVar(&replayServerPort, "port", "50051", "PCAS server port")
	replayCmd.Flags().StringVar(&replayServerAddr, "server", "", "PCAS server address (overrides --port)")
	replayCmd.Flags().StringVar(&replayDBPath, "db", "pcas.db", "Path to the PCAS database")
}