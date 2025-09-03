package cmd

import (
    "bufio"
    "errors"
    "fmt"
    "log"
    "os"
    "os/exec"
    "path/filepath"
    "runtime"
    "strings"
    "time"
    
    "github.com/spf13/cobra"
)

// setupCmd provides an interactive wizard to configure and start PCAS
var setupCmd = &cobra.Command{
    Use:   "setup",
    Short: "Interactive setup wizard for PCAS",
    Long:  "Guides you through configuring environment, generating policy.yaml, and starting the PCAS server.",
    RunE: func(cmd *cobra.Command, args []string) error {
        return runSetup()
    },
}

func init() {
    rootCmd.AddCommand(setupCmd)
}

func runSetup() error {
    fmt.Println("PCAS Setup Wizard — let’s get you running in minutes ✨")

    // Working directory
    cwd, _ := os.Getwd()
    fmt.Printf("Working directory: %s\n", cwd)

    // 1) Choose host/port
    host := askInput("Server host (blank = all interfaces)", "")
    port := askInput("Server port", "50051")

    // 2) Choose providers
    enableOpenAI := askYesNo("Enable OpenAI provider (requires API key)?", false)
    openAIKey := ""
    if enableOpenAI {
        openAIKey = askInput("Enter OPENAI_API_KEY (leave blank to skip saving)", "")
    }

    // 3) Vector storage (Chroma)
    enableChroma := false
    chromaURL := ""
    if enableOpenAI {
        enableChroma = askYesNo("Enable vector storage (ChromaDB)?", false)
        if enableChroma {
            chromaURL = askInput("Chroma URL", "http://localhost:8000")
        }
    }

    // 4) Write .env
    if enableOpenAI || enableChroma {
        if err := upsertDotEnv(cwd, openAIKey, chromaURL); err != nil {
            return fmt.Errorf("failed writing .env: %w", err)
        }
        fmt.Println(".env updated")
    }

    // 5) Ensure policy.yaml
    policyPath := filepath.Join(cwd, "policy.yaml")
    if _, err := os.Stat(policyPath); errors.Is(err, os.ErrNotExist) {
        if err := writeDefaultPolicy(policyPath, enableOpenAI); err != nil {
            return fmt.Errorf("failed writing policy.yaml: %w", err)
        }
        fmt.Printf("Created %s\n", policyPath)
    } else {
        overwrite := askYesNo("policy.yaml exists. Overwrite with a minimal template?", false)
        if overwrite {
            if err := writeDefaultPolicy(policyPath, enableOpenAI); err != nil {
                return fmt.Errorf("failed overwriting policy.yaml: %w", err)
            }
            fmt.Printf("Updated %s\n", policyPath)
        } else {
            fmt.Println("Keeping existing policy.yaml")
        }
    }

    // 6) Start server
    if askYesNo("Start PCAS server now?", true) {
        if err := startPCAServer(host, port); err != nil {
            return err
        }
        fmt.Println("Server starting in background (logs -> server.log). Waiting 2s…")
        time.Sleep(2 * time.Second)

        // Optional: send a quick test event
        if askYesNo("Send a test event now?", true) {
            // Use mock echo by default
            serverPort = port
            serverAddr = "" // use port
            eventType = "pcas.test.echo.v1"
            eventData = "{\"prompt\":\"hello from setup wizard\"}"
            eventSource = "pcasctl"
            traceID = ""
            if err := emitEvent(); err != nil {
                log.Printf("Failed to send test event: %v", err)
            }
        }

        fmt.Println("All set! Use 'pcas serve' to run again, or 'pcasctl emit' to send events.")
    } else {
        fmt.Println("You can start later with: pcas serve --host", host, "--port", port)
    }

    return nil
}

func upsertDotEnv(dir, openAIKey, chromaURL string) error {
    path := filepath.Join(dir, ".env")
    lines := []string{}
    existing := map[string]string{}

    // Read existing
    if b, err := os.ReadFile(path); err == nil {
        scanner := bufio.NewScanner(strings.NewReader(string(b)))
        for scanner.Scan() {
            line := scanner.Text()
            if strings.HasPrefix(strings.TrimSpace(line), "#") || strings.TrimSpace(line) == "" {
                lines = append(lines, line)
                continue
            }
            parts := strings.SplitN(line, "=", 2)
            if len(parts) == 2 {
                k := strings.TrimSpace(parts[0])
                v := parts[1]
                existing[k] = v
            } else {
                lines = append(lines, line)
            }
        }
    }

    if openAIKey != "" {
        existing["OPENAI_API_KEY"] = openAIKey
    }
    if chromaURL != "" {
        existing["CHROMA_URL"] = chromaURL
    }

    // Rebuild lines keeping comments, then appending/overwriting keys
    // Simple approach: rewrite file with keys we manage plus a comment
    out := []string{"# PCAS environment configuration"}
    for k, v := range existing {
        out = append(out, fmt.Sprintf("%s=%s", k, v))
    }
    content := strings.Join(out, "\n") + "\n"
    return os.WriteFile(path, []byte(content), 0600)
}

func writeDefaultPolicy(path string, includeOpenAI bool) error {
    var b strings.Builder
    b.WriteString("version: \"v0\"\n")
    b.WriteString("providers:\n")
    b.WriteString("  - name: mock\n")
    b.WriteString("    type: mock\n")
    if includeOpenAI {
        b.WriteString("  - name: openai\n")
        b.WriteString("    type: openai\n")
    }
    b.WriteString("rules:\n")
    b.WriteString("  - name: echo\n")
    b.WriteString("    if:\n")
    b.WriteString("      event_type: pcas.test.echo.v1\n")
    b.WriteString("    then:\n")
    b.WriteString("      provider: mock\n")
    if includeOpenAI {
        b.WriteString("  - name: chat\n")
        b.WriteString("    if:\n")
        b.WriteString("      event_type: pcas.llm.chat.v1\n")
        b.WriteString("    then:\n")
        b.WriteString("      provider: openai\n")
    }
    return os.WriteFile(path, []byte(b.String()), 0644)
}

func findPCASBinary() (string, []string) {
    // Prefer local binary in repo root
    candidates := []string{"pcas", "pcas.exe"}
    for _, c := range candidates {
        if _, err := os.Stat(c); err == nil {
            return "./" + c, nil
        }
    }
    // Fallback: if pcas is in PATH
    if path, err := exec.LookPath("pcas"); err == nil {
        return path, nil
    }
    // Last resort: go run
    return "go", []string{"run", "./cmd/pcas"}
}

func startPCAServer(host, port string) error {
    bin, args := findPCASBinary()
    serveArgs := []string{"serve"}
    if host != "" {
        serveArgs = append(serveArgs, "--host", host)
    }
    if port != "" {
        serveArgs = append(serveArgs, "--port", port)
    }
    cmdArgs := append(args, serveArgs...)

    cmd := exec.Command(bin, cmdArgs...)
    // Inherit environment (so .env may be picked up by server via godotenv)
    cmd.Env = os.Environ()

    // Redirect output to server.log
    logFile, err := os.OpenFile("server.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
    if err != nil {
        return fmt.Errorf("open server.log: %w", err)
    }
    cmd.Stdout = logFile
    cmd.Stderr = logFile

    // On Windows, ensure detached process? We'll just Start(), it remains until killed
    if err := cmd.Start(); err != nil {
        return fmt.Errorf("failed to start server: %w", err)
    }

    // Best-effort note about stopping
    if runtime.GOOS == "windows" {
        fmt.Println("To stop the server, end the process from Task Manager or close the shell that launched it.")
    } else {
        fmt.Println("To stop the server, kill the process (Ctrl+C if foreground, or kill by PID).")
    }
    return nil
}
