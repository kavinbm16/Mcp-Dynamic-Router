package dynamicrouter

import (
	"context"
	"strings"

	"github.com/kavinbm16/mcp-dynamic-router/mcpclient"
	"github.com/kavinbm16/mcp-dynamic-router/router"
	"github.com/kavinbm16/mcp-dynamic-router/streamrag"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Options struct {
	MCPConfigPath      string
	RouterConfig       router.Config
	Embedder           router.Embedder
	Reranker           router.Reranker
	ElicitationHandler mcpclient.ElicitationHandler
}

// App is the plug-and-play facade. It owns discovery, the routing index,
// Stream RAG sessions, and MCP connections.
type App struct {
	Registry *router.Registry
	Router   *router.Router
	MCP      *mcpclient.Client
	config   string
}

func New(options Options) *App {
	config := options.RouterConfig
	if config.MaxDomainTools == 0 && config.DomainTopK == 0 && config.CandidateTopK == 0 {
		config = router.DefaultConfig()
	}
	registry := router.NewRegistry()
	routerOptions := []router.Option{}
	if options.Reranker != nil {
		routerOptions = append(routerOptions, router.WithReranker(options.Reranker))
	}
	engine := router.New(registry, options.Embedder, config, routerOptions...)
	clientOptions := []mcpclient.Option{}
	if options.ElicitationHandler != nil {
		clientOptions = append(clientOptions, mcpclient.WithElicitationHandler(options.ElicitationHandler))
	}
	client := mcpclient.New(registry, engine.Refresh, clientOptions...)
	configPath := strings.TrimSpace(options.MCPConfigPath)
	if configPath == "" {
		configPath = "mcp.toml"
	}
	return &App{Registry: registry, Router: engine, MCP: client, config: configPath}
}

func (a *App) Start(ctx context.Context) (mcpclient.ConnectReport, error) {
	return a.MCP.ConnectFile(ctx, a.config)
}

func (a *App) Route(ctx context.Context, request router.RouteRequest) (router.RouteResult, error) {
	return a.Router.Route(ctx, request)
}

func (a *App) Call(ctx context.Context, tool router.Tool, arguments map[string]any) (*mcp.CallToolResult, error) {
	return a.MCP.Call(ctx, tool, arguments)
}

func (a *App) NewStream(options streamrag.Options, hooks streamrag.Hooks) *streamrag.Session {
	return streamrag.New(a.Router, options, hooks)
}

func (a *App) Close() error { return a.MCP.Close() }
