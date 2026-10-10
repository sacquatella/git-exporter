package markdown

import (
	"bytes"
	"git-exporter/internal/gitdata"
	"gopkg.in/yaml.v3"
	"testing"
	"time"
)

// TestTransformPreservesBodyAndCustomFields checks body and custom-field preservation,
// managed metadata, stable output, and project-specific IDs.
func TestTransformPreservesBodyAndCustomFields(t *testing.T) {
	body := []byte("# Title\r\n\r\nExact body.\r\n")
	input := append([]byte("---\r\ntitle: Old\r\ncustom:\r\n  nested: true\r\ntags: [old]\r\n---\r\n"), body...)
	when := time.Date(2020, 1, 1, 12, 0, 0, 0, time.FixedZone("Paris", 3600))
	meta := gitdata.Metadata{Created: when, Updated: when, Committer: "Last committer"}
	result, err := Transform(input, "project", "docs/test.md", "https://example.test/file", meta)
	if err != nil {
		t.Fatal(err)
	}
	parts := bytes.SplitN(result, []byte("---\n"), 3)
	if len(parts) != 3 || !bytes.Equal(parts[2], body) {
		t.Fatalf("body changed: %q", result)
	}
	var header map[string]any
	if err := yaml.Unmarshal(parts[1], &header); err != nil {
		t.Fatal(err)
	}
	if header["title"] != "docs/test.md" || header["visibility"] != "Internal" || header["creation_date"] != "2020-01-01T11:00:00Z" {
		t.Fatalf("%+v", header)
	}
	if header["custom"].(map[string]any)["nested"] != true {
		t.Fatal("custom field lost")
	}
	again, err := Transform(result, "project", "docs/test.md", "https://example.test/file", meta)
	if err != nil || !bytes.Equal(result, again) {
		t.Fatalf("not idempotent: %v", err)
	}
	other, _ := Transform(input, "other", "docs/test.md", "https://example.test/file", meta)
	if bytes.Contains(other, []byte(header["doc_id"].(string))) {
		t.Fatal("project identities collide")
	}
}

// TestMalformedFrontmatter checks rejection of malformed or unsupported frontmatter and acceptance of valid empty inputs.
func TestMalformedFrontmatter(t *testing.T) {
	for _, input := range []string{"---\ntitle: [\n---\nbody", "---\nno closing", "+++\ntitle='toml'\n+++\n", "---\n- list\n---\n", "---\ntitle: one\ntitle: two\n---\n", "{\"title\":\"json\"}\n"} {
		if _, err := Transform([]byte(input), "p", "a.md", "https://example.test", gitdata.Metadata{}); err == nil {
			t.Fatalf("accepted invalid input %q", input)
		}
	}
	for _, input := range []string{"", "body", "---\n---\nbody", "---\ncustom: true\n---"} {
		if _, err := Transform([]byte(input), "p", "a.md", "https://example.test", gitdata.Metadata{}); err != nil {
			t.Fatalf("%q: %v", input, err)
		}
	}
}

// TestSiteURL checks provider defaults, custom templates, escaped paths, and missing URL configuration errors.
func TestSiteURL(t *testing.T) {
	for _, test := range []struct{ remote, template, want string }{
		{"git@github.com:org/repo.git", "", "https://github.com/org/repo/blob/abc/docs/a%20%23%C3%A9.md"},
		{"https://gitlab.com/group/sub/repo.git", "", "https://gitlab.com/group/sub/repo/-/blob/abc/docs/a%20%23%C3%A9.md"},
		{"ssh://git@git.private.test:2222/org/repo.git", "{repo_url}/src/{commit}/{path}", "https://git.private.test/org/repo/src/abc/docs/a%20%23%C3%A9.md"},
		{"", "https://docs.example.test/{commit}/{path}", "https://docs.example.test/abc/docs/a%20%23%C3%A9.md"},
	} {
		value, err := SiteURL(test.remote, test.template, "abc", "docs/a #é.md")
		if err != nil || value != test.want {
			t.Fatalf("%s %v, want %s", value, err, test.want)
		}
	}
	if _, err := SiteURL("https://private.test/repo.git", "", "abc", "a.md"); err == nil {
		t.Fatal("private host needs template")
	}
	if _, err := SiteURL("", "{repo_url}/blob/{commit}/{path}", "abc", "a.md"); err == nil {
		t.Fatal("missing origin accepted")
	}
}
