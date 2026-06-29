package router

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAuditToolAcceptsStructuredDescription(t *testing.T) {
	tool := weatherTool()
	report := AuditTool(tool, DefaultDescriptionPolicy())
	if report.Score != 100 || len(report.Issues) != 0 {
		t.Fatalf("expected perfect audit, got score=%d issues=%v", report.Score, report.Issues)
	}
}

func TestAuditToolFindsMissingSectionsAndParameterDocs(t *testing.T) {
	tool := Tool{ID: "calendar.create", Name: "create_event", Description: "Creates an event.", InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`)}
	report := AuditTool(tool, DefaultDescriptionPolicy())
	if report.Score >= 70 {
		t.Fatalf("expected a poor score, got %d", report.Score)
	}
	if len(report.Issues) < 6 {
		t.Fatalf("expected multiple actionable issues, got %v", report.Issues)
	}
}

func TestRegistryReplaceServerIsAtomic(t *testing.T) {
	registry := NewRegistry()
	if err := registry.ReplaceServer("one", []Tool{{Name: "first"}, {Name: "second"}}); err != nil {
		t.Fatal(err)
	}
	if err := registry.ReplaceServer("one", []Tool{{Name: "third"}}); err != nil {
		t.Fatal(err)
	}
	snapshot := registry.Snapshot()
	if snapshot.Version != 2 || len(snapshot.Tools) != 1 || snapshot.Tools[0].ID != "one.third" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestRouterUsesDomainThenSelectsTool(t *testing.T) {
	registry := NewRegistry()
	tools := []Tool{
		weatherTool(),
		{ID: "calendar.create_event", Server: "calendar", Domain: "calendar", Name: "create_event", Description: structured("calendar", "Create a calendar event", "the user asks to schedule a meeting", "title and start time", "Does not find weather", "Schedule lunch tomorrow at noon"), InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	if err := registry.ReplaceServer("weather", tools[:1]); err != nil {
		t.Fatal(err)
	}
	if err := registry.ReplaceServer("calendar", tools[1:]); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.DomainTopK = 1
	router := New(registry, nil, config)
	if err := router.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := router.Route(context.Background(), RouteRequest{Utterance: "Will I need an umbrella in Bengaluru today?", Final: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionSelected || result.Candidates[0].Tool.ID != "weather.get_forecast" {
		t.Fatalf("unexpected route: %+v", result)
	}
	if len(result.Trace.Domains) != 1 || result.Trace.Domains[0] != "weather" {
		t.Fatalf("unexpected domains: %v", result.Trace.Domains)
	}
}

func TestRouterAbstainsForUnrelatedQuery(t *testing.T) {
	registry := NewRegistry()
	if err := registry.ReplaceServer("weather", []Tool{weatherTool()}); err != nil {
		t.Fatal(err)
	}
	router := New(registry, nil, DefaultConfig())
	if err := router.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := router.Route(context.Background(), RouteRequest{Utterance: "Tell me a bedtime story"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionNoTool {
		t.Fatalf("expected no_tool, got %+v", result)
	}
}

func TestValidateArguments(t *testing.T) {
	tool := weatherTool()
	errors := ValidateArguments(tool, map[string]any{"location": 42, "units": "kelvin"})
	if len(errors) != 2 {
		t.Fatalf("expected two errors, got %v", errors)
	}
	if errors[0].Field != "location" || errors[1].Field != "units" {
		t.Fatalf("unexpected errors: %v", errors)
	}
	if errors := ValidateArguments(tool, map[string]any{"location": "Bengaluru", "units": "metric"}); len(errors) != 0 {
		t.Fatalf("unexpected errors: %v", errors)
	}
}

func weatherTool() Tool {
	return Tool{ID: "weather.get_forecast", Server: "weather", Domain: "weather", Name: "get_forecast", Description: structured("weather", "Get the current weather and short-term forecast for a location", "the user asks about temperature, rain, or forecast", "location is a city; units is metric or imperial", "Does not provide historical climate data", "Will I need an umbrella in Bengaluru today?"), InputSchema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string","description":"City or place name"},"units":{"type":"string","description":"Unit system","enum":["metric","imperial"]}},"required":["location"],"additionalProperties":false}`), ReadOnly: true}
}

func structured(domain, purpose, invoke, parameters, limitations, example string) string {
	return "Domain: " + domain + "\nPurpose: " + purpose + ".\nInvoke when: Call this tool when " + invoke + ".\nParameters: " + parameters + ".\nLimitations: " + limitations + ".\nExample: User: " + example + " Arguments: {}"
}
