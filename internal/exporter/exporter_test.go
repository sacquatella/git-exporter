package exporter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-exporter/internal/config"
	"git-exporter/internal/testrepo"
)

func TestLocalExportAndSynchronization(t *testing.T) {
	repo := testrepo.New(t)
	first := repo.Commit(map[string]*string{"docs/a.md": testrepo.Text("---\ncustom: yes\n---\nbody\n"), "old.md": testrepo.Text("old"), "ignored.txt": testrepo.Text("skip")})
	output := t.TempDir()
	project := config.Project{Name: "p", Source: repo.Dir, SiteURLTemplate: "https://example.test/{commit}/{path}"}
	os.WriteFile(filepath.Join(repo.Dir, "docs/a.md"), []byte("uncommitted"), 0644)
	os.WriteFile(filepath.Join(repo.Dir, "untracked.md"), []byte("untracked"), 0644)
	result, err := Run(context.Background(), output, project, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if result.Exported != 2 || result.Mode != "local" {
		t.Fatalf("%+v", result)
	}
	data, err := os.ReadFile(filepath.Join(output, "p/docs/a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, []byte("body\n")) || !bytes.Contains(data, []byte(first.String())) {
		t.Fatalf("%s", data)
	}
	foreign := filepath.Join(output, "p/foreign.md")
	os.WriteFile(foreign, []byte("keep"), 0644)
	repo.Commit(map[string]*string{"docs/a.md": testrepo.Text("new body"), "old.md": nil})
	result, err = Run(context.Background(), output, project, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if result.Exported != 1 || result.Deleted != 1 {
		t.Fatalf("%+v", result)
	}
	if _, err := os.Stat(filepath.Join(output, "p/old.md")); !os.IsNotExist(err) {
		t.Fatal("obsolete file not removed")
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "keep" {
		t.Fatal("foreign file changed")
	}
	before, _ := os.ReadFile(filepath.Join(output, "p/docs/a.md"))
	repo.Commit(map[string]*string{"broken.md": testrepo.Text("---\nno closing"), "docs/a.md": testrepo.Text("do not publish")})
	if _, err := Run(context.Background(), output, project, io.Discard); err == nil {
		t.Fatal("invalid frontmatter accepted")
	}
	after, _ := os.ReadFile(filepath.Join(output, "p/docs/a.md"))
	if !bytes.Equal(before, after) {
		t.Fatal("failed preparation changed exports")
	}
	project.Ref = first.String()
	if _, err := Run(context.Background(), output, project, io.Discard); err != nil {
		t.Fatal("SHA export", err)
	}
}

func TestSynchronizationSafety(t *testing.T) {
	t.Run("foreign collision preflight", func(t *testing.T) {
		output := t.TempDir()
		Synchronize(output, "p", map[string][]byte{"a.md": []byte("old")})
		os.WriteFile(filepath.Join(output, "p/z.md"), []byte("foreign"), 0644)
		if _, err := Synchronize(output, "p", map[string][]byte{"a.md": []byte("new"), "z.md": []byte("overwrite")}); err == nil {
			t.Fatal("collision accepted")
		}
		content, _ := os.ReadFile(filepath.Join(output, "p/a.md"))
		if string(content) != "old" {
			t.Fatal("partial update")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		output := t.TempDir()
		outside := t.TempDir()
		os.Mkdir(filepath.Join(output, "p"), 0755)
		os.Symlink(outside, filepath.Join(output, "p/docs"))
		if _, err := Synchronize(output, "p", map[string][]byte{"docs/a.md": []byte("escape")}); err == nil {
			t.Fatal("symlink accepted")
		}
		if _, err := os.Stat(filepath.Join(outside, "a.md")); !os.IsNotExist(err) {
			t.Fatal("escaped destination")
		}
	})
	t.Run("malicious manifest", func(t *testing.T) {
		output := t.TempDir()
		os.Mkdir(filepath.Join(output, "p"), 0755)
		os.WriteFile(filepath.Join(output, "p", manifestName), []byte(`{"version":1,"project":"p","files":["../foreign.md"]}`), 0644)
		if _, err := Synchronize(output, "p", nil); err == nil {
			t.Fatal("unsafe manifest accepted")
		}
	})
	t.Run("empty export", func(t *testing.T) {
		output := t.TempDir()
		Synchronize(output, "p", map[string][]byte{"a.md": []byte("old")})
		deleted, err := Synchronize(output, "p", nil)
		if err != nil || deleted != 1 {
			t.Fatalf("%d %v", deleted, err)
		}
	})
}

func TestSafeError(t *testing.T) {
	t.Setenv("TOKEN", "very/secret")
	value := SafeError(fmt.Errorf("very/secret very%%2Fsecret"), config.Project{TokenEnv: "TOKEN"})
	if strings.Contains(value, "secret") {
		t.Fatal(value)
	}
}
