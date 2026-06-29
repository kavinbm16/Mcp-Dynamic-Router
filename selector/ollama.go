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
	prompt := "Select tools relevant to the request. Score every candidate from 0 to 1. Use limitations to reject superficially similar tools. Return JSON only.\nUtterance: " + utterance + "\nConversation context: " + conversationContext + "\nCandidates: " + string(encodedTools)
	requestBody := map[string]any{
		"model":  o.Model,
		"stream": false,
		"format": map[string]any{
			"type": "object",
			"properties": map[string]any{"scores": map[string]any{
				"type": "array", "items": map[string]any{
					"type": "object", "properties": map[string]any{
						"tool_id": map[string]any{"type": "string"},
						"score":   map[string]any{"type": "number"},
					}, "required": []string{"tool_id", "score"},
				},
			}},
			"required": []string{"scores"},
		},
		"messages": []map[string]string{
			{"role": "system", "content": "You are a precise MCP tool selector."},
			{"role": "user", "content": prompt},
		},
		"options": map[string]any{"temperature": 0},
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
		Scores []struct {
			ToolID string  `json:"tool_id"`
			Score  float64 `json:"score"`
		} `json:"scores"`
	}
	if err := json.Unmarshal([]byte(envelope.Message.Content), &selection); err != nil {
		return nil, fmt.Errorf("decode selector output: %w", err)
	}
	result := make(map[string]float64, len(selection.Scores))
	for _, score := range selection.Scores {
		result[score.ToolID] = score.Score
	}
	return result, nil
}
