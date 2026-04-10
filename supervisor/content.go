package supervisor

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// ContentMeta replaces raw content when expose_content is false.
type ContentMeta struct {
	ContentLength       int    `json:"content_length"`
	ContentSHA256       string `json:"content_sha256"`
	ContentTypeDetected string `json:"content_type_detected"`
}

// contentKeys are param keys whose values are treated as content.
var contentKeys = map[string]bool{
	"content":      true,
	"body":         true,
	"data":         true,
	"file_content": true,
	"text":         true,
}

// contentThreshold is the minimum string length to trigger redaction
// for non-content keys.
const contentThreshold = 256

// RedactParams returns a deep copy of params with large or content-like
// string values replaced by ContentMeta. The original map is not modified.
func RedactParams(params map[string]any) map[string]any {
	if params == nil {
		return nil
	}
	result := make(map[string]any, len(params))
	for k, v := range params {
		result[k] = redactValue(k, v)
	}
	return result
}

func redactValue(key string, v any) any {
	switch val := v.(type) {
	case string:
		if contentKeys[strings.ToLower(key)] || len(val) >= contentThreshold {
			return ContentMeta{
				ContentLength:       len(val),
				ContentSHA256:       fmt.Sprintf("%x", sha256.Sum256([]byte(val))),
				ContentTypeDetected: DetectContentType(val),
			}
		}
		return val
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, v2 := range val {
			out[k] = redactValue(k, v2)
		}
		return out
	default:
		return v
	}
}

// DetectContentType returns a MIME type guess based on content prefix.
func DetectContentType(data string) string {
	trimmed := strings.TrimSpace(data)
	if len(trimmed) == 0 {
		return "text/plain"
	}

	// JSON
	if trimmed[0] == '{' || trimmed[0] == '[' {
		var js json.RawMessage
		if json.Unmarshal([]byte(trimmed), &js) == nil {
			return "application/json"
		}
	}

	// XML/HTML
	if trimmed[0] == '<' {
		if strings.HasPrefix(strings.ToLower(trimmed), "<!doctype html") ||
			strings.HasPrefix(strings.ToLower(trimmed), "<html") {
			return "text/html"
		}
		return "text/xml"
	}

	// Check for binary content (non-printable bytes in first 512 bytes)
	check := data
	if len(check) > 512 {
		check = check[:512]
	}
	for _, b := range []byte(check) {
		if b < 0x09 || (b > 0x0d && b < 0x20 && b != 0x1b) {
			return "application/octet-stream"
		}
	}

	return "text/plain"
}
