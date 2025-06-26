package bus

import (
	"bytes"
	"container/list"
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	
	"google.golang.org/protobuf/types/known/structpb"
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
	"github.com/soaringjerry/pcas/internal/storage"
)

const (
	// Maximum tokens for context (approximately 4000 tokens)
	maxContextTokens = 16000 // ~4000 tokens at 4 chars/token
	
	// RAG configuration
	ragTopK = 5
	ragTimeout = 25 * time.Second // 留出足够时间给 OpenAI API 调用
)

// embeddingCacheEntry represents a cached embedding
type embeddingCacheEntry struct {
	embedding []float32
	timestamp time.Time
}

// embeddingCache implements an LRU cache for embeddings
type embeddingCache struct {
	mu       sync.RWMutex
	capacity int
	cache    map[string]*list.Element
	lru      *list.List
	
	// Metrics
	hits   int64
	misses int64
}

// cacheItem stores the key-value pair in the LRU list
type cacheItem struct {
	key   string
	value embeddingCacheEntry
}

// newEmbeddingCache creates a new LRU embedding cache
func newEmbeddingCache(capacity int) *embeddingCache {
	return &embeddingCache{
		capacity: capacity,
		cache:    make(map[string]*list.Element),
		lru:      list.New(),
	}
}

// Get retrieves an embedding from the cache
func (c *embeddingCache) Get(key string) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if elem, found := c.cache[key]; found {
		c.hits++
		// Move to front (most recently used)
		c.lru.MoveToFront(elem)
		item := elem.Value.(*cacheItem)
		return item.value.embedding, true
	}
	
	c.misses++
	return nil, false
}

// Set stores an embedding in the cache
func (c *embeddingCache) Set(key string, embedding []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Check if already exists
	if elem, found := c.cache[key]; found {
		// Update and move to front
		c.lru.MoveToFront(elem)
		item := elem.Value.(*cacheItem)
		item.value = embeddingCacheEntry{
			embedding: embedding,
			timestamp: time.Now(),
		}
		return
	}
	
	// Add new entry
	item := &cacheItem{
		key: key,
		value: embeddingCacheEntry{
			embedding: embedding,
			timestamp: time.Now(),
		},
	}
	elem := c.lru.PushFront(item)
	c.cache[key] = elem
	
	// Evict oldest if over capacity
	if c.lru.Len() > c.capacity {
		oldest := c.lru.Back()
		if oldest != nil {
			c.lru.Remove(oldest)
			oldItem := oldest.Value.(*cacheItem)
			delete(c.cache, oldItem.key)
		}
	}
}

// applyRAGEnhancement enriches the event context with relevant historical events
func (s *Server) applyRAGEnhancement(ctx context.Context, event *eventsv1.Event, requestData map[string]interface{}) {
	// Check if RAG is enabled via environment variable
	if os.Getenv("PCAS_RAG_ENABLED") != "true" {
		return
	}
	
	// Create timeout context
	ragCtx, cancel := context.WithTimeout(ctx, ragTimeout)
	defer cancel()
	
	// Log RAG enhancement attempt
	log.Printf("Applying RAG enhancement for event %s", event.Id)
	
	// Graceful degradation wrapper
	defer func() {
		if r := recover(); r != nil {
			log.Printf("RAG enhancement panic recovered: %v", r)
		}
	}()
	
	// Generate query text from event
	queryText := s.generateQueryText(event, requestData)
	if queryText == "" {
		log.Printf("RAG: Unable to generate query text for event %s", event.Id)
		return
	}
	log.Printf("RAG: Generated query text: %s", queryText)
	
	// Check embedding cache first
	cacheKey := fmt.Sprintf("rag:%s", queryText)
	var queryEmbedding []float32
	var err error
	
	if cached, found := s.embeddingCache.Get(cacheKey); found {
		queryEmbedding = cached
		log.Printf("RAG: Using cached embedding for query")
	} else {
		// Rate limit embedding requests
		if err := s.rateLimiter.Wait(ragCtx); err != nil {
			log.Printf("RAG: Rate limit exceeded: %v", err)
			return
		}
		
		// Use singleflight to deduplicate concurrent requests
		result, err, _ := s.singleFlight.Do(cacheKey, func() (interface{}, error) {
			return s.embeddingProvider.CreateEmbedding(ragCtx, queryText)
		})
		
		if err != nil {
			log.Printf("RAG: Failed to create embedding: %v", err)
			return
		}
		
		queryEmbedding = result.([]float32)
		s.embeddingCache.Set(cacheKey, queryEmbedding)
	}
	
	// Query similar events (no filters for now)
	similarResults, err := s.vectorStorage.QuerySimilar(ragCtx, queryEmbedding, ragTopK, nil)
	if err != nil {
		log.Printf("RAG: Failed to query similar events: %v", err)
		return
	}
	
	// CRITICAL: Immediately filter out self-reference
	var cleanedResults []storage.QueryResult
	for _, result := range similarResults {
		if result.ID != event.Id {
			cleanedResults = append(cleanedResults, result)
		} else {
			log.Printf("RAG: Filtered out self-reference: %s", result.ID)
		}
	}
	
	if len(cleanedResults) == 0 {
		log.Printf("RAG: No similar events found after self-reference filtering")
		// Mark RAG as attempted but no results
		if requestData == nil {
			requestData = make(map[string]interface{})
		}
		requestData["rag_applied"] = false
		requestData["rag_reason"] = "no_similar_events"
		return
	}
	
	// Filter by relevance score threshold
	var relevantIDs []string
	for _, result := range cleanedResults {
		log.Printf("RAG: Found similar event %s with score %.3f", result.ID, result.Score)
		if result.Score > 0.4 { // Adjusted threshold for semantic similarity
			relevantIDs = append(relevantIDs, result.ID)
		}
	}
	
	if len(relevantIDs) == 0 {
		log.Printf("RAG: No relevant events after filtering")
		// Mark RAG as attempted but low similarity
		if requestData == nil {
			requestData = make(map[string]interface{})
		}
		requestData["rag_applied"] = false
		requestData["rag_reason"] = "low_similarity"
		return
	}
	
	// Batch retrieve relevant events
	relevantEvents, err := s.storage.BatchGetEvents(ragCtx, relevantIDs)
	if err != nil {
		log.Printf("RAG: Failed to batch retrieve events: %v", err)
		// Mark RAG as attempted but retrieval error
		if requestData == nil {
			requestData = make(map[string]interface{})
		}
		requestData["rag_applied"] = false
		requestData["rag_reason"] = "retrieval_error"
		return
	}
	
	// Sort events by relevance score
	eventScoreMap := make(map[string]float32)
	for _, result := range cleanedResults {
		eventScoreMap[result.ID] = result.Score
	}
	
	sortedEvents := make([]*eventsv1.Event, len(relevantEvents))
	copy(sortedEvents, relevantEvents)
	sort.Slice(sortedEvents, func(i, j int) bool {
		scoreI := eventScoreMap[sortedEvents[i].Id]
		scoreJ := eventScoreMap[sortedEvents[j].Id]
		return scoreI > scoreJ
	})
	
	// Render events as context with token limit
	contextMarkdown := s.renderEventsMarkdown(sortedEvents, maxContextTokens)
	if contextMarkdown == "" {
		log.Printf("RAG: No context generated")
		// Mark RAG as attempted but no context
		if requestData == nil {
			requestData = make(map[string]interface{})
		}
		requestData["rag_applied"] = false
		requestData["rag_reason"] = "no_context_generated"
		return
	}
	
	// Add context to request data
	if requestData == nil {
		requestData = make(map[string]interface{})
	}
	
	// Inject context as system message for OpenAI
	originalPrompt, _ := requestData["prompt"].(string)
	systemMessage := fmt.Sprintf("You have access to the following relevant historical context. Use this information to provide accurate and personalized responses:\n\n%s", contextMarkdown)
	
	// Create messages array for OpenAI chat format
	requestData["messages"] = []map[string]string{
		{
			"role": "system",
			"content": systemMessage,
		},
		{
			"role": "user", 
			"content": originalPrompt,
		},
	}
	
	// Remove the original prompt to avoid confusion
	delete(requestData, "prompt")
	
	// Add metadata
	requestData["rag_event_count"] = len(sortedEvents)
	requestData["rag_applied"] = true
	
	log.Printf("RAG: Successfully enhanced with %d relevant events", len(sortedEvents))
}

// generateQueryText creates a search query from the event and request data
func (s *Server) generateQueryText(event *eventsv1.Event, requestData map[string]interface{}) string {
	var parts []string
	
	// Only extract pure semantic content
	
	// Add subject if present (pure content)
	if event.Subject != "" {
		parts = append(parts, event.Subject)
	}
	
	// Extract key information from request data
	if requestData != nil {
		// Look for common fields that might contain query-relevant info
		if query, ok := requestData["query"].(string); ok && query != "" {
			parts = append(parts, query)
		}
		if prompt, ok := requestData["prompt"].(string); ok && prompt != "" {
			parts = append(parts, prompt)
		}
		if message, ok := requestData["message"].(string); ok && message != "" {
			parts = append(parts, message)
		}
		if text, ok := requestData["text"].(string); ok && text != "" {
			parts = append(parts, text)
		}
	}
	
	if len(parts) == 0 {
		return ""
	}
	
	return strings.Join(parts, " ")
}

// renderEventsMarkdown converts events to markdown with token limit
func (s *Server) renderEventsMarkdown(events []*eventsv1.Event, maxTokens int) string {
	var buf bytes.Buffer
	currentTokens := 0
	
	buf.WriteString("## Relevant Historical Context\n\n")
	currentTokens += 40 // Approximate tokens for header
	
	for i, event := range events {
		// Render event to markdown
		eventMD := s.renderSingleEventMarkdown(event)
		eventTokens := len(eventMD) / 4 // Rough estimate: 4 chars per token
		
		// Check if adding this event would exceed token limit
		if currentTokens+eventTokens > maxTokens {
			buf.WriteString(fmt.Sprintf("\n*... and %d more relevant events (truncated due to token limit)*\n", 
				len(events)-i))
			break
		}
		
		buf.WriteString(eventMD)
		buf.WriteString("\n---\n\n")
		currentTokens += eventTokens + 10 // Include separator tokens
	}
	
	return buf.String()
}

// renderSingleEventMarkdown renders a single event as markdown
func (s *Server) renderSingleEventMarkdown(event *eventsv1.Event) string {
	var buf bytes.Buffer
	
	// More concise format focusing on key information
	if event.Time != nil {
		buf.WriteString(fmt.Sprintf("**[%s]** ", event.Time.AsTime().Format("2006-01-02 15:04")))
	}
	
	// Type and subject are most important
	buf.WriteString(fmt.Sprintf("%s", event.Type))
	if event.Subject != "" {
		buf.WriteString(fmt.Sprintf(": %s", event.Subject))
	}
	buf.WriteString("\n")
	
	// Extract key data fields
	if event.Data != nil {
		// Try to unmarshal as structpb.Value
		value := &structpb.Value{}
		if event.Data.MessageIs(value) {
			if err := event.Data.UnmarshalTo(value); err == nil {
				// Convert to map and extract key fields
				if dataMap, ok := value.AsInterface().(map[string]interface{}); ok {
					var keyFields []string
					
					// Priority fields to display
					priorityKeys := []string{"prompt", "message", "query", "text", "description", "content", "response", "result"}
					for _, key := range priorityKeys {
						if val, exists := dataMap[key]; exists {
							if strVal, ok := val.(string); ok && strVal != "" {
								// Truncate long values
								if len(strVal) > 200 {
									strVal = strVal[:197] + "..."
								}
								keyFields = append(keyFields, fmt.Sprintf("  - %s: %s", key, strVal))
							}
						}
					}
					
					if len(keyFields) > 0 {
						buf.WriteString(strings.Join(keyFields, "\n"))
						buf.WriteString("\n")
					}
				}
			}
		}
	}
	
	return buf.String()
}