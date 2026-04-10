package supervisor

import (
	"strings"
	"testing"
)

func TestRedactParamsContentKeys(t *testing.T) {
	params := map[string]any{
		"path":    "/tmp/test.go",
		"content": "package main\nfunc main() {}",
	}
	redacted := RedactParams(params)

	// path is short and not a content key — should pass through
	if redacted["path"] != "/tmp/test.go" {
		t.Errorf("path should not be redacted, got %v", redacted["path"])
	}

	// content is a content key — should be redacted regardless of length
	meta, ok := redacted["content"].(ContentMeta)
	if !ok {
		t.Fatalf("content should be ContentMeta, got %T", redacted["content"])
	}
	if meta.ContentLength != len("package main\nfunc main() {}") {
		t.Errorf("content_length = %d, want %d", meta.ContentLength, len("package main\nfunc main() {}"))
	}
	if meta.ContentSHA256 == "" {
		t.Error("content_sha256 should not be empty")
	}
}

func TestRedactParamsLongStrings(t *testing.T) {
	longVal := strings.Repeat("x", 300)
	params := map[string]any{
		"description": longVal,
		"name":        "short",
	}
	redacted := RedactParams(params)

	if _, ok := redacted["description"].(ContentMeta); !ok {
		t.Error("long string should be redacted to ContentMeta")
	}
	if redacted["name"] != "short" {
		t.Error("short string should not be redacted")
	}
}

func TestRedactParamsNested(t *testing.T) {
	params := map[string]any{
		"nested": map[string]any{
			"body": "some content here",
		},
	}
	redacted := RedactParams(params)
	nested := redacted["nested"].(map[string]any)
	if _, ok := nested["body"].(ContentMeta); !ok {
		t.Error("nested content key should be redacted")
	}
}

func TestRedactParamsDeepCopy(t *testing.T) {
	params := map[string]any{
		"content": "original",
	}
	RedactParams(params)

	// Original should not be modified
	if params["content"] != "original" {
		t.Error("RedactParams should not modify the original map")
	}
}

func TestRedactParamsNil(t *testing.T) {
	if RedactParams(nil) != nil {
		t.Error("nil params should return nil")
	}
}

func TestDetectContentTypeJSON(t *testing.T) {
	if got := DetectContentType(`{"key": "value"}`); got != "application/json" {
		t.Errorf("JSON object: got %q, want application/json", got)
	}
	if got := DetectContentType(`[1, 2, 3]`); got != "application/json" {
		t.Errorf("JSON array: got %q, want application/json", got)
	}
}

func TestDetectContentTypeXML(t *testing.T) {
	if got := DetectContentType(`<root><item/></root>`); got != "text/xml" {
		t.Errorf("XML: got %q, want text/xml", got)
	}
}

func TestDetectContentTypeHTML(t *testing.T) {
	if got := DetectContentType(`<!DOCTYPE html><html></html>`); got != "text/html" {
		t.Errorf("HTML: got %q, want text/html", got)
	}
}

func TestDetectContentTypePlain(t *testing.T) {
	if got := DetectContentType("hello world"); got != "text/plain" {
		t.Errorf("plain text: got %q, want text/plain", got)
	}
}

func TestDetectContentTypeEmpty(t *testing.T) {
	if got := DetectContentType(""); got != "text/plain" {
		t.Errorf("empty: got %q, want text/plain", got)
	}
}

func TestDetectContentTypeBinary(t *testing.T) {
	if got := DetectContentType("\x00\x01\x02binary"); got != "application/octet-stream" {
		t.Errorf("binary: got %q, want application/octet-stream", got)
	}
}
