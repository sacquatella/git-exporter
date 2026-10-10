package cli

import (
	"bytes"
	"git-exporter/internal/testrepo"
	"path/filepath"
	"strings"
	"testing"
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
