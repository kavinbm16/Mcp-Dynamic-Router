package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/kavinbm16/mcp-dynamic-router/embedding"
	"github.com/kavinbm16/mcp-dynamic-router/router"
)

type caseRecord struct {
	Utterance    string `json:"utterance"`
	Context      string `json:"context,omitempty"`
	ExpectedTool string `json:"expected_tool,omitempty"`
}

func main() {
	toolsPath := flag.String("tools", "testdata/tools.json", "path to tool registry JSON")
	casesPath := flag.String("cases", "testdata/cases.jsonl", "path to labelled JSONL cases")
	ollamaURL := flag.String("ollama", "", "optional Ollama URL")
	model := flag.String("model", "nomic-embed-text", "Ollama embedding model")
	flag.Parse()

	tools := readTools(*toolsPath)
	registry := router.NewRegistry()
	byServer := make(map[string][]router.Tool)
	for _, tool := range tools {
		byServer[tool.Server] = append(byServer[tool.Server], tool)
	}
	for server, serverTools := range byServer {
		fatalIf(registry.ReplaceServer(server, serverTools))
	}
	var embedder router.Embedder
	if *ollamaURL != "" {
		embedder = &embedding.Ollama{BaseURL: *ollamaURL, Model: *model}
	}
	engine := router.New(registry, embedder, router.DefaultConfig())
	fatalIf(engine.Refresh(context.Background()))

	cases := readCases(*casesPath)
	var top1, top5, falseRoutes int
	latencies := make([]time.Duration, 0, len(cases))
	for _, test := range cases {
		result, err := engine.Route(context.Background(), router.RouteRequest{Utterance: test.Utterance, Context: test.Context, Final: true})
		fatalIf(err)
		latencies = append(latencies, result.Trace.TotalLatency)
		if test.ExpectedTool == "" {
			if result.Decision != router.DecisionNoTool {
				falseRoutes++
			}
			continue
		}
		for index, candidate := range result.Candidates {
			if candidate.Tool.ID == test.ExpectedTool {
				if index == 0 {
					top1++
				}
				top5++
				break
			}
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	toolCases, noToolCases := 0, 0
	for _, test := range cases {
		if test.ExpectedTool == "" {
			noToolCases++
		} else {
			toolCases++
		}
	}
	fmt.Printf("cases=%d tool_cases=%d no_tool_cases=%d\n", len(cases), toolCases, noToolCases)
	fmt.Printf("top1=%.2f%% top5=%.2f%% false_route=%.2f%% p50=%s p95=%s\n", percent(top1, toolCases), percent(top5, toolCases), percent(falseRoutes, noToolCases), percentile(latencies, .50), percentile(latencies, .95))
}

func readTools(path string) []router.Tool {
	data, err := os.ReadFile(path)
	fatalIf(err)
	var tools []router.Tool
	fatalIf(json.Unmarshal(data, &tools))
	return tools
}
func readCases(path string) []caseRecord {
	file, err := os.Open(path)
	fatalIf(err)
	defer file.Close()
	var cases []caseRecord
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var test caseRecord
		fatalIf(json.Unmarshal(scanner.Bytes(), &test))
		cases = append(cases, test)
	}
	fatalIf(scanner.Err())
	return cases
}
func percentile(values []time.Duration, quantile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * quantile)
	return values[index]
}
func percent(value, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(value) / float64(total)
}
func fatalIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
