// The adapter reads only a synthetic, already captured product context. It
// never accesses the database, modifies memories, or calls a model.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "frozen adapter rejected input")
		os.Exit(1)
	}
}
func run() error {
	return replay(os.Stdin, os.Stdout, os.Getenv("PCAS_V2B_CONTEXT_SNAPSHOT"))
}
func replay(input io.Reader, output io.Writer, path string) error {
	s, err := doing.LoadSnapshot(path)
	if err != nil {
		return err
	}
	var in struct {
		Task    string `json:"task_id"`
		Request string `json:"request"`
		AsOf    string `json:"as_of"`
	}
	if err := json.NewDecoder(input).Decode(&in); err != nil {
		return err
	}
	if in.AsOf != s.AsOf {
		return fmt.Errorf("date mismatch")
	}
	for _, e := range s.Entries {
		if e.Task == in.Task {
			if e.RequestSHA != doing.SHA(in.Request) {
				return fmt.Errorf("request mismatch")
			}
			return json.NewEncoder(output).Encode(struct {
				Context    string `json:"context"`
				ModelCalls int    `json:"model_calls"`
			}{e.Text, 0})
		}
	}
	return fmt.Errorf("unknown task")
}
