package gitdata

import (
	"context"
	"fmt"
	"git-exporter/internal/config"
	"git-exporter/internal/testrepo"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"io"
	"testing"
)

// TestBlobBatchesAndRemoteReferences checks that remote revisions resolve correctly
// and Markdown blobs are fetched in batches of at most 128.
func TestBlobBatchesAndRemoteReferences(t *testing.T) {
	fixture := testrepo.New(t)
	changes := map[string]*string{}
	for i := 0; i < 130; i++ {
		changes[fmt.Sprintf("docs/%03d.md", i)] = testrepo.Text(fmt.Sprintf("content %d", i))
	}
	head := fixture.Commit(changes)
	for _, ref := range []string{"main", "v1", head.String()} {
		t.Run(ref, func(t *testing.T) {
			server := &gitServer{repo: fixture.Repo, head: head, filter: true}
			source, err := Open(context.Background(), config.Project{Name: "p", Source: serve(t, server), Ref: ref, TokenEnv: "TEST_GIT_TOKEN"}, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			if len(server.requests) != 3 || len(server.requests[1].Wants) != 128 || len(server.requests[2].Wants) != 2 {
				t.Fatalf("wrong batches: %+v", server.requests)
			}
		})
	}
}

// TestLocalAnnotatedTag checks that local and advertised annotated tags resolve to their target commit.
func TestLocalAnnotatedTag(t *testing.T) {
	fixture := testrepo.New(t)
	head := fixture.Commit(map[string]*string{"a.md": testrepo.Text("body")})
	commit, _ := fixture.Repo.CommitObject(head)
	_, err := fixture.Repo.CreateTag("release", head, &git.CreateTagOptions{Tagger: &object.Signature{Name: "tagger", Email: "tagger@example.test", When: commit.Committer.When}, Message: "release"})
	if err != nil {
		t.Fatal(err)
	}
	source, err := Open(context.Background(), config.Project{Source: fixture.Dir, Ref: "release"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if source.Commit != head {
		t.Fatalf("tag not peeled: %s", source.Commit)
	}
	adv := (&gitServer{head: head}).refs()
	adv.References["refs/tags/annotated"] = plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	adv.Peeled["refs/tags/annotated"] = head
	if actual, err := resolve(adv, "annotated"); err != nil || actual != head {
		t.Fatalf("%s %v", actual, err)
	}
}
