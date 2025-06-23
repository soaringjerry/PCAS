package cmd

import (
	"fmt"
	"log"
	"net"
	"os"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	
	"github.com/soaringjerry/pcas/internal/bus"
	"github.com/soaringjerry/pcas/internal/policy"
	"github.com/soaringjerry/pcas/internal/providers"
	"github.com/soaringjerry/pcas/internal/providers/mock"
	"github.com/soaringjerry/pcas/internal/providers/openai"
	busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
)

var (
	serverHost string
	serverPort string
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
	
	// Build the listen address from host and port
	listenAddr := fmt.Sprintf("%s:%s", serverHost, serverPort)
	
	// Listen on the configured address
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", listenAddr, err)
	}

	// Create gRPC server
	grpcServer := grpc.NewServer()
	
	// Create and register our bus service with policy engine and providers
	busServer := bus.NewServer(policyEngine, providerMap)
	busv1.RegisterEventBusServiceServer(grpcServer, busServer)
	
	log.Printf("PCAS server starting on %s...", listenAddr)
	
	// Start serving
	if err := grpcServer.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve: %v", err)
	}
	
	return nil
}

func init() {
	rootCmd.AddCommand(serveCmd)
	
	// Add flags
	serveCmd.Flags().StringVar(&serverHost, "host", "", "Host to bind the server to (default: all interfaces)")
	serveCmd.Flags().StringVar(&serverPort, "port", "50051", "Port to bind the server to")
}