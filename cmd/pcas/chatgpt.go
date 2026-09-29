package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai/siwc"
)

// This helper needs no PCAS database or Codex binary. Run login on the machine
// running the browser, then transfer credentials.json over SSH for remote use.
func chatgptCommand(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	dirDefault := os.Getenv("PCAS_CHATGPT_DIR")
	if dirDefault == "" {
		dirDefault = "data/chatgpt"
	}
	dir := flags.String("dir", dirDefault, "protected ChatGPT credential directory")
	client := flags.String("client", "", "saved issued client ID for reauthorization")
	port := flags.Int("port", 1455, "local callback port (0 selects an available port)")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	m, err := siwc.New(*dir, "127.0.0.1", *port)
	if err != nil {
		return err
	}
	defer m.Close()
	switch args[0] {
	case "chatgpt-import":
		if flags.NArg() != 1 {
			return fmt.Errorf("usage: pcas chatgpt-import [--dir DIR] PROTECTED_FILE")
		}
		if err := m.Import(ctx, flags.Arg(0)); err != nil {
			return err
		}
		fmt.Println("ChatGPT credentials imported; destination host ID preserved.")
		return nil
	case "chatgpt-verify":
		if err := m.Verify(ctx); err != nil {
			return err
		}
		fmt.Println("ChatGPT generation, rotating refresh, renewed generation and remote revocation verified. Sign in again with the saved client ID to enable the default.")
		return nil
	case "chatgpt-login":
		login, err := m.Begin(ctx, *client, false, false)
		if err != nil {
			return err
		}
		fmt.Println("Open this URL in the browser on THIS computer. Requests consume your ChatGPT plan:")
		fmt.Println(login.AuthorizationURL) // No retained ID-token hint in CLI URLs.
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				status, err := m.Status(ctx)
				if err != nil {
					return err
				}
				if status.Pending {
					continue
				}
				if status.Error != "" {
					return fmt.Errorf("%s", status.Error)
				}
				for _, account := range status.Accounts {
					if account.ClientID == status.Active && account.Connected {
						fmt.Printf("Connected registration: %s\nProtected credentials: %s\n", account.ClientID, filepath.Join(*dir, "credentials.json"))
						if !account.PlanEnabled {
							fmt.Println("ChatGPT plan permission was not granted; inference is disabled.")
						}
						return nil
					}
				}
				return fmt.Errorf("ChatGPT sign-in did not complete")
			}
		}
	}
	return fmt.Errorf("unknown ChatGPT command")
}
