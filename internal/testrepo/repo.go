// Package testrepo creates deterministic fixtures entirely through Go.
package testrepo

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type Repo struct {
	Repo *git.Repository
	Dir  string
	T    testing.TB
	Tick int
}

func New(t testing.TB) *Repo {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	return &Repo{Repo: repo, Dir: dir, T: t}
}
func (r *Repo) Commit(changes map[string]*string, parents ...plumbing.Hash) plumbing.Hash {
	r.T.Helper()
	work, err := r.Repo.Worktree()
	if err != nil {
		r.T.Fatal(err)
	}
	for name, content := range changes {
		filename := filepath.Join(r.Dir, filepath.FromSlash(name))
		if content == nil {
			if _, err := work.Remove(name); err != nil {
				r.T.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			r.T.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(*content), 0644); err != nil {
			r.T.Fatal(err)
		}
		if _, err := work.Add(name); err != nil {
			r.T.Fatal(err)
		}
	}
	r.Tick++
	author := &object.Signature{Name: "Author", Email: "author@example.test", When: time.Date(2020, 1, r.Tick, 0, 0, 0, 0, time.UTC)}
	committer := &object.Signature{Name: "Committer", Email: "committer@example.test", When: author.When.Add(time.Hour)}
	hash, err := work.Commit("fixture", &git.CommitOptions{Author: author, Committer: committer, Parents: parents, AllowEmptyCommits: true})
	if err != nil {
		r.T.Fatal(err)
	}
	return hash
}
func Text(s string) *string { return &s }
