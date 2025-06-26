// Package cmd implements the command-line interface for the PCAS server.
// It provides commands for starting and managing the PCAS service.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "pcas",
	Short: "PCAS a local-first, intelligent decision-making engine",
	Long: `PCAS (Personal Context Aware System) is a local-first, intelligent 
decision-making engine that processes events and manages personal data 
with privacy and security at its core.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}