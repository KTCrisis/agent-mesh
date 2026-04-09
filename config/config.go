package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Port       int               `yaml:"port"`
	TraceFile  string            `yaml:"trace_file"`
	Approval   ApprovalConfig    `yaml:"approval"`
	Policies   []Policy          `yaml:"policies"`
	MCPServers []MCPServerConfig `yaml:"mcp_servers"`
	CLITools   []CLIToolConfig   `yaml:"cli_tools"`
}

// CLIToolConfig declares a CLI binary to wrap as governed tools.
type CLIToolConfig struct {
	Name          string                      `yaml:"name"`
	Bin           string                      `yaml:"bin"`
	DefaultAction string                      `yaml:"default_action"` // allow, deny, human_approval (default: deny)
	Strict        bool                        `yaml:"strict"`         // only declared commands allowed
	WorkingDir    string                      `yaml:"working_dir,omitempty"`
	Env           map[string]string           `yaml:"env,omitempty"`
	Commands      map[string]CLICommandConfig `yaml:"commands,omitempty"`
}

// CLICommandConfig declares a specific subcommand with constraints.
type CLICommandConfig struct {
	AllowedArgs []string `yaml:"allowed_args,omitempty"`
	Timeout     string   `yaml:"timeout,omitempty"` // e.g. "30s", "5m"
}

// ApprovalConfig controls the human approval gate behavior.
type ApprovalConfig struct {
	TimeoutSeconds int    `yaml:"timeout_seconds"` // default 300 (5 min)
	NotifyURL      string `yaml:"notify_url"`      // webhook URL for new pending approvals
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
	Name      string     `yaml:"name"`
	Agent     string     `yaml:"agent"` // agent ID pattern (* = any)
	RateLimit *RateLimit `yaml:"rate_limit,omitempty"`
	Rules     []Rule     `yaml:"rules"`
}

// RateLimit defines per-agent call constraints.
type RateLimit struct {
	MaxPerMinute int `yaml:"max_per_minute"` // max calls per sliding minute window
	MaxTotal     int `yaml:"max_total"`      // max total calls (lifetime of process)
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
	if err := cfg.validateCLITools(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) validateCLITools() error {
	seen := make(map[string]bool, len(c.CLITools))
	for i, ct := range c.CLITools {
		if ct.Name == "" {
			return fmt.Errorf("cli_tools[%d]: name is required", i)
		}
		if ct.Bin == "" {
			return fmt.Errorf("cli_tools[%d] (%s): bin is required", i, ct.Name)
		}
		if seen[ct.Name] {
			return fmt.Errorf("cli_tools[%d]: duplicate name %q", i, ct.Name)
		}
		seen[ct.Name] = true

		switch ct.DefaultAction {
		case "", "allow", "deny", "human_approval":
			// ok — empty defaults to "deny" at runtime
		default:
			return fmt.Errorf("cli_tools[%d] (%s): invalid default_action %q", i, ct.Name, ct.DefaultAction)
		}

		if ct.Strict && len(ct.Commands) == 0 {
			return fmt.Errorf("cli_tools[%d] (%s): strict mode requires at least one declared command", i, ct.Name)
		}

		for cmdName, cmd := range ct.Commands {
			if cmd.Timeout != "" {
				if _, err := time.ParseDuration(cmd.Timeout); err != nil {
					return fmt.Errorf("cli_tools[%d] (%s): command %q has invalid timeout %q: %w", i, ct.Name, cmdName, cmd.Timeout, err)
				}
			}
		}
	}
	return nil
}
