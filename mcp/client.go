package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MCPClient manages a connection to a single upstream MCP server.
type MCPClient struct {
	Name      string
	Transport string // "stdio"

	// stdio
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	// state
	writeMu   sync.Mutex // protects stdin writes
	stateMu   sync.Mutex // protects tools, status, lastError
	nextID    atomic.Int64
	pending   map[int64]chan rpcResponse
	pendingMu sync.Mutex
	tools     []MCPTool
	status    string // "connecting", "ready", "error", "closed"
	lastError string
}

// NewStdioClient creates an MCP client that communicates via stdin/stdout of a subprocess.
func NewStdioClient(name, command string, args []string, env map[string]string) *MCPClient {
	cmd := exec.Command(command, args...)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	return &MCPClient{
		Name:      name,
		Transport: "stdio",
		cmd:       cmd,
		pending:   make(map[int64]chan rpcResponse),
		status:    "connecting",
	}
}

// Connect starts the subprocess, performs the MCP initialize handshake, and discovers tools.
func (c *MCPClient) Connect(ctx context.Context) error {
	var err error

	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdoutPipe, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	c.stdout = bufio.NewReader(stdoutPipe)

	// Log subprocess stderr via slog
	stderrPipe, err := c.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			slog.Debug("MCP server stderr", "server", c.Name, "line", scanner.Text())
		}
	}()

	if err := c.cmd.Start(); err != nil {
		c.setStatus("error", err.Error())
		return fmt.Errorf("start process: %w", err)
	}

	slog.Info("MCP client: process started", "server", c.Name, "pid", c.cmd.Process.Pid)

	// Start read loop
	go c.readLoop()

	// Initialize handshake
	initResp, err := c.send(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "agent-mesh",
			"version": "0.1.0",
		},
	})
	if err != nil {
		c.setStatus("error", err.Error())
		return fmt.Errorf("initialize: %w", err)
	}
	slog.Info("MCP client: initialized", "server", c.Name, "result", initResp.Result)

	// Send initialized notification (no response expected)
	c.writeRequest(rpcRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	})

	// Discover tools
	toolsResp, err := c.send(ctx, "tools/list", nil)
	if err != nil {
		c.setStatus("error", err.Error())
		return fmt.Errorf("tools/list: %w", err)
	}

	if err := c.parseTools(toolsResp.Result); err != nil {
		c.setStatus("error", err.Error())
		return fmt.Errorf("parse tools: %w", err)
	}

	c.setStatus("ready", "")
	slog.Info("MCP client: ready", "server", c.Name, "tools", len(c.tools))
	return nil
}

// Tools returns the discovered MCP tools.
func (c *MCPClient) Tools() []MCPTool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.tools
}

// Status returns the current connection status.
func (c *MCPClient) Status() (status string, lastError string) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.status, c.lastError
}

// CallTool invokes a tool on the upstream MCP server.
func (c *MCPClient) CallTool(ctx context.Context, name string, arguments map[string]any) (any, error) {
	resp, err := c.send(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": arguments,
	})
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

// Close shuts down the connection and kills the subprocess.
func (c *MCPClient) Close() error {
	c.setStatus("closed", "")
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}
	return nil
}

// send dispatches a JSON-RPC request and waits for the response.
func (c *MCPClient) send(ctx context.Context, method string, params map[string]any) (rpcResponse, error) {
	id := c.nextID.Add(1)
	ch := make(chan rpcResponse, 1)

	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := c.writeRequest(req); err != nil {
		return rpcResponse{}, err
	}

	timeout := 30 * time.Second
	deadline, ok := ctx.Deadline()
	if ok {
		timeout = time.Until(deadline)
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(timeout):
		return rpcResponse{}, fmt.Errorf("timeout waiting for response to %s", method)
	case <-ctx.Done():
		return rpcResponse{}, ctx.Err()
	}
}

func (c *MCPClient) writeRequest(req rpcRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = fmt.Fprintf(c.stdin, "%s\n", data)
	return err
}

func (c *MCPClient) readLoop() {
	for {
		line, err := c.stdout.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				slog.Error("MCP client: read error", "server", c.Name, "error", err)
			}
			c.setStatus("error", "connection lost")
			return
		}

		line = []byte(strings.TrimSpace(string(line)))
		if len(line) == 0 {
			continue
		}

		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			slog.Warn("MCP client: invalid JSON from server", "server", c.Name, "error", err)
			continue
		}

		// Match response to pending request by ID
		if resp.ID != nil {
			id, ok := toInt64(resp.ID)
			if ok {
				c.pendingMu.Lock()
				ch, found := c.pending[id]
				c.pendingMu.Unlock()
				if found {
					ch <- resp
				}
			}
		}
	}
}

func (c *MCPClient) parseTools(result any) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	var wrapper struct {
		Tools []MCPTool `json:"tools"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}
	c.stateMu.Lock()
	c.tools = wrapper.Tools
	c.stateMu.Unlock()
	return nil
}

func (c *MCPClient) setStatus(status, errMsg string) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.status = status
	c.lastError = errMsg
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}
