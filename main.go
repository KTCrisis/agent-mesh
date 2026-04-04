package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KTCrisis/agent-mesh/config"
	"github.com/KTCrisis/agent-mesh/mcp"
	"github.com/KTCrisis/agent-mesh/policy"
	"github.com/KTCrisis/agent-mesh/proxy"
	"github.com/KTCrisis/agent-mesh/registry"
	"github.com/KTCrisis/agent-mesh/trace"
)

func main() {
	// Subcommand: discover
	if len(os.Args) > 1 && os.Args[1] == "discover" {
		runDiscover(os.Args[2:])
		return
	}

	configPath := flag.String("config", "policies.yaml", "Path to config/policies YAML")
	specURL := flag.String("openapi", "", "OpenAPI spec URL to load")
	backendURL := flag.String("backend", "", "Backend base URL (overrides spec)")
	port := flag.Int("port", 0, "Port override (default from config or 9090)")
	mcpMode := flag.Bool("mcp", false, "Run as MCP server (stdio JSON-RPC instead of HTTP)")
	mcpAgent := flag.String("mcp-agent", "claude", "Agent ID for MCP mode policy evaluation")
	flag.Parse()

	// Setup structured logging
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	// 1. Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "path", *configPath, "error", err)
		os.Exit(1)
	}
	slog.Info("config loaded", "policies", len(cfg.Policies))

	if *port > 0 {
		cfg.Port = *port
	}

	// 2. Build registry
	reg := registry.New()

	if *specURL != "" {
		slog.Info("loading OpenAPI spec", "url", *specURL)
		if err := reg.LoadOpenAPI(*specURL, *backendURL, nil); err != nil {
			slog.Error("failed to load OpenAPI spec", "error", err)
			os.Exit(1)
		}
		slog.Info("registry loaded", "tools", len(reg.All()))
		for _, t := range reg.All() {
			slog.Info("  tool registered", "name", t.Name, "method", t.Method, "path", t.Path)
		}
	} else {
		slog.Info("no OpenAPI spec provided — use --openapi to load REST tools")
	}

	// 3. Build policy engine
	pol := policy.NewEngine(cfg.Policies)
	slog.Info("policy engine ready", "policies", len(cfg.Policies))

	// 4. Build trace store
	traces := trace.NewStore(10000)

	// 5. Build handler
	handler := proxy.NewHandler(reg, pol, traces)

	// 6. Connect upstream MCP servers
	var mcpManager *mcp.Manager
	if len(cfg.MCPServers) > 0 {
		mcpManager = mcp.NewManager()
		handler.MCPForwarder = mcpManager

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		for _, serverCfg := range cfg.MCPServers {
			switch serverCfg.Transport {
			case "stdio":
				client := mcp.NewStdioClient(serverCfg.Name, serverCfg.Command, serverCfg.Args, serverCfg.Env)
				if err := client.Connect(ctx); err != nil {
					slog.Error("failed to connect MCP server", "name", serverCfg.Name, "error", err)
					continue
				}
				mcpManager.Add(client)

				// Register discovered tools into the shared registry
				defs := convertMCPTools(client.Tools())
				reg.LoadMCP(serverCfg.Name, defs)
				for _, d := range defs {
					slog.Info("  MCP tool registered", "name", serverCfg.Name+"."+d.Name, "server", serverCfg.Name)
				}
			default:
				slog.Error("unsupported MCP transport", "name", serverCfg.Name, "transport", serverCfg.Transport)
			}
		}
		cancel()

		slog.Info("MCP upstream servers connected", "count", len(mcpManager.All()))
	}

	// 7. MCP mode or HTTP mode
	if *mcpMode {
		// MCP: JSON-RPC over stdio — logs go to stderr to keep stdout clean
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
		server := &mcp.Server{
			Registry: reg,
			Policy:   pol,
			Traces:   traces,
			Handler:  handler,
			AgentID:  *mcpAgent,
		}
		if err := server.Run(); err != nil {
			slog.Error("MCP server failed", "error", err)
			os.Exit(1)
		}
	} else {
		addr := fmt.Sprintf(":%d", cfg.Port)
		slog.Info("agent-mesh sidecar starting", "addr", addr)
		slog.Info("endpoints",
			"tool_call", fmt.Sprintf("POST http://localhost%s/tool/{name}", addr),
			"list_tools", fmt.Sprintf("GET  http://localhost%s/tools", addr),
			"mcp_servers", fmt.Sprintf("GET  http://localhost%s/mcp-servers", addr),
			"traces", fmt.Sprintf("GET  http://localhost%s/traces", addr),
			"health", fmt.Sprintf("GET  http://localhost%s/health", addr),
		)

		// Graceful shutdown: close MCP clients on SIGINT/SIGTERM
		srv := &http.Server{Addr: addr, Handler: handler}
		go func() {
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			<-sigCh
			slog.Info("shutting down...")
			if mcpManager != nil {
				mcpManager.CloseAll()
			}
			srv.Shutdown(context.Background())
		}()

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}
}

// convertMCPTools bridges mcp.MCPTool → registry.MCPToolDef.
func convertMCPTools(tools []mcp.MCPTool) []registry.MCPToolDef {
	defs := make([]registry.MCPToolDef, 0, len(tools))
	for _, t := range tools {
		props := make(map[string]registry.MCPPropDef, len(t.InputSchema.Properties))
		for name, p := range t.InputSchema.Properties {
			props[name] = registry.MCPPropDef{Type: p.Type}
		}
		defs = append(defs, registry.NewMCPToolDef(t.Name, t.Description, props, t.InputSchema.Required))
	}
	return defs
}
