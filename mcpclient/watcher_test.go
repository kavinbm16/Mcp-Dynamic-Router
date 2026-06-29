package mcpclient

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kavinbm16/mcp-dynamic-router/router"
)

func TestConfigWatcherHotReload(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.toml")

	// 1. Write initial configuration with a nonexistent/failing port to avoid actual connection delays
	initialContent := `
[[servers]]
name = "server-one"
url = "http://127.0.0.1:9999/mcp"
transport = "streamable-http"
`
	if err := os.WriteFile(configPath, []byte(initialContent), 0o600); err != nil {
		t.Fatal(err)
	}

	registry := router.NewRegistry()
	var mu sync.Mutex
	changedCount := 0
	onToolsChanged := func(ctx context.Context) error {
		mu.Lock()
		changedCount++
		mu.Unlock()
		return nil
	}

	client := New(registry, onToolsChanged)

	// Stub out the Connect function to avoid actual network dialing in tests
	// We want to test the reload mechanics and file watcher triggers.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Initial connect should register the watcher
	_, _ = client.ConnectFile(ctx, configPath)
	defer client.Close()

	mu.Lock()
	initialChanges := changedCount
	mu.Unlock()

	// 2. Modify config contents and write it back, forcing a mod-time change
	updatedContent := `
[[servers]]
name = "server-one"
url = "http://127.0.0.1:9999/mcp"
transport = "streamable-http"

[[servers]]
name = "server-two"
url = "http://127.0.0.1:9998/mcp"
transport = "streamable-http"
`
	// Sleep briefly to ensure filesystem modification time actually advances
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(configPath, []byte(updatedContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// 3. Wait for watcher to trigger (polling interval is 2 seconds)
	triggered := false
	for i := 0; i < 20; i++ {
		time.Sleep(200 * time.Millisecond)
		mu.Lock()
		currentChanges := changedCount
		mu.Unlock()
		if currentChanges > initialChanges {
			triggered = true
			break
		}
	}

	if !triggered {
		t.Fatal("expected config watcher to detect file modification and reload configurations")
	}
}
