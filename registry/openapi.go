package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Tool represents a callable API operation discovered from an OpenAPI spec.
type Tool struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	BaseURL     string            `json:"base_url"`
	Params      []Param           `json:"params,omitempty"`
	Headers     map[string]string `json:"-"` // backend auth headers (not exposed to agents)
}

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

// LoadOpenAPI fetches an OpenAPI 2.0/3.0 spec and extracts tools.
func (r *Registry) LoadOpenAPI(specURL string, backendURL string, headers map[string]string) error {
	resp, err := http.Get(specURL)
	if err != nil {
		return fmt.Errorf("fetch spec: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read spec: %w", err)
	}

	var spec map[string]any
	if err := json.Unmarshal(body, &spec); err != nil {
		return fmt.Errorf("parse spec: %w", err)
	}

	// Determine base URL
	base := backendURL
	if base == "" {
		base = inferBaseURL(spec)
	}

	// Extract paths → tools
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		return fmt.Errorf("no paths found in spec")
	}

	for path, methods := range paths {
		methodMap, ok := methods.(map[string]any)
		if !ok {
			continue
		}
		for method, opRaw := range methodMap {
			method = strings.ToUpper(method)
			if method == "OPTIONS" || method == "HEAD" {
				continue
			}

			op, ok := opRaw.(map[string]any)
			if !ok {
				continue
			}

			tool := &Tool{
				Name:        buildToolName(method, path, op),
				Description: strVal(op, "summary", strVal(op, "description", "")),
				Method:      method,
				Path:        path,
				BaseURL:     base,
				Headers:     headers,
				Params:      extractParams(op),
			}

			r.tools[tool.Name] = tool
		}
	}

	return nil
}

// LoadManual registers a tool manually (for non-OpenAPI backends).
func (r *Registry) LoadManual(tool *Tool) {
	r.tools[tool.Name] = tool
}

// buildToolName creates a snake_case name from operationId or method+path.
func buildToolName(method string, path string, op map[string]any) string {
	if opID, ok := op["operationId"].(string); ok && opID != "" {
		return toSnake(opID)
	}
	// Fallback: get_users, post_orders, delete_order_by_id
	clean := strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(path)
	clean = strings.Trim(clean, "_")
	return strings.ToLower(method) + "_" + clean
}

func extractParams(op map[string]any) []Param {
	var params []Param

	// OpenAPI 2.0 / 3.0 parameters
	if rawParams, ok := op["parameters"].([]any); ok {
		for _, rp := range rawParams {
			p, ok := rp.(map[string]any)
			if !ok {
				continue
			}
			param := Param{
				Name:     strVal(p, "name", ""),
				In:       strVal(p, "in", "query"),
				Required: boolVal(p, "required"),
			}
			if schema, ok := p["schema"].(map[string]any); ok {
				param.Type = strVal(schema, "type", "string")
			} else {
				param.Type = strVal(p, "type", "string")
			}
			if param.Name != "" {
				params = append(params, param)
			}
		}
	}

	// OpenAPI 3.0 requestBody → treat as a "body" param
	if _, ok := op["requestBody"]; ok {
		params = append(params, Param{
			Name:     "body",
			In:       "body",
			Type:     "object",
			Required: true,
		})
	}

	return params
}

func inferBaseURL(spec map[string]any) string {
	// OpenAPI 3.0: servers[0].url
	if servers, ok := spec["servers"].([]any); ok && len(servers) > 0 {
		if s, ok := servers[0].(map[string]any); ok {
			if url, ok := s["url"].(string); ok {
				return url
			}
		}
	}
	// OpenAPI 2.0: host + basePath
	host := strVal(spec, "host", "localhost")
	basePath := strVal(spec, "basePath", "")
	scheme := "https"
	if schemes, ok := spec["schemes"].([]any); ok && len(schemes) > 0 {
		if s, ok := schemes[0].(string); ok {
			scheme = s
		}
	}
	return scheme + "://" + host + basePath
}

func strVal(m map[string]any, key string, fallback string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return fallback
}

func boolVal(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func toSnake(s string) string {
	var b strings.Builder
	for i, c := range s {
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(c + 32) // toLower
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}
