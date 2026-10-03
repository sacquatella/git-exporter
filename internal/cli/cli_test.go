package cli

import (
	"bytes"
	"git-exporter/internal/testrepo"
	"path/filepath"
	"strings"
	"testing"
)

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
	if !strings.Contains(output.String(), "good: mode=local, exportés=1") || !strings.Contains(log.String(), "bad: erreur") {
		t.Fatalf("%s\n%s", output.String(), log.String())
	}
}
