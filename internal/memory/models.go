package memory

// ModelInfo is the configuration of one model as the rest of the system reads
// it: what it is, what it costs, and what it can do.
type ModelInfo struct {
	ID, Name, Protocol, Model         string
	InputPerMillion, OutputPerMillion float64
	MaxOutput                         int
	Embedding, Transcription          bool
}
