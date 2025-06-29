package cmd

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	
	"github.com/soaringjerry/pcas/internal/bus"
	"github.com/soaringjerry/pcas/internal/policy"
	"github.com/soaringjerry/pcas/internal/providers"
	"github.com/soaringjerry/pcas/internal/providers/mock"
	"github.com/soaringjerry/pcas/internal/providers/openai"
	"github.com/soaringjerry/pcas/internal/storage/sqlite"
	busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
)

var (
	serverHost string
	serverPort string
	dbPath     string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the PCAS server",
	Long: `Start the PCAS server to begin processing events and managing 
personal data with privacy and security at its core.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runServer()
	},
}

func runServer() error {
	// Create a channel to signal when shutdown is complete
	shutdownComplete := make(chan struct{})
	
	// Load policy from file
	log.Println("Loading policy from policy.yaml...")
	policyConfig, err := policy.LoadPolicy("policy.yaml")
	if err != nil {
		return fmt.Errorf("failed to load policy: %w", err)
	}
	
	// Create policy engine
	policyEngine := policy.NewEngine(policyConfig)
	
	// Initialize providers based on policy configuration
	providerMap := make(map[string]providers.ComputeProvider)
	for _, providerConfig := range policyConfig.Providers {
		switch providerConfig.Type {
		case "mock":
			providerMap[providerConfig.Name] = mock.NewProvider()
			log.Printf("Initialized provider: %s (type: %s)", providerConfig.Name, providerConfig.Type)
		case "openai":
			apiKey := os.Getenv("OPENAI_API_KEY")
			if apiKey == "" {
				log.Printf("Warning: Skipping provider %s - OPENAI_API_KEY environment variable not set", providerConfig.Name)
				continue
			}
			providerMap[providerConfig.Name] = openai.NewProvider(apiKey)
			log.Printf("Initialized provider: %s (type: %s)", providerConfig.Name, providerConfig.Type)
		default:
			log.Printf("Unknown provider type: %s", providerConfig.Type)
		}
	}
	
	// Ensure data directory exists
	if err := os.MkdirAll("data", 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}
	
	// Initialize SQLite storage (using pure Go implementation)
	log.Println("Initializing SQLite storage...")
	localStorage, err := sqlite.NewProvider(dbPath)
	if err != nil {
		return fmt.Errorf("failed to initialize SQLite storage: %w", err)
	}
	// Note: We'll close localStorage in the signal handler for graceful shutdown
	log.Println("SQLite storage initialized successfully")
	
	// Initialize embedding provider if OpenAI API key is available
	var embeddingProvider providers.EmbeddingProvider
	
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		// Initialize OpenAI embedding provider
		embeddingProvider = openai.NewEmbeddingProvider(apiKey)
		log.Println("OpenAI embedding provider initialized")
	} else {
		log.Println("OPENAI_API_KEY not set, skipping embedding provider initialization")
		log.Println("[WARNING] OPENAI_API_KEY not set. The server will start, but SEARCH and RAG functionalities will be DISABLED.")
	}
	
	// Build the listen address from host and port
	listenAddr := fmt.Sprintf("%s:%s", serverHost, serverPort)
	
	// Listen on the configured address
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", listenAddr, err)
	}

	// Create gRPC server
	grpcServer := grpc.NewServer()
	
	// Create and register our bus service with policy engine, providers and storage
	busServer := bus.NewServer(policyEngine, providerMap, localStorage)
	
	// Set embedding provider if available
	if embeddingProvider != nil {
		busServer.SetEmbeddingProvider(embeddingProvider)
	}
	
	busv1.RegisterEventBusServiceServer(grpcServer, busServer)
	
	log.Printf("PCAS server starting on %s...", listenAddr)
	
	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	// Start a goroutine to handle shutdown signals
	go func() {
		sig := <-sigChan
		log.Printf("Received signal %v, initiating graceful shutdown...", sig)
		
		// Gracefully stop the gRPC server
		log.Println("Stopping gRPC server...")
		grpcServer.GracefulStop()
		
		// NEW: Wait for all background tasks to complete
		log.Println("Waiting for background vectorization to complete...")
		busServer.WaitForVectorization()
		log.Println("All background tasks finished.")
		
		// NOW, it's safe to close storage
		log.Println("Closing storage (this will save the HNSW index)...")
		if err := localStorage.Close(); err != nil {
			log.Printf("[ERROR] Failed to close storage: %v", err)
		} else {
			log.Println("Storage closed successfully")
		}
		
		log.Println("Graceful shutdown complete")
		close(shutdownComplete)  // Signal that shutdown is complete
	}()
	
	// Start serving (this blocks until the server is stopped)
	if err := grpcServer.Serve(lis); err != nil {
		// grpcServer.Serve returns when the server is stopped
		// We still need to wait for the shutdown sequence to complete
		<-shutdownComplete
		return fmt.Errorf("failed to serve: %v", err)
	}
	
	// Wait for the shutdown goroutine to finish before exiting
	<-shutdownComplete
	log.Println("Server exited gracefully.")
	
	return nil
}

func init() {
	rootCmd.AddCommand(serveCmd)
	
	// Add flags
	serveCmd.Flags().StringVar(&serverHost, "host", "", "Host to bind the server to (default: all interfaces)")
	serveCmd.Flags().StringVar(&serverPort, "port", "50051", "Port to bind the server to")
	serveCmd.Flags().StringVar(&dbPath, "db-path", "pcas.db", "Path to the PCAS SQLite database file")
}