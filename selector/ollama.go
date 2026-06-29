package selector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kavinbm16/mcp-dynamic-router/router"
)

// Ollama is an optional stage-two selector. It only sees the domain shortlist,
// never the complete registry.
type Ollama struct {
	BaseURL string
	Model   string
	Client  *http.Client
}

func (o *Ollama) Rerank(ctx context.Context, utterance, conversationContext string, tools []router.Tool) (map[string]float64, error) {
	if o.Model == "" {
		return nil, fmt.Errorf("selector model is required")
	}
	toolPayload := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		toolPayload = append(toolPayload, map[string]any{
			"id": tool.ID, "description": tool.Description, "input_schema": json.RawMessage(tool.InputSchema),
		})
	}
	encodedTools, err := json.Marshal(toolPayload)
	if err != nil {
		return nil, err
	}
	prompt := "Identify the single most relevant tool from the candidates list for the utterance. Return JSON only with the selected tool's id (or 'none' if no tools are relevant).\nUtterance: " + utterance + "\nConversation context: " + conversationContext + "\nCandidates: " + string(encodedTools)
	requestBody := map[string]any{
		"model":  o.Model,
		"stream": false,
		"format": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tool_id": map[string]any{"type": "string"},
			},
			"required": []string{"tool_id"},
		},
		"messages": []map[string]string{
			{"role": "system", "content": "You are a precise MCP tool selector."},
			{"role": "user", "content": prompt},
		},
		"options": map[string]any{
			"temperature": 0,
			"num_predict": 35, // Limit output tokens to reduce latency
		},
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}
	baseURL := strings.TrimRight(o.BaseURL, "/")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama selector returned %s", response.Status)
	}
	var envelope struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	var selection struct {
		ToolID string `json:"tool_id"`
	}
	if err := json.Unmarshal([]byte(envelope.Message.Content), &selection); err != nil {
		return nil, fmt.Errorf("decode selector output: %w", err)
	}
	result := make(map[string]float64)
	if selection.ToolID != "" && selection.ToolID != "none" {
		result[selection.ToolID] = 1.0
	}
	return result, nil
}
