package router

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Snapshot struct {
	Version uint64 `json:"version"`
	Tools   []Tool `json:"tools"`
}

// Registry supports atomic server-level replacement, which matches MCP's
// tools/list_changed model and prevents partially updated registries.
type Registry struct {
	mu      sync.RWMutex
	version uint64
	tools   map[string]Tool
}

func NewRegistry() *Registry { return &Registry{tools: make(map[string]Tool)} }

func (r *Registry) ReplaceServer(server string, tools []Tool) error {
	server = strings.TrimSpace(server)
	if server == "" {
		return fmt.Errorf("server name is required")
	}

	next := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		tool.Server = server
		tool.Name = strings.TrimSpace(tool.Name)
		if tool.Name == "" {
			return fmt.Errorf("server %q contains a tool without a name", server)
		}
		if tool.ID == "" {
			tool.ID = server + "." + tool.Name
		}
		if _, exists := next[tool.ID]; exists {
			return fmt.Errorf("duplicate tool id %q", tool.ID)
		}
		next[tool.ID] = cloneTool(tool)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for id, tool := range r.tools {
		if tool.Server == server {
			delete(r.tools, id)
		}
	}
	for id, tool := range next {
		r.tools[id] = tool
	}
	r.version++
	return nil
}

func (r *Registry) RemoveServer(server string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := false
	for id, tool := range r.tools {
		if tool.Server == server {
			delete(r.tools, id)
			changed = true
		}
	}
	if changed {
		r.version++
	}
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, cloneTool(tool))
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].ID < tools[j].ID })
	return Snapshot{Version: r.version, Tools: tools}
}

func cloneTool(tool Tool) Tool {
	tool.InputSchema = append([]byte(nil), tool.InputSchema...)
	return tool
}
