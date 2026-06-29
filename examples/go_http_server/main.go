package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	dynamicrouter "github.com/kavinbm16/mcp-dynamic-router"
	"github.com/kavinbm16/mcp-dynamic-router/router"
)

type CustomServer struct {
	app *dynamicrouter.App
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 1. Initialize the Dynamic Router App using the Fluent Builder Pattern
	app := dynamicrouter.NewBuilder().
		WithMCPConfigPath("mcp.toml").
		Build()
	defer app.Close()

	// 2. Start downstream MCP server connections
	report, err := app.Start(ctx)
	if err != nil {
		log.Fatalf("Failed to connect downstream MCP servers: %v", err)
	}
	fmt.Printf("Connected downstream servers: %v\n", report.Connected)

	// 3. Start custom HTTP Server wrapping the router
	server := &CustomServer{app: app}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /route", server.HandleRoute)
	mux.HandleFunc("POST /execute", server.HandleExecute)

	log.Println("Custom Go HTTP Router Server listening on :8080...")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}

// HandleRoute parses a JSON query, runs dynamic routing, and returns candidate tools.
func (s *CustomServer) HandleRoute(w http.ResponseWriter, r *http.Request) {
	var req router.RouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Invoke routing engine
	result, err := s.app.Route(r.Context(), req)
	if err != nil {
		http.Error(w, fmt.Sprintf("routing failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// HandleExecute accepts a tool ID and arguments, validates them, and executes them downstream.
func (s *CustomServer) HandleExecute(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ToolID    string         `json:"tool_id"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// 1. Retrieve tool metadata from the registry
	tool, found := s.app.Registry.Lookup(input.ToolID)
	if !found {
		http.Error(w, fmt.Sprintf("tool %q not found", input.ToolID), http.StatusNotFound)
		return
	}

	// 2. Validate arguments at the gateway boundary
	if valErrs := router.ValidateArguments(tool, input.Arguments); len(valErrs) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error":             "schema validation failed",
			"validation_errors": valErrs,
		})
		return
	}

	// 3. Dispatch execution to the downstream MCP server session
	result, err := s.app.Call(r.Context(), tool, input.Arguments)
	if err != nil {
		http.Error(w, fmt.Sprintf("tool execution failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
