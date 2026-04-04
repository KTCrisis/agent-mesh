package mcp

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
)

type stdioTransport struct {
	name    string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	writeMu sync.Mutex
}

func newStdioTransport(name, command string, args []string, env map[string]string) *stdioTransport {
	cmd := exec.Command(command, args...)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return &stdioTransport{name: name, cmd: cmd}
}

func (t *stdioTransport) Start() error {
	var err error

	t.stdin, err = t.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdoutPipe, err := t.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	t.stdout = bufio.NewReader(stdoutPipe)

	stderrPipe, err := t.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			slog.Debug("MCP server stderr", "server", t.name, "line", scanner.Text())
		}
	}()

	if err := t.cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}

	slog.Info("MCP client: process started", "server", t.name, "pid", t.cmd.Process.Pid)
	return nil
}

func (t *stdioTransport) WriteRequest(data []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	_, err := fmt.Fprintf(t.stdin, "%s\n", data)
	return err
}

func (t *stdioTransport) ReadLoop(onMessage func([]byte)) {
	for {
		line, err := t.stdout.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				slog.Error("MCP client: read error", "server", t.name, "error", err)
			}
			return
		}
		trimmed := trimBytes(line)
		if len(trimmed) == 0 {
			continue
		}
		onMessage(trimmed)
	}
}

func (t *stdioTransport) Close() error {
	if t.stdin != nil {
		t.stdin.Close()
	}
	if t.cmd == nil || t.cmd.Process == nil {
		return nil
	}
	t.cmd.Process.Kill()
	// Non-blocking wait with channel
	done := make(chan struct{})
	go func() {
		t.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-wait5s():
		slog.Warn("MCP client: subprocess did not exit after kill", "server", t.name)
	}
	return nil
}
