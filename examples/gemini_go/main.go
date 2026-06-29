package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	dynamicrouter "github.com/kavinbm16/mcp-dynamic-router"
	"github.com/kavinbm16/mcp-dynamic-router/router"
	"google.golang.org/genai"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY environment variable is required")
	}

	// 1. Initialize the Dynamic Router App
	app := dynamicrouter.New(dynamicrouter.Options{
		MCPConfigPath: "mcp.toml",
	})
	defer app.Close()

	report, err := app.Start(ctx)
	if err != nil {
		log.Fatalf("Failed to start dynamic router: %v", err)
	}
	fmt.Printf("Connected downstream servers: %v\n", report.Connected)

	// 2. Initialize the Google GenAI Go SDK Client
	// The client automatically picks up the GEMINI_API_KEY environment variable.
	client, err := genai.NewClient(ctx, nil)
	if err != nil {
		log.Fatalf("Failed to initialize GenAI client: %v", err)
	}

	// 3. Define the single meta-tool declaration for Gemini.
	// This delegates all tool search and dispatch logic to the dynamic router.
	toolsConfig := []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        "invoke_tool",
					Description: "Semantic Tool Router. Invokes specialized MCP tools (weather, calculations, search) by matching the intent query.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"intent": {
								Type:        genai.TypeString,
								Description: "The user query or intent description (e.g. 'weather forecast in Bengaluru').",
							},
							"arguments": {
								Type:        genai.TypeObject,
								Description: "Parameters extracted from the conversation matching the tool requirement.",
							},
						},
						Required: []string{"intent", "arguments"},
					},
				},
			},
		},
	}

	prompt := "Will I need an umbrella in Bengaluru today? Check the weather forecast."
	fmt.Printf("\nUser: %q\n", prompt)

	// 4. Send request to Gemini with tools enabled
	model := "gemini-2.5-flash"
	config := &genai.GenerateContentConfig{
		Tools: toolsConfig,
	}

	resp, err := client.Models.GenerateContent(ctx, model, genai.Text(prompt), config)
	if err != nil {
		log.Fatalf("Gemini generation failed: %v", err)
	}

	// 5. Intercept the function call from Gemini and execute it using the router
	var functionCall *genai.FunctionCall
	for _, candidate := range resp.Candidates {
		if candidate.Content != nil {
			for _, part := range candidate.Content.Parts {
				if part.FunctionCall != nil && part.FunctionCall.Name == "invoke_tool" {
					functionCall = part.FunctionCall
					break
				}
			}
		}
	}

	if functionCall == nil {
		fmt.Printf("Gemini Response: %s\n", resp.Text())
		return
	}

	intent, _ := functionCall.Args["intent"].(string)
	args, _ := functionCall.Args["arguments"].(map[string]any)
	fmt.Printf("[Gemini Tool Call] Intercepted intent: %q\n", intent)

	// 6. Use the Dynamic Router to select the target tool
	routeRes, err := app.Route(ctx, router.RouteRequest{
		Utterance: intent,
		Final:     true,
	})
	if err != nil {
		log.Fatalf("Routing failed: %v", err)
	}

	if routeRes.Decision != router.DecisionSelected || len(routeRes.Candidates) == 0 {
		log.Fatalf("Abstained from routing: %s", routeRes.Reason)
	}

	targetTool := routeRes.Candidates[0].Tool
	fmt.Printf("[Router Selected] ID: %s\n", targetTool.ID)

	// 7. Execute the tool via our gateway facade
	callRes, err := app.Call(ctx, targetTool, args)
	if err != nil {
		log.Fatalf("Tool call execution failed: %v", err)
	}
	fmt.Printf("[Execution Success] Result: %+v\n", callRes.Content)

	// 8. Feed the tool result back to Gemini to obtain the final verbal response
	responseParts := []*genai.Part{
		{
			FunctionResponse: &genai.FunctionResponse{
				Name:     "invoke_tool",
				Response: map[string]any{"result": callRes.Content},
			},
		},
	}

	// Resume generation session with the tool outputs
	finalResp, err := client.Models.GenerateContent(ctx, model, genai.Text(prompt), &genai.GenerateContentConfig{
		Tools: toolsConfig,
		// In a real chat turn, you would pass the full conversation history.
		// For this example, we pass the response parts back to get the final answer.
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				{
					Text: "Integrate the tool output into a conversational response.",
				},
			},
		},
	})
	_ = responseParts // Unused in this simple example turn, but would be appended in full history sessions

	if err != nil {
		log.Fatalf("Final generation failed: %v", err)
	}
	fmt.Printf("Gemini Final Response: %s\n", finalResp.Text())
}
