package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Ollama implements router.Embedder using Ollama's batch /api/embed endpoint.
type Ollama struct {
	BaseURL string
	Model   string
	Client  *http.Client
}

func (o *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	baseURL := strings.TrimRight(o.BaseURL, "/")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if o.Model == "" {
		return nil, fmt.Errorf("embedding model is required")
	}
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	body, err := json.Marshal(map[string]any{"model": o.Model, "input": texts})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/embed", bytes.NewReader(body))
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
		return nil, fmt.Errorf("ollama embed returned %s", response.Status)
	}
	var payload struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Embeddings, nil
}
