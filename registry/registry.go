package registry

// Tool represents a callable operation discovered from an OpenAPI spec or MCP server.
type Tool struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Method      string            `json:"method"`              // HTTP method (empty for MCP tools)
	Path        string            `json:"path"`                // HTTP path (empty for MCP tools)
	BaseURL     string            `json:"base_url"`            // backend URL (empty for MCP tools)
	Params      []Param           `json:"params,omitempty"`
	Headers     map[string]string `json:"-"`
	Source      string            `json:"source"`              // "openapi" or "mcp"
	MCPServer   string            `json:"mcp_server,omitempty"`
}

// Param describes a single parameter for a tool.
type Param struct {
	Name     string `json:"name"`
	In       string `json:"in"`       // path, query, body
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// Registry holds all discovered tools.
type Registry struct {
	tools map[string]*Tool
}

func New() *Registry {
	return &Registry{tools: make(map[string]*Tool)}
}

func (r *Registry) Get(name string) *Tool {
	return r.tools[name]
}

func (r *Registry) All() []*Tool {
	out := make([]*Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// Remove deletes a tool by name.
func (r *Registry) Remove(name string) {
	delete(r.tools, name)
}

// LoadManual registers a tool manually (for non-OpenAPI backends).
func (r *Registry) LoadManual(tool *Tool) {
	r.tools[tool.Name] = tool
}
