package ai

import "github.com/soaringjerry/PCAS/internal/memory"

// Info is the configuration of the provider without its credentials.
func (p Provider) Info() memory.ModelInfo {
	return memory.ModelInfo{ID: p.ID, Name: p.Name, Protocol: p.Protocol, Model: p.Model, InputPerMillion: p.InputPerMillion, OutputPerMillion: p.OutputPerMillion, MaxOutput: p.MaxOutput, Embedding: p.Embedding, Transcription: p.Transcription}
}

// Describe returns the configuration of one model.
func (r *Registry) Describe(id string) (memory.ModelInfo, bool) {
	p, ok := r.Get(id)
	return p.Info(), ok
}

// List returns the configuration of each model.
func (r *Registry) List() []memory.ModelInfo {
	out := []memory.ModelInfo{}
	for _, p := range r.Providers() {
		out = append(out, p.Info())
	}
	return out
}

// Estimate is the cost that a call of the model with the input reserves. An
// unknown model reserves nothing.
func (r *Registry) Estimate(id, input string) float64 {
	p, ok := r.Get(id)
	if !ok {
		return 0
	}
	return p.Reserve(input)
}

// DirectDefault reports whether the direct ChatGPT connection is ready to be
// the default text model.
func (r *Registry) DirectDefault() bool {
	return r != nil && r.ChatGPT != nil && r.ChatGPT.DefaultReady()
}

// EndJob closes the shared subscription login after a job when the worker
// reloads it for each job.
func (r *Registry) EndJob() {
	if r != nil && r.ReloadSubscription && r.Codex != nil {
		r.Codex.Close()
	}
}
