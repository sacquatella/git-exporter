package gitdata

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"git-exporter/internal/config"
	"git-exporter/internal/testrepo"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

type gitServer struct {
	extraRefs map[string]plumbing.Hash
	repo      *git.Repository
	head      plumbing.Hash
	filter    bool
	reject    string
	status    int
	corrupt   bool
	mu        sync.Mutex
	requests  []packp.UploadRequest
	sent      []plumbing.Hash
}

// refs builds the test server's advertised references and optional filter capability.
func (s *gitServer) refs() *packp.AdvRefs {
	adv := packp.NewAdvRefs()
	adv.Head = &s.head
	adv.References["refs/heads/main"] = s.head
	adv.References["refs/tags/v1"] = s.head
	for name, hash := range s.extraRefs {
		adv.References[name] = hash
	}
	adv.Capabilities.Set(capability.SymRef, "HEAD:refs/heads/main")
	if s.filter {
		adv.Capabilities.Set(capability.Filter)
	}
	return adv
}

// pack encodes the requested objects and their dependencies, honors blob filtering,
// and records which objects the test server sends.
func (s *gitServer) pack(req *packp.UploadRequest) ([]byte, error) {
	selected := map[plumbing.Hash]bool{}
	var visit func(plumbing.Hash, bool) error
	visit = func(hash plumbing.Hash, explicit bool) error {
		if selected[hash] {
			return nil
		}
		obj, err := s.repo.Storer.EncodedObject(plumbing.AnyObject, hash)
		if err != nil {
			return err
		}
		if obj.Type() == plumbing.BlobObject && req.Filter != "" && !explicit {
			return nil
		}
		selected[hash] = true
		switch obj.Type() {
		case plumbing.CommitObject:
			commit, err := s.repo.CommitObject(hash)
			if err != nil {
				return err
			}
			if err := visit(commit.TreeHash, false); err != nil {
				return err
			}
			for _, parent := range commit.ParentHashes {
				if err := visit(parent, false); err != nil {
					return err
				}
			}
		case plumbing.TreeObject:
			tree, err := s.repo.TreeObject(hash)
			if err != nil {
				return err
			}
			for _, entry := range tree.Entries {
				if err := visit(entry.Hash, false); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, hash := range req.Wants {
		if err := visit(hash, true); err != nil {
			return nil, err
		}
	}
	hashes := []plumbing.Hash{}
	for hash := range selected {
		hashes = append(hashes, hash)
	}
	s.mu.Lock()
	s.sent = append(s.sent, hashes...)
	s.mu.Unlock()
	var output bytes.Buffer
	_, err := packfile.NewEncoder(&output, s.repo.Storer, false).Encode(hashes, 0)
	return output.Bytes(), err
}

// ServeHTTP emulates authenticated Git reference discovery and upload-pack responses,
// including configurable feature refusals, HTTP errors, and corrupt packs.
func (s *gitServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.status != 0 {
		http.Error(w, "failure", s.status)
		return
	}
	if user, password, ok := r.BasicAuth(); !ok || user != "git" || password != "secret" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/info/refs") {
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		encoder := pktline.NewEncoder(w)
		encoder.EncodeString("# service=git-upload-pack\n")
		encoder.Flush()
		s.refs().Encode(w)
		return
	}
	req, err := decodeTestRequest(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.mu.Lock()
	s.requests = append(s.requests, *req)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	blobs := false
	if len(req.Wants) > 0 {
		_, err := s.repo.BlobObject(req.Wants[0])
		blobs = err == nil
	}
	if (s.reject == "blob" && blobs) || (s.reject == "filter" && req.Filter != "") {
		message := "filtering not supported"
		if blobs {
			message = "upload-pack: not our ref " + req.Wants[0].String()
		}
		(&pktline.ErrorLine{Text: message}).Encode(w)
		return
	}
	pktline.NewEncoder(w).EncodeString("NAK\n")
	if s.corrupt {
		w.Write([]byte("not a pack"))
		return
	}
	data, err := s.pack(req)
	if err != nil {
		return
	}
	w.Write(data)
}

// serve starts a TLS Git test server, configures its trusted client and token,
// and registers cleanup to close the server and restore the previous transport.
func serve(t *testing.T, s *gitServer) string {
	t.Helper()
	server := httptest.NewTLSServer(s)
	t.Cleanup(server.Close)
	old := client.Protocols["https"]
	client.InstallProtocol("https", githttp.NewClient(server.Client()))
	t.Cleanup(func() { client.InstallProtocol("https", old) })
	t.Setenv("TEST_GIT_TOKEN", "secret")
	return server.URL + "/repo.git"
}

// TestSelectiveAndFallbackHTTPS checks selective HTTPS fetching and full-clone fallback,
// including transferred objects and matching metadata.
func TestSelectiveAndFallbackHTTPS(t *testing.T) {
	fixture := testrepo.New(t)
	first := fixture.Commit(map[string]*string{"docs/a.md": testrepo.Text("old"), "large.bin": testrepo.Text(strings.Repeat("B", 100000))})
	head := fixture.Commit(map[string]*string{"docs/a.md": testrepo.Text("current"), "same.MD": testrepo.Text("current")})
	oldCommit, _ := fixture.Repo.CommitObject(first)
	oldFile, _ := oldCommit.File("docs/a.md")
	currentCommit, _ := fixture.Repo.CommitObject(head)
	binary, _ := currentCommit.File("large.bin")
	var expectedMeta map[string]Metadata
	for _, test := range []struct {
		name         string
		filter       bool
		reject, mode string
	}{
		{"selective", true, "", "selective"}, {"no_filter", false, "", "full"}, {"reject_blobs", true, "blob", "full"}, {"reject_filter", true, "filter", "full"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := &gitServer{repo: fixture.Repo, head: head, filter: test.filter, reject: test.reject}
			address := serve(t, server)
			var log bytes.Buffer
			source, err := Open(context.Background(), config.Project{Name: "docs", Source: address, TokenEnv: "TEST_GIT_TOKEN"}, &log)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			if source.Mode != test.mode {
				t.Fatalf("mode %s", source.Mode)
			}
			commit, err := source.Repo.CommitObject(source.Commit)
			if err != nil {
				t.Fatal(err)
			}
			file, err := commit.File("docs/a.md")
			if err != nil {
				t.Fatal(err)
			}
			content, err := file.Contents()
			if err != nil || content != "current" {
				t.Fatalf("%q %v", content, err)
			}
			files, err := MarkdownFiles(commit)
			if err != nil {
				t.Fatal(err)
			}
			meta, err := History(context.Background(), commit, files)
			if err != nil {
				t.Fatal(err)
			}
			if expectedMeta == nil {
				expectedMeta = meta
			} else if fmt.Sprint(expectedMeta) != fmt.Sprint(meta) {
				t.Fatalf("metadata differs: %v / %v", meta, expectedMeta)
			}
			if test.mode == "selective" {
				for _, forbidden := range []plumbing.Hash{oldFile.Hash, binary.Hash} {
					if source.Repo.Storer.HasEncodedObject(forbidden) == nil {
						t.Fatalf("unnecessary object fetched: %s", forbidden)
					}
					for _, sent := range server.sent {
						if sent == forbidden {
							t.Fatal("unnecessary object transmitted")
						}
					}
				}
				if len(server.requests) != 2 || len(server.requests[1].Wants) != 1 {
					t.Fatalf("expected two requests with deduplicated blobs: %+v", server.requests)
				}
				if log.Len() != 0 {
					t.Fatal(log.String())
				}
			} else {
				if !strings.Contains(log.String(), "full clone") {
					t.Fatal("fallback not logged")
				}
				if source.Repo.Storer.HasEncodedObject(binary.Hash) != nil {
					t.Fatal("full clone missing binary")
				}
			}
		})
	}
}

// TestRemoteErrorsDoNotFallback checks that authentication, transport, integrity,
// and cancellation errors do not trigger a full clone.
func TestRemoteErrorsDoNotFallback(t *testing.T) {
	fixture := testrepo.New(t)
	head := fixture.Commit(map[string]*string{"a.md": testrepo.Text("body")})
	for _, test := range []struct {
		name    string
		status  int
		corrupt bool
	}{{"authentication", 401, false}, {"network_http", 503, false}, {"integrity", 0, true}} {
		t.Run(test.name, func(t *testing.T) {
			server := &gitServer{repo: fixture.Repo, head: head, filter: true, status: test.status, corrupt: test.corrupt}
			var log bytes.Buffer
			_, err := Open(context.Background(), config.Project{Name: "p", Source: serve(t, server), TokenEnv: "TEST_GIT_TOKEN"}, &log)
			if err == nil {
				t.Fatal("expected error")
			}
			if log.Len() != 0 {
				t.Fatal("unexpected fallback:", log.String())
			}
		})
	}
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, io.ErrUnexpectedEOF, transport.ErrAuthenticationRequired, errors.New("invalid checksum")} {
		if selectiveRefusal(err) {
			t.Fatalf("incorrect fallback: %v", err)
		}
	}
}

// TestResolveReferences checks resolution of HEAD, branches, tags, and full hashes, and rejection of missing references.
func TestResolveReferences(t *testing.T) {
	fixture := testrepo.New(t)
	head := fixture.Commit(map[string]*string{"a.md": testrepo.Text("body")})
	adv := (&gitServer{head: head}).refs()
	for _, ref := range []string{"", "HEAD", "main", "refs/heads/main", "v1", head.String()} {
		got, err := resolve(adv, ref)
		if err != nil || got != head {
			t.Fatalf("%s: %s %v", ref, got, err)
		}
	}
	if _, err := resolve(adv, "missing"); err == nil {
		t.Fatal("missing reference accepted")
	}
}

// TestHistoryRecreationAndFirstParent checks first-parent history, recreated and renamed paths,
// and the use of committer metadata.
func TestHistoryRecreationAndFirstParent(t *testing.T) {
	fixture := testrepo.New(t)
	first := fixture.Commit(map[string]*string{"a.md": testrepo.Text("one")})
	fixture.Commit(map[string]*string{"a.md": testrepo.Text("two")})
	fixture.Commit(map[string]*string{"a.md": nil, "renamed.md": testrepo.Text("two")})
	fourth := fixture.Commit(map[string]*string{"a.md": testrepo.Text("recreated")})
	merge := fixture.Commit(map[string]*string{"a.md": testrepo.Text("merged")}, fourth, first)
	commit, _ := fixture.Repo.CommitObject(merge)
	files, err := MarkdownFiles(commit)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := History(context.Background(), commit, files)
	if err != nil {
		t.Fatal(err)
	}
	if metadata["a.md"].Created.Day() != 4 || metadata["a.md"].Updated.Day() != 5 || metadata["renamed.md"].Created.Day() != 3 {
		t.Fatalf("wrong history: %+v", metadata)
	}
	if metadata["a.md"].Committer != "Committer" {
		t.Fatal("author used instead of committer")
	}
}
