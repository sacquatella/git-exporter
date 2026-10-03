package gitdata

import (
	"context"
	"git-exporter/internal/config"
	"git-exporter/internal/testrepo"
	"github.com/go-git/go-git/v5/plumbing"
	"io"
	"os"
	"testing"
)

func TestFullCloneAllBranchesAndCleanup(t *testing.T) {
	fixture := testrepo.New(t)
	selected := fixture.Commit(map[string]*string{"a.md": testrepo.Text("selected")})
	other := fixture.Commit(map[string]*string{"other.bin": testrepo.Text("other branch")})
	server := &gitServer{repo: fixture.Repo, head: selected, extraRefs: map[string]plumbing.Hash{"refs/heads/other": other, "refs/tags/other-tag": other}}
	address := serve(t, server)
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	source, err := Open(context.Background(), config.Project{Name: "p", Source: address, TokenEnv: "TEST_GIT_TOKEN"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if source.Mode != "complet" || source.Commit != selected {
		t.Fatalf("%s %s", source.Mode, source.Commit)
	}
	if _, err := source.Repo.CommitObject(other); err != nil {
		t.Fatal("other branch not cloned", err)
	}
	if _, err := source.Repo.Tag("other-tag"); err != nil {
		t.Fatal("other tag not cloned", err)
	}
	source.Close()
	entries, err := os.ReadDir(temporary)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary storage not cleaned: %v %v", entries, err)
	}
}
