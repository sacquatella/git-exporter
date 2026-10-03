// Package gitdata reads Git objects directly, without invoking a Git executable.
package gitdata

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"

	"git-exporter/internal/config"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

var ErrSelectiveUnsupported = errors.New("récupération sélective non prise en charge")
var fullHash = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

type Source struct {
	Repo    *git.Repository
	Commit  plumbing.Hash
	URL     string
	Mode    string
	cleanup func()
}

func (s *Source) Close() {
	if s.cleanup != nil {
		s.cleanup()
	}
}

type sessionFactory func() (transport.UploadPackSession, error)

func Auth(p config.Project, ep *transport.Endpoint) (transport.AuthMethod, error) {
	switch ep.Protocol {
	case "https":
		if p.TokenEnv == "" {
			return nil, nil
		}
		token, ok := os.LookupEnv(p.TokenEnv)
		if !ok || token == "" {
			return nil, fmt.Errorf("variable %s absente ou vide", p.TokenEnv)
		}
		username := p.Username
		if username == "" {
			username = "git"
		}
		return &githttp.BasicAuth{Username: username, Password: token}, nil
	case "ssh":
		username := p.Username
		if username == "" {
			username = ep.User
		}
		if username == "" {
			username = "git"
		}
		if p.SSHKey == "" {
			return gitssh.NewSSHAgentAuth(username)
		}
		passphrase := ""
		if p.SSHPassphraseEnv != "" {
			var ok bool
			passphrase, ok = os.LookupEnv(p.SSHPassphraseEnv)
			if !ok {
				return nil, fmt.Errorf("variable %s absente", p.SSHPassphraseEnv)
			}
		}
		return gitssh.NewPublicKeysFromFile(username, p.SSHKey, passphrase)
	default:
		return nil, fmt.Errorf("transport non pris en charge: %s", ep.Protocol)
	}
}

func Open(ctx context.Context, p config.Project, log io.Writer) (*Source, error) {
	if !config.IsRemote(p.Source) {
		repo, err := git.PlainOpen(p.Source)
		if err != nil {
			return nil, err
		}
		ref := p.Ref
		if ref == "" {
			ref = "HEAD"
		}
		hash, err := repo.ResolveRevision(plumbing.Revision(ref))
		if err != nil {
			return nil, err
		}
		remoteURL := ""
		if remote, err := repo.Remote("origin"); err == nil && len(remote.Config().URLs) > 0 {
			remoteURL = remote.Config().URLs[0]
		}
		return &Source{Repo: repo, Commit: *hash, URL: remoteURL, Mode: "local"}, nil
	}
	ep, err := transport.NewEndpoint(p.Source)
	if err != nil {
		return nil, err
	}
	auth, err := Auth(p, ep)
	if err != nil {
		return nil, err
	}
	transportClient, err := client.NewClient(ep)
	if err != nil {
		return nil, err
	}
	factory := func() (transport.UploadPackSession, error) { return transportClient.NewUploadPackSession(ep, auth) }
	return openRemote(ctx, p, log, factory, func(dir string) (*git.Repository, error) {
		return git.PlainCloneContext(ctx, dir, true, &git.CloneOptions{URL: p.Source, Auth: auth, NoCheckout: true, Tags: git.AllTags})
	})
}

func advertised(ctx context.Context, factory sessionFactory) (*packp.AdvRefs, error) {
	session, err := factory()
	if err != nil {
		return nil, err
	}
	defer session.Close()
	return session.AdvertisedReferencesContext(ctx)
}

func resolve(adv *packp.AdvRefs, ref string) (plumbing.Hash, error) {
	if fullHash.MatchString(ref) {
		return plumbing.NewHash(ref), nil
	}
	if ref == "" {
		ref = "HEAD"
	}
	refs, err := adv.AllReferences()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	for _, name := range []string{ref, "refs/heads/" + ref, "refs/tags/" + ref} {
		resolved, err := storer.ResolveReference(refs, plumbing.ReferenceName(name))
		if err == nil {
			if peeled, ok := adv.Peeled[name]; ok {
				return peeled, nil
			}
			return resolved.Hash(), nil
		}
	}
	return plumbing.ZeroHash, fmt.Errorf("référence distante introuvable: %s", ref)
}

func openRemote(ctx context.Context, p config.Project, log io.Writer, factory sessionFactory, clone func(string) (*git.Repository, error)) (_ *Source, err error) {
	adv, err := advertised(ctx, factory)
	if err != nil {
		return nil, err
	}
	selected, err := resolve(adv, p.Ref)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "git-exporter-selective-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()
	repo, err := git.PlainInit(dir, true)
	if err != nil {
		return nil, err
	}
	err = selective(ctx, repo, selected, adv, factory)
	mode := "sélectif"
	if errors.Is(err, ErrSelectiveUnsupported) {
		// Never include a raw server response here: it can contain credentials.
		fmt.Fprintf(log, "%s: %s ; repli vers un clone complet\n", p.Name, err)
		if removeErr := os.RemoveAll(dir); removeErr != nil {
			return nil, removeErr
		}
		dir, err = os.MkdirTemp("", "git-exporter-full-")
		if err != nil {
			return nil, err
		}
		repo, err = clone(dir)
		mode = "complet"
	}
	if err != nil {
		return nil, err
	}
	commit, err := repo.CommitObject(selected)
	if err != nil {
		return nil, fmt.Errorf("commit sélectionné indisponible: %w", err)
	}
	return &Source{Repo: repo, Commit: commit.Hash, URL: p.Source, Mode: mode, cleanup: func() { os.RemoveAll(dir) }}, nil
}

func selective(ctx context.Context, repo *git.Repository, selected plumbing.Hash, adv *packp.AdvRefs, factory sessionFactory) error {
	if !adv.Capabilities.Supports(capability.Filter) {
		return fmt.Errorf("%w: filtre blob:none absent", ErrSelectiveUnsupported)
	}
	if err := fetchObjects(ctx, repo, factory, []plumbing.Hash{selected}, true); err != nil {
		return err
	}
	commit, err := repo.CommitObject(selected)
	if err != nil {
		return err
	}
	files, err := MarkdownFiles(commit)
	if err != nil {
		return err
	}
	seen := map[plumbing.Hash]bool{}
	hashes := []plumbing.Hash{}
	for _, file := range files {
		if !seen[file.Hash] {
			seen[file.Hash] = true
			hashes = append(hashes, file.Hash)
		}
	}
	for start := 0; start < len(hashes); start += 128 {
		end := min(start+128, len(hashes))
		if err := fetchObjects(ctx, repo, factory, hashes[start:end], false); err != nil {
			return err
		}
		for _, hash := range hashes[start:end] {
			if _, err := repo.BlobObject(hash); err != nil {
				return fmt.Errorf("blob demandé absent: %w", err)
			}
		}
	}
	return nil
}

// Only explicit feature/object refusals qualify for fallback. Connection,
// authentication, cancellation and malformed pack errors must remain failures.
func selectiveRefusal(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, transport.ErrAuthenticationRequired) || errors.Is(err, transport.ErrAuthorizationFailed) {
		return false
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"filtering not supported", "filter is not supported", "does not support filter", "filtering is not allowed", "filter 'blob:none' not supported", "unadvertised object", "not our ref", "does not allow request for unadvertised"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func fetchObjects(ctx context.Context, repo *git.Repository, factory sessionFactory, hashes []plumbing.Hash, filtered bool) error {
	session, err := factory()
	if err != nil {
		return err
	}
	defer session.Close()
	adv, err := session.AdvertisedReferencesContext(ctx)
	if err != nil {
		return err
	}
	req := packp.NewUploadPackRequestFromCapabilities(adv.Capabilities)
	req.Capabilities.Delete(capability.ThinPack) // no external delta bases
	req.Wants = hashes
	if filtered {
		if !adv.Capabilities.Supports(capability.Filter) {
			return fmt.Errorf("%w: filtre blob:none absent", ErrSelectiveUnsupported)
		}
		if err := req.Capabilities.Set(capability.Filter); err != nil {
			return err
		}
		req.Filter = packp.FilterBlobNone()
	}
	response, err := session.UploadPack(ctx, req)
	if err != nil {
		if selectiveRefusal(err) {
			return fmt.Errorf("%w: requête filtrée ou objet ciblé refusé", ErrSelectiveUnsupported)
		}
		return err
	}
	defer response.Close()
	var reader io.Reader = response
	if req.Capabilities.Supports(capability.Sideband64k) {
		reader = sideband.NewDemuxer(sideband.Sideband64k, response)
	} else if req.Capabilities.Supports(capability.Sideband) {
		reader = sideband.NewDemuxer(sideband.Sideband, response)
	}
	if err := packfile.UpdateObjectStorage(repo.Storer, reader); err != nil {
		if selectiveRefusal(err) {
			return fmt.Errorf("%w: requête filtrée ou objet ciblé refusé", ErrSelectiveUnsupported)
		}
		return fmt.Errorf("lecture du pack Git: %w", err)
	}
	return ctx.Err()
}
