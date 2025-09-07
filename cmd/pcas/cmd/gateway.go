package cmd

import (
    "log"
    "net/http"
    "os"

    "github.com/spf13/cobra"
)

var (
    gwAddr   string
    pcasAddr string
)

// gatewayCmd starts an OpenAI-compatible HTTP gateway with auto-RAG.
var gatewayCmd = &cobra.Command{
    Use:   "gateway",
    Short: "Start OpenAI-compatible gateway (auto-RAG)",
    RunE: func(cmd *cobra.Command, args []string) error {
        if gwAddr == "" {
            if v := os.Getenv("PCAS_GW_ADDR"); v != "" {
                gwAddr = v
            } else {
                gwAddr = "127.0.0.1:50052"
            }
        }
        if pcasAddr == "" {
            if v := os.Getenv("PCAS_ADDR"); v != "" {
                pcasAddr = v
            } else {
                pcasAddr = "127.0.0.1:50051"
            }
        }

        mux, err := newGatewayMux(pcasAddr)
        if err != nil {
            return err
        }
        log.Printf("PCAS gateway listening on http://%s (PCAS gRPC: %s)", gwAddr, pcasAddr)
        return http.ListenAndServe(gwAddr, mux)
    },
}

func init() {
    rootCmd.AddCommand(gatewayCmd)
    gatewayCmd.Flags().StringVar(&gwAddr, "addr", "", "HTTP listen addr (default 127.0.0.1:50052 or PCAS_GW_ADDR)")
    gatewayCmd.Flags().StringVar(&pcasAddr, "pcas", "", "PCAS gRPC addr (default 127.0.0.1:50051 or PCAS_ADDR)")
}

