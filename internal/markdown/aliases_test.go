package markdown

import (
	"bytes"
	"git-exporter/internal/gitdata"
	"gopkg.in/yaml.v3"
	"testing"
)

// TestAliasToReplacedField verifies that replacing a managed field preserves custom values referencing its YAML anchor.
func TestAliasToReplacedField(t *testing.T) {
	input := []byte("---\ntitle: &original Old\ncustom: *original\n---\nbody")
	out, err := Transform(input, "p", "a.md", "https://example.test", gitdata.Metadata{})
	if err != nil {
		t.Fatal(err)
	}
	parts := bytes.SplitN(out, []byte("---\n"), 3)
	var fields map[string]any
	if err := yaml.Unmarshal(parts[1], &fields); err != nil {
		t.Fatal(err)
	}
	if fields["custom"] != "Old" || fields["title"] != "a.md" {
		t.Fatalf("%+v", fields)
	}
}
