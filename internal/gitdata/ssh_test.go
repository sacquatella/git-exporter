package gitdata

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git-exporter/internal/config"
	"git-exporter/internal/testrepo"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// go-git's server decoder does not yet understand filter lines; strip and
// validate that extension here while retaining its parser for the other lines.
func decodeTestRequest(reader io.Reader) (*packp.UploadRequest, error) {
	var raw bytes.Buffer
	encoder := pktline.NewEncoder(&raw)
	scanner := pktline.NewScanner(reader)
	filter := packp.Filter("")
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			encoder.Flush()
			break
		}
		if bytes.HasPrefix(line, []byte("filter ")) {
			filter = packp.Filter(strings.TrimSpace(string(line[7:])))
			continue
		}
		if err := encoder.Encode(line); err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	req := packp.NewUploadRequest()
	if err := req.Decode(&raw); err != nil {
		return nil, err
	}
	req.Filter = filter
	return req, nil
}

func TestSelectiveSSHAndHostVerification(t *testing.T) {
	fixture := testrepo.New(t)
	head := fixture.Commit(map[string]*string{"a.md": testrepo.Text("body"), "binary": testrepo.Text("excluded")})
	server := &gitServer{repo: fixture.Repo, head: head, filter: true}
	_, hostKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}
	userPublic, userPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	userKey, err := ssh.NewPublicKey(userPublic)
	if err != nil {
		t.Fatal(err)
	}
	serverConfig := &ssh.ServerConfig{PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if meta.User() != "git" || !bytes.Equal(key.Marshal(), userKey.Marshal()) {
			return nil, fmt.Errorf("unauthorized")
		}
		return nil, nil
	}}
	serverConfig.AddHostKey(hostSigner)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func(connection net.Conn) {
				defer connection.Close()
				connection.SetDeadline(time.Now().Add(10 * time.Second))
				sshConn, channels, requests, err := ssh.NewServerConn(connection, serverConfig)
				if err != nil {
					return
				}
				defer sshConn.Close()
				go ssh.DiscardRequests(requests)
				for newChannel := range channels {
					if newChannel.ChannelType() != "session" {
						newChannel.Reject(ssh.UnknownChannelType, "session required")
						continue
					}
					channel, requests, err := newChannel.Accept()
					if err != nil {
						return
					}
					func() {
						defer channel.Close()
						for request := range requests {
							if request.Type != "exec" {
								request.Reply(false, nil)
								continue
							}
							var payload struct{ Command string }
							if ssh.Unmarshal(request.Payload, &payload) != nil || !strings.HasPrefix(payload.Command, "git-upload-pack ") {
								request.Reply(false, nil)
								return
							}
							request.Reply(true, nil)
							if server.refs().Encode(channel) != nil {
								return
							}
							req, err := decodeTestRequest(channel)
							if err != nil {
								return
							}
							scanner := pktline.NewScanner(channel)
							for scanner.Scan() {
								if strings.TrimSpace(string(scanner.Bytes())) == "done" {
									break
								}
							}
							if scanner.Err() != nil {
								return
							}
							data, err := server.pack(req)
							if err != nil {
								return
							}
							pktline.NewEncoder(channel).EncodeString("NAK\n")
							channel.Write(data)
							channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
							return
						}
					}()
				}
			}(connection)
		}
	}()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")
	block, err := ssh.MarshalPrivateKey(userPrivate, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}
	hostsPath := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(hostsPath, []byte(knownhosts.Line([]string{listener.Addr().String()}, hostSigner.PublicKey())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSH_KNOWN_HOSTS", hostsPath)
	p := config.Project{Name: "p", Source: "ssh://git@" + listener.Addr().String() + "/repo.git", SSHKey: keyPath}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	source, err := Open(ctx, p, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	source.Close()
	if source.Mode != "sélectif" {
		t.Fatal(source.Mode)
	}
	// A changed host key must fail, with no full-clone retry.
	os.WriteFile(hostsPath, []byte(knownhosts.Line([]string{listener.Addr().String()}, userKey)+"\n"), 0600)
	var log bytes.Buffer
	if _, err := Open(ctx, p, &log); err == nil {
		t.Fatal("changed host key accepted")
	}
	if log.Len() != 0 {
		t.Fatal("unexpected fallback")
	}
}
