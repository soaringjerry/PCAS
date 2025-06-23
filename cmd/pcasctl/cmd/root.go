package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "pcasctl",
	Short: "A CLI for interacting with the PCAS engine",
	Long: `pcasctl is a command-line interface tool for interacting with the 
PCAS (Personal Context Aware System) engine. It allows you to emit events, 
query data, and manage your personal context system.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}