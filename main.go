package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/KTCrisis/agent-mesh/config"
	"github.com/KTCrisis/agent-mesh/mcp"
	"github.com/KTCrisis/agent-mesh/policy"
	"github.com/KTCrisis/agent-mesh/proxy"
	"github.com/KTCrisis/agent-mesh/registry"
	"github.com/KTCrisis/agent-mesh/trace"
)

func main() {
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
		slog.Warn("no OpenAPI spec provided — registry is empty. Use --openapi to load tools.")
	}

	// 3. Build policy engine
	pol := policy.NewEngine(cfg.Policies)
	slog.Info("policy engine ready", "policies", len(cfg.Policies))

	// 4. Build trace store
	traces := trace.NewStore(10000)

	// 5. Build handler
	handler := proxy.NewHandler(reg, pol, traces)

	// 6. MCP mode or HTTP mode
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
			"traces", fmt.Sprintf("GET  http://localhost%s/traces", addr),
			"health", fmt.Sprintf("GET  http://localhost%s/health", addr),
		)

		if err := http.ListenAndServe(addr, handler); err != nil {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}
}
