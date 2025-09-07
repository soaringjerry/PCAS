package cmd

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "strings"
    "time"

    "github.com/sashabaranov/go-openai"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"

    busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
    eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

type gateway struct {
    pcas busv1.EventBusServiceClient
    oai  *openai.Client
}

func newGatewayMux(pcasAddr string) (http.Handler, error) {
    // gRPC to PCAS
    conn, err := grpc.Dial(pcasAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        return nil, fmt.Errorf("pcas dial: %w", err)
    }
    pcasClient := busv1.NewEventBusServiceClient(conn)

    // Upstream OpenAI (OpenRouter auto by env)
    apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
    cfg := openai.DefaultConfig(apiKey)
    if base := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")); base != "" {
        cfg.BaseURL = base
    } else if strings.HasPrefix(strings.ToLower(apiKey), "sk-or-") {
        cfg.BaseURL = "https://openrouter.ai/api/v1"
    }
    oaiClient := openai.NewClientWithConfig(cfg)

    g := &gateway{pcas: pcasClient, oai: oaiClient}
    mux := http.NewServeMux()
    mux.HandleFunc("/v1/chat/completions", g.handleChatCompletions)
    mux.HandleFunc("/v1/embeddings", g.handleEmbeddings)
    mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })
    return mux, nil
}

// Minimal OpenAI schema
type chatRequest struct {
    Model    string                   `json:"model"`
    Messages []openai.ChatCompletionMessage `json:"messages"`
    UserID   string                   `json:"user_id"`
    PCASRAG  any                      `json:"pcas_rag"`
    Temperature *float32              `json:"temperature,omitempty"`
}

type chatResponse = openai.ChatCompletionResponse

func (g *gateway) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    var req chatRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "bad json", http.StatusBadRequest)
        return
    }
    if req.Model == "" {
        req.Model = "gpt-4o-mini"
    }
    if req.UserID == "" {
        req.UserID = r.Header.Get("X-User-ID")
    }

    // Auto RAG if requested (truthy pcas_rag)
    if truthy(req.PCASRAG) && len(req.Messages) > 0 {
        // Use last user message as query
        query := lastUserMessage(req.Messages)
        if query != "" {
            ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
            defer cancel()
            sreq := &busv1.SearchRequest{QueryText: query, TopK: 5, UserId: req.UserID}
            sresp, err := g.pcas.Search(ctx, sreq)
            if err == nil && len(sresp.Events) > 0 {
                system := buildSystemContext(sresp.Events)
                // Prepend system message
                newMsgs := []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleSystem, Content: system}}
                newMsgs = append(newMsgs, req.Messages...)
                req.Messages = newMsgs
            }
        }
    }

    // Call upstream
    oreq := openai.ChatCompletionRequest{Model: req.Model, Messages: req.Messages}
    if req.Temperature != nil {
        oreq.Temperature = *req.Temperature
    }
    resp, err := g.oai.CreateChatCompletion(r.Context(), oreq)
    if err != nil {
        http.Error(w, fmt.Sprintf("upstream error: %v", err), http.StatusBadGateway)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

type embedRequest struct {
    Model string        `json:"model"`
    Input any           `json:"input"`
}

func (g *gateway) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    var req embedRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "bad json", http.StatusBadRequest)
        return
    }
    if req.Model == "" {
        req.Model = string(openai.LargeEmbedding3)
    }
    oreq := openai.EmbeddingRequest{Model: openai.EmbeddingModel(req.Model), Input: req.Input}
    resp, err := g.oai.CreateEmbeddings(r.Context(), oreq)
    if err != nil {
        http.Error(w, fmt.Sprintf("upstream error: %v", err), http.StatusBadGateway)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

func truthy(v any) bool {
    switch x := v.(type) {
    case bool:
        return x
    case string:
        t := strings.ToLower(strings.TrimSpace(x))
        return t == "1" || t == "true" || t == "yes"
    case map[string]any:
        return true
    default:
        return false
    }
}

func lastUserMessage(msgs []openai.ChatCompletionMessage) string {
    for i := len(msgs) - 1; i >= 0; i-- {
        if msgs[i].Role == openai.ChatMessageRoleUser && strings.TrimSpace(msgs[i].Content) != "" {
            return msgs[i].Content
        }
    }
    return ""
}

// naive system prompt builder from events' subjects + text-like fields
func buildSystemContext(events []*eventsv1.Event) string {
    var b strings.Builder
    b.WriteString("You are a personal AI assistant. Answer using only the trusted context below when relevant.\n\n---\n")
    for _, e := range events {
        // type and subject
        if e.GetTime() != nil {
            b.WriteString(fmt.Sprintf("[%s] ", e.GetTime().AsTime().Format("2006-01-02 15:04")))
        }
        b.WriteString(e.GetType())
        if s := strings.TrimSpace(e.GetSubject()); s != "" {
            b.WriteString(": ")
            b.WriteString(s)
        }
        b.WriteString("\n")
        // try to extract text-like fields from data
        if e.GetData() != nil {
            // data is Any of structpb.Value; we can reuse JSON text of the Any
            raw := e.GetData().String()
            // best effort compact
            if len(raw) > 2000 { raw = raw[:2000] + "..." }
            b.WriteString(raw)
            b.WriteString("\n")
        }
        b.WriteString("---\n")
    }
    return b.String()
}
