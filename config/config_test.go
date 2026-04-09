package config

import (
	"os"
	"testing"
)

func TestLoadBasic(t *testing.T) {
	yaml := `
port: 8080
policies:
  - name: test
    agent: "*"
    rules:
      - tools: ["*"]
        action: allow
`
	f := writeTempFile(t, yaml)
	cfg, err := Load(f)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("port = %d, want 8080", cfg.Port)
	}
	if len(cfg.Policies) != 1 {
		t.Errorf("policies = %d, want 1", len(cfg.Policies))
	}
}

func TestLoadDefaultPort(t *testing.T) {
	yaml := `
policies:
  - name: default
    agent: "*"
    rules:
      - tools: ["*"]
        action: deny
`
	f := writeTempFile(t, yaml)
	cfg, err := Load(f)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("port = %d, want 9090 (default)", cfg.Port)
	}
}

func TestLoadMCPServers(t *testing.T) {
	yaml := `
port: 9090
mcp_servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    env:
      FOO: bar
  - name: remote
    transport: sse
    url: "http://localhost:8080/sse"
    headers:
      Authorization: "Bearer token"
policies: []
`
	f := writeTempFile(t, yaml)
	cfg, err := Load(f)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.MCPServers) != 2 {
		t.Fatalf("mcp_servers = %d, want 2", len(cfg.MCPServers))
	}

	fs := cfg.MCPServers[0]
	if fs.Name != "filesystem" {
		t.Errorf("name = %q, want filesystem", fs.Name)
	}
	if fs.Transport != "stdio" {
		t.Errorf("transport = %q, want stdio", fs.Transport)
	}
	if fs.Command != "npx" {
		t.Errorf("command = %q, want npx", fs.Command)
	}
	if len(fs.Args) != 3 {
		t.Errorf("args = %d, want 3", len(fs.Args))
	}
	if fs.Env["FOO"] != "bar" {
		t.Errorf("env[FOO] = %q, want bar", fs.Env["FOO"])
	}

	remote := cfg.MCPServers[1]
	if remote.Transport != "sse" {
		t.Errorf("transport = %q, want sse", remote.Transport)
	}
	if remote.URL != "http://localhost:8080/sse" {
		t.Errorf("url = %q", remote.URL)
	}
	if remote.Headers["Authorization"] != "Bearer token" {
		t.Errorf("headers[Authorization] = %q", remote.Headers["Authorization"])
	}
}

func TestLoadConditions(t *testing.T) {
	yaml := `
port: 9090
policies:
  - name: limited
    agent: "support-*"
    rules:
      - tools: ["create_refund"]
        action: allow
        condition:
          field: "params.amount"
          operator: "<"
          value: 500
`
	f := writeTempFile(t, yaml)
	cfg, err := Load(f)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rule := cfg.Policies[0].Rules[0]
	if rule.Condition == nil {
		t.Fatal("condition is nil")
	}
	if rule.Condition.Field != "params.amount" {
		t.Errorf("field = %q", rule.Condition.Field)
	}
	if rule.Condition.Operator != "<" {
		t.Errorf("operator = %q", rule.Condition.Operator)
	}
	if rule.Condition.Value != 500 {
		t.Errorf("value = %f", rule.Condition.Value)
	}
}

func TestLoadApprovalConfig(t *testing.T) {
	yaml := `
port: 9090
approval:
  timeout_seconds: 120
policies: []
`
	f := writeTempFile(t, yaml)
	cfg, err := Load(f)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Approval.TimeoutSeconds != 120 {
		t.Errorf("timeout_seconds = %d, want 120", cfg.Approval.TimeoutSeconds)
	}
}

func TestLoadApprovalDefault(t *testing.T) {
	yaml := `
policies: []
`
	f := writeTempFile(t, yaml)
	cfg, err := Load(f)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Approval.TimeoutSeconds != 300 {
		t.Errorf("timeout_seconds = %d, want 300 (default)", cfg.Approval.TimeoutSeconds)
	}
}

func TestLoadCLITools(t *testing.T) {
	yaml := `
policies: []
cli_tools:
  - name: terraform
    bin: terraform
    default_action: human_approval
    commands:
      plan:
        timeout: 120s
      apply:
        allowed_args: ["-target"]
        timeout: 300s
  - name: kubectl
    bin: kubectl
    strict: true
    commands:
      get:
        allowed_args: ["-n", "--namespace"]
`
	f := writeTempFile(t, yaml)
	cfg, err := Load(f)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.CLITools) != 2 {
		t.Fatalf("cli_tools = %d, want 2", len(cfg.CLITools))
	}
	tf := cfg.CLITools[0]
	if tf.Name != "terraform" {
		t.Errorf("name = %q, want terraform", tf.Name)
	}
	if tf.DefaultAction != "human_approval" {
		t.Errorf("default_action = %q", tf.DefaultAction)
	}
	if len(tf.Commands) != 2 {
		t.Errorf("commands = %d, want 2", len(tf.Commands))
	}
	if tf.Commands["apply"].Timeout != "300s" {
		t.Errorf("apply timeout = %q", tf.Commands["apply"].Timeout)
	}
}

func TestLoadCLIToolsValidation(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{"missing name", `
policies: []
cli_tools:
  - bin: terraform
`, true},
		{"missing bin", `
policies: []
cli_tools:
  - name: terraform
`, true},
		{"duplicate name", `
policies: []
cli_tools:
  - name: tf
    bin: terraform
  - name: tf
    bin: terraform
`, true},
		{"invalid default_action", `
policies: []
cli_tools:
  - name: terraform
    bin: terraform
    default_action: maybe
`, true},
		{"strict without commands", `
policies: []
cli_tools:
  - name: kubectl
    bin: kubectl
    strict: true
`, true},
		{"invalid timeout", `
policies: []
cli_tools:
  - name: terraform
    bin: terraform
    commands:
      plan:
        timeout: notaduration
`, true},
		{"valid simple mode", `
policies: []
cli_tools:
  - name: gh
    bin: gh
    default_action: allow
`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := writeTempFile(t, tt.yaml)
			_, err := Load(f)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadFileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(content)
	f.Close()
	return f.Name()
}
