package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Result  any            `json:"result,omitempty"`
	Error   map[string]any `json:"error,omitempty"`
}

func main() {
	// 1. Define and start the Weather Service
	weatherMux := http.NewServeMux()
	weatherMux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var res jsonRPCResponse
		res.JSONRPC = "2.0"
		res.ID = req.ID

		switch req.Method {
		case "initialize":
			res.Result = map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "mock-weather", "version": "1.0.0"},
			}
		case "notifications/initialized":
			w.WriteHeader(http.StatusNoContent)
			return
		case "tools/list":
			res.Result = map[string]any{
				"tools": []map[string]any{
					{
						"name":        "forecast",
						"description": "Domain: weather. Purpose: Get weather forecast. Parameters: location (string).",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"location": map[string]any{"type": "string"},
							},
							"required": []string{"location"},
						},
					},
				},
			}
		case "tools/call":
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			json.Unmarshal(req.Params, &params)
			loc, _ := params.Arguments["location"].(string)
			res.Result = map[string]any{
				"content": []map[string]any{
					{
						"type": "text",
						"text": fmt.Sprintf("The weather in %s is currently sunny and 22°C.", loc),
					},
				},
			}
		default:
			res.Error = map[string]any{"code": -32601, "message": "method not found"}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	})

	// 2. Define and start the Calculator Service
	calcMux := http.NewServeMux()
	calcMux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var res jsonRPCResponse
		res.JSONRPC = "2.0"
		res.ID = req.ID

		switch req.Method {
		case "initialize":
			res.Result = map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "mock-calculator", "version": "1.0.0"},
			}
		case "notifications/initialized":
			w.WriteHeader(http.StatusNoContent)
			return
		case "tools/list":
			res.Result = map[string]any{
				"tools": []map[string]any{
					{
						"name":        "add",
						"description": "Domain: math. Purpose: Add two numbers. Parameters: a (number), b (number).",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"a": map[string]any{"type": "number"},
								"b": map[string]any{"type": "number"},
							},
							"required": []string{"a", "b"},
						},
					},
				},
			}
		case "tools/call":
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			json.Unmarshal(req.Params, &params)
			a, _ := params.Arguments["a"].(float64)
			b, _ := params.Arguments["b"].(float64)
			res.Result = map[string]any{
				"content": []map[string]any{
					{
						"type": "text",
						"text": fmt.Sprintf("Result: %.2f", a+b),
					},
				},
			}
		default:
			res.Error = map[string]any{"code": -32601, "message": "method not found"}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	})

	// Start servers in separate goroutines
	go func() {
		log.Println("[Mock Server] Starting Weather Service on :8091/mcp...")
		if err := http.ListenAndServe(":8091", weatherMux); err != nil {
			log.Printf("Weather Service stopped: %v", err)
		}
	}()

	go func() {
		log.Println("[Mock Server] Starting Calculator Service on :8092/mcp...")
		if err := http.ListenAndServe(":8092", calcMux); err != nil {
			log.Printf("Calculator Service stopped: %v", err)
		}
	}()

	// 3. Write dynamic config file 'mcp-mock.toml'
	mcpConfig := `[[servers]]
name = "weather-service"
url = "http://localhost:8091/mcp"
transport = "streamable-http"

[[servers]]
name = "calculator-service"
url = "http://localhost:8092/mcp"
transport = "streamable-http"
`
	if err := os.WriteFile("mcp-mock.toml", []byte(mcpConfig), 0644); err != nil {
		log.Fatalf("Failed to write mcp-mock.toml: %v", err)
	}
	log.Println("[Mock Server] Generated 'mcp-mock.toml' configuration file!")

	// Block until interrupted
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("[Mock Server] Stopping mock services...")
	os.Remove("mcp-mock.toml")
}
