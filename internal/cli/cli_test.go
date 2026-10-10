package cli

import (
	"bytes"
	"git-exporter/internal/testrepo"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestVersionFlag verifies that both native Cobra version flags display the configured version.
func TestVersionFlag(t *testing.T) {
	originalVersion := Version
	t.Cleanup(func() { Version = originalVersion })
	for _, version := range []string{"dev", "v1.2.3"} {
		for _, flag := range []string{"--version", "-v"} {
			t.Run(version+"/"+flag, func(t *testing.T) {
				Version = version
				command := New()
				command.SetArgs([]string{flag})
				var output bytes.Buffer
				command.SetOut(&output)
				command.SetErr(&output)
				if err := command.Execute(); err != nil {
					t.Fatal(err)
				}
				if got := output.String(); got != "git-exporter version "+version+"\n" {
					t.Fatalf("unexpected version output: %q", got)
				}
			})
		}
	}
}

// TestVerboseExport checks both flag forms, default output, empty exports, and
// partial failures, and matches table values against the published frontmatter.
func TestVerboseExport(t *testing.T) {
	for _, test := range []struct {
		name, flag  string
		empty, fail bool
	}{
		{name: "long", flag: "--verbose"},
		{name: "short", flag: "-v"},
		{name: "default"},
		{name: "empty", flag: "-v", empty: true},
		{name: "partial failure", flag: "-v", fail: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := testrepo.New(t)
			changes := map[string]*string{"ignored.txt": testrepo.Text("skip")}
			if !test.empty {
				changes["docs/a.md"] = testrepo.Text("body")
			}
			repo.Commit(changes)
			outputDir := t.TempDir()
			args := []string{"export", "-o", outputDir}
			if test.flag != "" {
				args = append(args, test.flag)
			}
			if test.fail {
				bad := testrepo.New(t)
				bad.Commit(map[string]*string{"a.md": testrepo.Text("prepared"), "z.md": testrepo.Text("---\nunterminated")})
				args = append(args, "--repo", "bad="+bad.Dir, "--site-url-template", "bad=https://example.test/bad/{commit}/{path}")
			}
			for _, project := range []string{"one", "two"} {
				args = append(args, "--repo", project+"="+repo.Dir, "--site-url-template", project+"=https://example.test/"+project+"/{commit}/{path}")
			}
			command := New()
			var output, log bytes.Buffer
			command.SetOut(&output)
			command.SetErr(&log)
			command.SetArgs(args)
			if err := command.Execute(); (err != nil) != test.fail {
				t.Fatalf("unexpected error: %v", err)
			}
			text := output.String()
			if test.flag == "" {
				if strings.Contains(text, "Page ID") || strings.Contains(text, "Markdown files retrieved:") {
					t.Fatalf("verbose output enabled by default: %s", text)
				}
				return
			}
			for _, header := range []string{"Page ID", "Url", "Markdown file"} {
				if !strings.Contains(text, header) {
					t.Fatalf("missing table header %q: %s", header, text)
				}
			}
			count := "2"
			if test.empty {
				count = "0"
			}
			if !strings.HasSuffix(text, "Markdown files retrieved: "+count+"\n") {
				t.Fatalf("missing final count: %s", text)
			}
			if strings.Contains(text, "https://example.test/bad/") {
				t.Fatalf("failed project included in table: %s", text)
			}
			if test.empty {
				return
			}
			for _, project := range []string{"one", "two"} {
				filename := filepath.Join(outputDir, project, "docs", "a.md")
				data, err := os.ReadFile(filename)
				if err != nil {
					t.Fatal(err)
				}
				parts := bytes.SplitN(data, []byte("---\n"), 3)
				if len(parts) != 3 {
					t.Fatalf("missing frontmatter: %s", data)
				}
				var header map[string]any
				if err := yaml.Unmarshal(parts[1], &header); err != nil {
					t.Fatal(err)
				}
				for _, value := range []string{filename, header["doc_id"].(string), header["site_url"].(string)} {
					if strings.Count(text, value) != 1 {
						t.Fatalf("expected table value %q exactly once: %s", value, text)
					}
				}
			}
		})
	}
}

// TestContinueAfterFailure verifies that a failed project does not prevent later exports and still causes an error.
func TestContinueAfterFailure(t *testing.T) {
	repo := testrepo.New(t)
	repo.Commit(map[string]*string{"a.md": testrepo.Text("body")})
	command := New()
	var output, log bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&log)
	command.SetArgs([]string{"export", "--output", t.TempDir(), "--repo", "bad=" + filepath.Join(t.TempDir(), "missing"), "--repo", "good=" + repo.Dir, "--site-url-template", "good=https://example.test/{commit}/{path}"})
	err := command.Execute()
	if err == nil {
		t.Fatal("expected partial failure")
	}
	if !strings.Contains(output.String(), "good: mode=local, exported=1") || !strings.Contains(log.String(), "bad: error") {
		t.Fatalf("%s\n%s", output.String(), log.String())
	}
}
