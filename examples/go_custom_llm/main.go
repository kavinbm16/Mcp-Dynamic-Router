package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	dynamicrouter "github.com/kavinbm16/mcp-dynamic-router"
	"github.com/kavinbm16/mcp-dynamic-router/router"
)

// CustomEmbedder implements the router.Embedder interface.
// Replace the mock logic below with actual calls to your embedding provider
// (e.g. OpenAI, Cohere, HuggingFace, or a custom internal API).
type CustomEmbedder struct {
	Dimension int
}

func (e *CustomEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	fmt.Printf("[Custom Embedder] Batch embedding requested for %d texts\n", len(texts))
	results := make([][]float32, len(texts))
	for i, text := range texts {
		// Mock vector generation. In production, make a POST request to your embedding API.
		vector := make([]float32, e.Dimension)
		// Seed vector values based on text content length for mock variance
		val := float32(len(text)) / 100.0
		for d := 0; d < e.Dimension; d++ {
			vector[d] = val * float32(d+1)
		}
		results[i] = vector
	}
	return results, nil
}

// CustomReranker implements the router.Reranker interface.
// It receives the candidate shortlist and uses a generative LLM or cross-encoder
// to score tool relevance relative to the query context.
type CustomReranker struct{}

func (r *CustomReranker) Rerank(ctx context.Context, utterance, conversationContext string, tools []router.Tool) (map[string]float64, error) {
	fmt.Printf("[Custom Reranker] Reranking %d candidates for utterance: %q\n", len(tools), utterance)
	scores := make(map[string]float64)

	for _, tool := range tools {
		score := 0.1 // default fallback score

		// Simple mock semantic rules
		if strings.Contains(strings.ToLower(utterance), "weather") && strings.Contains(tool.ID, "weather") {
			score = 0.95
		} else if strings.Contains(strings.ToLower(utterance), "schedule") && strings.Contains(tool.ID, "calendar") {
			score = 0.98
		}

		scores[tool.ID] = score
	}
	return scores, nil
}

func main() {
	ctx := context.Background()

	// 1. Instantiate the Custom Embedder and Reranker
	myEmbedder := &CustomEmbedder{Dimension: 384}
	myReranker := &CustomReranker{}

	// 2. Configure dynamic router options using the Fluent Builder Pattern
	app := dynamicrouter.NewBuilder().
		WithMCPConfigPath("mcp.toml").
		WithEmbedder(myEmbedder).
		WithReranker(myReranker).
		Build()
	defer app.Close()

	// 3. Register dummy tools in the registry for mock verification
	dummyTools := []router.Tool{
		{
			ID:          "weather.forecast",
			Server:      "weather-service",
			Domain:      "weather",
			Name:        "forecast",
			Description: "Domain: weather. Purpose: Get forecast details.",
			InputSchema: []byte(`{"type":"object"}`),
		},
		{
			ID:          "calendar.create",
			Server:      "calendar-service",
			Domain:      "calendar",
			Name:        "create_event",
			Description: "Domain: calendar. Purpose: Book calendar meetings.",
			InputSchema: []byte(`{"type":"object"}`),
		},
	}

	if err := app.Registry.ReplaceServer("weather-service", dummyTools[:1]); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}
	if err := app.Registry.ReplaceServer("calendar-service", dummyTools[1:]); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	// 4. Compile/Refresh index (this triggers the CustomEmbedder for all tool descriptions)
	fmt.Println("Compiling routing index...")
	if err := app.Router.Refresh(ctx); err != nil {
		log.Fatalf("Index refresh failed: %v", err)
	}

	// 5. Run Routing (triggers CustomEmbedder for query, then CustomReranker for candidates)
	fmt.Println("\nRouting Query...")
	res, err := app.Route(ctx, router.RouteRequest{
		Utterance: "Please schedule a meeting for tomorrow at 2 PM",
		Context:   "",
		Final:     true,
	})
	if err != nil {
		log.Fatalf("Routing request failed: %v", err)
	}

	fmt.Printf("\nRouting Complete.\nDecision: %s\n", res.Decision)
	for _, c := range res.Candidates {
		fmt.Printf("- Candidate: %s (Fused Score: %.3f, Semantic Score: %.3f)\n", c.Tool.ID, c.Score, c.SemanticScore)
	}
}
