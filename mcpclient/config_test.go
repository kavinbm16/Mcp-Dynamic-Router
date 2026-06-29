package mcpclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "mcp.toml")
	contents := `[elicitation]
enabled = true
auto_accept = false
timeout = 30

[[servers]]
name = "weather-service"
url = "http://localhost:23202/mcp"
transport = "streamable-http"

[[servers]]
name = "wellness-service"
url = "http://localhost:23002/mcp"
`
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Servers) != 2 || config.Servers[1].Transport != "streamable-http" {
		t.Fatalf("unexpected config: %+v", config)
	}
	if !config.Elicitation.Enabled || config.Elicitation.Duration().Seconds() != 30 {
		t.Fatalf("unexpected elicitation config: %+v", config.Elicitation)
	}
}

func TestLoadConfigRejectsSSE(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.toml")
	if err := os.WriteFile(configPath, []byte("[[servers]]\nname='old'\nurl='http://localhost/mcp'\ntransport='sse'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(configPath); err == nil {
		t.Fatal("expected deprecated SSE transport to be rejected")
	}
}
