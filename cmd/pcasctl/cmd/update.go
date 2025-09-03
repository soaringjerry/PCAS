package cmd

import (
    "fmt"
    "log"
    "os"
    "os/exec"
    
    "github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
    Use:   "update",
    Short: "Update PCAS from source and/or pull latest Docker image",
    Long:  "Offers to rebuild local binaries from source and pull the latest PCAS Docker image from GHCR.",
    RunE: func(cmd *cobra.Command, args []string) error {
        return runUpdate()
    },
}

func init() {
    rootCmd.AddCommand(updateCmd)
}

func runUpdate() error {
    // 1) Update from source (optional)
    if askYesNo("Rebuild local binaries from source?", true) {
        // Optional: git pull if repo present
        if _, err := os.Stat(".git"); err == nil {
            if askYesNo("Git repository detected. Run 'git pull' first?", true) {
                if err := runCmd("git", "pull"); err != nil {
                    log.Printf("git pull failed: %v", err)
                }
            }
        }
        if err := runCmd("go", "build", "-o", "pcas", "./cmd/pcas"); err != nil {
            return fmt.Errorf("failed to build pcas: %w", err)
        }
        if err := runCmd("go", "build", "-o", "pcasctl", "./cmd/pcasctl"); err != nil {
            return fmt.Errorf("failed to build pcasctl: %w", err)
        }
        fmt.Println("Built binaries: ./pcas and ./pcasctl")
    }

    // 2) Update Docker image (optional)
    if _, err := exec.LookPath("docker"); err == nil {
        if askYesNo("Pull latest Docker image ghcr.io/soaringjerry/pcas:latest?", true) {
            if err := runCmd("docker", "pull", "ghcr.io/soaringjerry/pcas:latest"); err != nil {
                log.Printf("docker pull failed: %v", err)
            }
        }
    } else {
        log.Printf("Docker not found in PATH; skipping image update")
    }

    fmt.Println("Update complete. You can run: ./pcas serve --port 50051")
    return nil
}

func runCmd(name string, args ...string) error {
    cmd := exec.Command(name, args...)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Run()
}
