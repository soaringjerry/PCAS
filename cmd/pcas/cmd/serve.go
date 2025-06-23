package cmd

import (
	"fmt"
	"log"
	"net"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	
	"github.com/soaringjerry/pcas/internal/bus"
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
	// Build the listen address from host and port
	listenAddr := fmt.Sprintf("%s:%s", serverHost, serverPort)
	
	// Listen on the configured address
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", listenAddr, err)
	}

	// Create gRPC server
	grpcServer := grpc.NewServer()
	
	// Create and register our bus service
	busServer := bus.NewServer()
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