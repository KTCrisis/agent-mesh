package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Port       int               `yaml:"port"`
	TraceFile  string            `yaml:"trace_file"`
	Approval   ApprovalConfig    `yaml:"approval"`
	Policies   []Policy          `yaml:"policies"`
	MCPServers []MCPServerConfig `yaml:"mcp_servers"`
}

// ApprovalConfig controls the human approval gate behavior.
type ApprovalConfig struct {
	TimeoutSeconds int `yaml:"timeout_seconds"` // default 300 (5 min)
}

// MCPServerConfig declares an upstream MCP server to connect to.
type MCPServerConfig struct {
	Name      string            `yaml:"name"`
	Transport string            `yaml:"transport"` // "stdio" or "sse"
	Command   string            `yaml:"command,omitempty"`
	Args      []string          `yaml:"args,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	URL       string            `yaml:"url,omitempty"`
	Headers   map[string]string `yaml:"headers,omitempty"`
}

type Policy struct {
	Name  string `yaml:"name"`
	Agent string `yaml:"agent"` // agent ID pattern (* = any)
	Rules []Rule `yaml:"rules"`
}

type Rule struct {
	Tools     []string   `yaml:"tools"`
	Action    string     `yaml:"action"` // allow, deny, human_approval
	Condition *Condition `yaml:"condition,omitempty"`
}

type Condition struct {
	Field    string  `yaml:"field"`    // e.g. "params.amount"
	Operator string  `yaml:"operator"` // <, >, ==, !=
	Value    float64 `yaml:"value"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Port == 0 {
		cfg.Port = 9090
	}
	if cfg.Approval.TimeoutSeconds == 0 {
		cfg.Approval.TimeoutSeconds = 300
	}
	return &cfg, nil
}
