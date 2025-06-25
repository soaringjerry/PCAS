package pcas

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
)

// Client provides a simple interface to interact with PCAS
type Client struct {
	conn       *grpc.ClientConn
	grpcClient busv1.EventBusServiceClient
}

// NewClient creates a new PCAS client
func NewClient(ctx context.Context, serverAddr string) (*Client, error) {
	// Establish gRPC connection
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PCAS server: %w", err)
	}

	// Create gRPC client
	grpcClient := busv1.NewEventBusServiceClient(conn)

	return &Client{
		conn:       conn,
		grpcClient: grpcClient,
	}, nil
}

// Close closes the client connection
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}