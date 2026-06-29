package main

import (
	"context"
	"fmt"
	"log"
	"time"

	dynamicrouter "github.com/kavinbm16/mcp-dynamic-router"
	"github.com/kavinbm16/mcp-dynamic-router/router"
	"github.com/kavinbm16/mcp-dynamic-router/streamrag"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Initialize the Dynamic Router App using the Fluent Builder Pattern
	app := dynamicrouter.NewBuilder().
		WithMCPConfigPath("mcp.toml").
		Build()
	defer app.Close()

	// 2. Start the App (connects to all configured downstream MCP servers)
	report, err := app.Start(ctx)
	if err != nil {
		log.Fatalf("Failed to start MCP dynamic router client: %v", err)
	}
	fmt.Printf("MCP servers connected: %v\n", report.Connected)
	if len(report.Failed) > 0 {
		fmt.Printf("MCP servers failed: %v\n", report.Failed)
	}

	// 3. Stateless Routing Example (Single Utterance)
	// We simulate a user asking a question that requires weather tool routing.
	fmt.Println("\n--- Stateless Routing ---")
	routeRes, err := app.Route(ctx, router.RouteRequest{
		Utterance: "What is the weather like in Bengaluru?",
		Context:   "User timezone: Asia/Kolkata",
		Final:     true,
	})
	if err != nil {
		log.Fatalf("Routing error: %v", err)
	}

	fmt.Printf("Decision: %s (Reason: %s)\n", routeRes.Decision, routeRes.Reason)
	if routeRes.Decision == router.DecisionSelected && len(routeRes.Candidates) > 0 {
		topTool := routeRes.Candidates[0].Tool
		fmt.Printf("Top Candidate Tool ID: %s (Server: %s, Name: %s)\n", topTool.ID, topTool.Server, topTool.Name)

		// Simulating tool execution using the gateway's Call method
		fmt.Println("Calling tool execution gateway...")
		args := map[string]any{"location": "Bengaluru", "units": "metric"}
		callRes, err := app.Call(ctx, topTool, args)
		if err != nil {
			log.Fatalf("Tool call execution failed: %v", err)
		}
		fmt.Printf("Execution Result: %+v\n", callRes.Content)
	}

	// 4. Stream RAG Session Example (Incremental Speech Partials) using Fluent StreamBuilder
	// We simulate a user speaking, triggering partial updates before committing.
	fmt.Println("\n--- Stream RAG Speech Pipeline ---")
	session := app.NewStreamBuilder().
		OnPrediction(func(p streamrag.Prediction) {
			fmt.Printf("[Stream Hook] Partial stable=%v, top_tool=%s\n", p.Stable, p.Result.Candidates[0].Tool.ID)
		}).
		OnPrefetch(func(ctx context.Context, tool router.Tool) error {
			fmt.Printf("[Stream Hook] Prefetching read-only tool to warm connection: %s\n", tool.ID)
			return nil
		}).
		OnCommit(func(p streamrag.Prediction) {
			fmt.Printf("[Stream Hook] Speech Committed! Final tool selected: %s\n", p.Result.Candidates[0].Tool.ID)
		}).
		Build()
	defer session.Close()

	// Simulate speech partial 1
	fmt.Println("User: 'What's the weather...'")
	session.Submit(ctx, streamrag.Event{
		Transcript: "What's the weather",
		Confidence: 0.70,
		Final:      false,
	})
	time.Sleep(100 * time.Millisecond)

	// Simulate speech partial 2 (becomes stable)
	fmt.Println("User: 'What's the weather in Bengaluru...'")
	session.Submit(ctx, streamrag.Event{
		Transcript: "What's the weather in Bengaluru",
		Confidence: 0.85,
		Final:      false,
	})
	time.Sleep(100 * time.Millisecond)

	// Simulate speech commit (final)
	fmt.Println("User: 'What's the weather in Bengaluru today?'")
	session.Submit(ctx, streamrag.Event{
		Transcript: "What's the weather in Bengaluru today",
		Confidence: 0.95,
		Final:      true,
	})
	time.Sleep(100 * time.Millisecond)
}
