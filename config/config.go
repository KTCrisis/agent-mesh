package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Port     int      `yaml:"port"`
	Policies []Policy `yaml:"policies"`
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
	return &cfg, nil
}
