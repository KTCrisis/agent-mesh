package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

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

	r.mu.Lock()
	defer r.mu.Unlock()

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
				Source:      "openapi",
			}

			r.set(tool.Name, tool)
		}
	}

	return nil
}

// buildToolName creates a snake_case name from operationId or method+path.
func buildToolName(method string, path string, op map[string]any) string {
	if opID, ok := op["operationId"].(string); ok && opID != "" {
		return toSnake(opID)
	}
	clean := strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(path)
	clean = strings.Trim(clean, "_")
	return strings.ToLower(method) + "_" + clean
}

func extractParams(op map[string]any) []Param {
	var params []Param

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
	if servers, ok := spec["servers"].([]any); ok && len(servers) > 0 {
		if s, ok := servers[0].(map[string]any); ok {
			if url, ok := s["url"].(string); ok {
				return url
			}
		}
	}
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
			b.WriteRune(c + 32)
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}
