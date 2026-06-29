package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	dynamicrouter "github.com/kavinbm16/mcp-dynamic-router"
	"github.com/kavinbm16/mcp-dynamic-router/router"
)

func TestRouteEndpoint(t *testing.T) {
	handler := testAPI(t)
	defer handler.close()
	request := httptest.NewRequest(http.MethodPost, "/v1/route", bytes.NewBufferString(`{"utterance":"weather forecast Bengaluru","final":true}`))
	recorder := httptest.NewRecorder()
	handler.route(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var result router.RouteResult
	if err := json.NewDecoder(recorder.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Decision != router.DecisionSelected || result.Candidates[0].Tool.ID != "weather.forecast" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestStreamEndpointMaintainsStabilityAndClosesFinalSession(t *testing.T) {
	handler := testAPI(t)
	defer handler.close()
	first := postStream(t, handler, `{"session_id":"call-1","transcript":"weather forecast Bengaluru","confidence":0.9,"final":false}`)
	if first.Stable {
		t.Fatal("first prediction should not be stable")
	}
	second := postStream(t, handler, `{"session_id":"call-1","transcript":"weather forecast Bengaluru tomorrow","confidence":0.95,"final":false}`)
	if !second.Stable {
		t.Fatalf("second route should be stable: %+v", second)
	}
	_ = postStream(t, handler, `{"session_id":"call-1","transcript":"weather forecast Bengaluru tomorrow morning","confidence":0.98,"final":true}`)
	handler.mu.Lock()
	_, exists := handler.sessions["call-1"]
	handler.mu.Unlock()
	if exists {
		t.Fatal("final transcript should close the stream session")
	}
}

func postStream(t *testing.T, handler *api, body string) struct {
	Stable bool `json:"stable"`
} {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/v1/stream", bytes.NewBufferString(body))
	recorder := httptest.NewRecorder()
	handler.stream(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Stable bool `json:"stable"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	return response
}

func testAPI(t *testing.T) *api {
	t.Helper()
	app := dynamicrouter.New(dynamicrouter.Options{})
	tool := router.Tool{ID: "weather.forecast", Server: "weather", Name: "forecast", Domain: "weather", ReadOnly: true, Description: "Domain: weather\nPurpose: Get a weather forecast.\nInvoke when: Call for weather, rain, or temperature.\nParameters: location is a city.\nLimitations: No historical data.\nExample: weather forecast Bengaluru tomorrow", InputSchema: json.RawMessage(`{"type":"object"}`)}
	if err := app.Registry.ReplaceServer("weather", []router.Tool{tool}); err != nil {
		t.Fatal(err)
	}
	if err := app.Router.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return &api{app: app, sessions: make(map[string]*streamEntry)}
}
