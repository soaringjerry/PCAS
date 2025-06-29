package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

var (
	topK int
	searchUserID string
)

// searchCmd represents the search command
var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search for events using natural language",
	Long: `Search for events in PCAS using semantic search.
	
Examples:
  pcasctl search "user login errors"
  pcasctl search "discussions about architecture" --top-k 10
  pcasctl search "recent deployments"`,
	Args: cobra.ExactArgs(1),
	RunE: runSearch,
}

func init() {
	rootCmd.AddCommand(searchCmd)
	searchCmd.Flags().IntVar(&topK, "top-k", 5, "Number of top results to return")
	searchCmd.Flags().StringVar(&serverPort, "port", "50051", "PCAS server port")
	searchCmd.Flags().StringVar(&serverAddr, "server", "", "PCAS server address (overrides --port)")
	searchCmd.Flags().StringVar(&searchUserID, "user-id", "", "User ID to filter results by (optional)")
}

func runSearch(cmd *cobra.Command, args []string) error {
	// Use timeout context for search operations
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	queryText := args[0]

	// Determine server address
	if serverAddr == "" {
		serverAddr = fmt.Sprintf("localhost:%s", serverPort)
	}

	// Connect to gRPC server
	conn, err := grpc.Dial(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer conn.Close()

	client := busv1.NewEventBusServiceClient(conn)

	// Create search request
	req := &busv1.SearchRequest{
		QueryText: queryText,
		TopK:      int32(topK),
		UserId:    searchUserID,
	}

	// Perform search
	resp, err := client.Search(ctx, req)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	// Display results
	if len(resp.Events) == 0 {
		fmt.Println("No matching events found.")
		return nil
	}

	fmt.Printf("Found %d matching events:\n\n", len(resp.Events))
	
	for i, event := range resp.Events {
		fmt.Printf("%d. Event ID: %s\n", i+1, event.Id)
		// Display similarity score if available
		if i < len(resp.Scores) {
			fmt.Printf("   Similarity Score: %.3f\n", resp.Scores[i])
		}
		fmt.Printf("   Type: %s\n", event.Type)
		fmt.Printf("   Source: %s\n", event.Source)
		if event.Subject != "" {
			fmt.Printf("   Subject: %s\n", event.Subject)
		}
		if event.Time != nil {
			fmt.Printf("   Time: %s\n", event.Time.AsTime().Format(time.RFC3339))
		}
		
		// Try to parse and display data
		if event.Data != nil {
			var data interface{}
			structData := &structpb.Value{}
			if event.Data.MessageIs(structData) {
				if err := event.Data.UnmarshalTo(structData); err == nil {
					data = structData.AsInterface()
				}
			} else {
				// Try to unmarshal as raw JSON
				if err := json.Unmarshal(event.Data.Value, &data); err == nil {
					// Successfully unmarshaled
				} else {
					data = string(event.Data.Value)
				}
			}
			
			if dataJSON, err := json.MarshalIndent(data, "   ", "  "); err == nil {
				fmt.Printf("   Data:\n   %s\n", dataJSON)
			} else {
				fmt.Printf("   Data: %v\n", data)
			}
		}
		
		if event.TraceId != "" {
			fmt.Printf("   Trace ID: %s\n", event.TraceId)
		}
		if event.CorrelationId != "" {
			fmt.Printf("   Correlation ID: %s\n", event.CorrelationId)
		}
		
		fmt.Println()
	}

	return nil
}