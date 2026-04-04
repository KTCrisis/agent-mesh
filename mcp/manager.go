package mcp

import (
	"context"
	"fmt"
	"sync"
)

// ServerStatus describes an upstream MCP server's current state.
type ServerStatus struct {
	Name      string   `json:"name"`
	Transport string   `json:"transport"`
	Status    string   `json:"status"`
	Error     string   `json:"error,omitempty"`
	Tools     []string `json:"tools"`
}

// Manager holds all upstream MCP client connections.
type Manager struct {
	mu      sync.RWMutex
	clients map[string]*MCPClient
}

func NewManager() *Manager {
	return &Manager{
		clients: make(map[string]*MCPClient),
	}
}

func (m *Manager) Add(client *MCPClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[client.Name] = client
}

func (m *Manager) Get(name string) *MCPClient {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.clients[name]
}

func (m *Manager) All() []*MCPClient {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*MCPClient, 0, len(m.clients))
	for _, c := range m.clients {
		out = append(out, c)
	}
	return out
}

// ServerStatuses returns the status of all upstream MCP servers.
func (m *Manager) ServerStatuses() any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make([]ServerStatus, 0, len(m.clients))
	for _, c := range m.clients {
		status, lastErr := c.Status()
		tools := c.Tools()
		toolNames := make([]string, len(tools))
		for i, t := range tools {
			toolNames[i] = t.Name
		}
		statuses = append(statuses, ServerStatus{
			Name:      c.Name,
			Transport: c.Transport,
			Status:    status,
			Error:     lastErr,
			Tools:     toolNames,
		})
	}
	return statuses
}

// CallTool forwards a tool call to a specific upstream MCP server.
// Implements the proxy.MCPForwarder interface.
func (m *Manager) CallTool(ctx context.Context, serverName string, toolName string, arguments map[string]any) (any, error) {
	client := m.Get(serverName)
	if client == nil {
		return nil, fmt.Errorf("MCP server not found: %s", serverName)
	}
	return client.CallTool(ctx, toolName, arguments)
}

// CloseAll shuts down all MCP client connections.
func (m *Manager) CloseAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.clients {
		c.Close()
	}
	return nil
}
