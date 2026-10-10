package markdown

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"git-exporter/internal/gitdata"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"gopkg.in/yaml.v3"
)

// SiteURL builds and validates a web URL for a file at a specific commit.
// It escapes path segments and uses GitHub or GitLab defaults when no template is given.
func SiteURL(remote, template, commit, filename string) (string, error) {
	repoURL, host := "", ""
	if remote != "" {
		ep, err := transport.NewEndpoint(remote)
		if err != nil {
			return "", fmt.Errorf("invalid Git URL")
		}
		if ep.Host != "" {
			host = ep.Host
			repositoryPath := strings.TrimSuffix(strings.TrimPrefix(ep.Path, "/"), ".git")
			repoURL = "https://" + host + "/" + repositoryPath
			if ep.Protocol == "https" && ep.Port != 0 && ep.Port != 443 {
				repoURL = fmt.Sprintf("https://%s:%d/%s", host, ep.Port, repositoryPath)
			}
		}
	}
	if template == "" {
		switch strings.ToLower(host) {
		case "github.com":
			template = "{repo_url}/blob/{commit}/{path}"
		case "gitlab.com":
			template = "{repo_url}/-/blob/{commit}/{path}"
		default:
			return "", fmt.Errorf("site_url_template required for this host or a repository without origin")
		}
	}
	if strings.Contains(template, "{repo_url}") && repoURL == "" {
		return "", fmt.Errorf("remote URL required for {repo_url}")
	}
	segments := strings.Split(filename, "/")
	for i := range segments {
		segments[i] = url.PathEscape(segments[i])
	}
	value := strings.NewReplacer("{repo_url}", repoURL, "{commit}", commit, "{path}", strings.Join(segments, "/")).Replace(template)
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || strings.ContainsAny(value, "{}") {
		return "", fmt.Errorf("invalid site_url_template")
	}
	return value, nil
}

// DocumentID returns the stable document identity for a project and repository-relative path.
func DocumentID(project, filename string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(project+"\x00"+filename)))
}

// Transform adds or replaces managed YAML frontmatter fields using project and Git metadata.
// It preserves the body, any UTF-8 BOM, and custom field values, and rejects invalid frontmatter.
func Transform(input []byte, project, filename, site string, meta gitdata.Metadata) ([]byte, error) {
	bom := []byte(nil)
	if bytes.HasPrefix(input, []byte{0xef, 0xbb, 0xbf}) {
		bom = input[:3]
		input = input[3:]
	}
	body := input
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	first, rest, _ := bytes.Cut(input, []byte("\n"))
	delimiter := strings.TrimSuffix(string(first), "\r")
	if delimiter == "+++" || strings.HasPrefix(strings.TrimSpace(string(first)), "{") {
		return nil, fmt.Errorf("only YAML frontmatter is supported")
	}
	if delimiter == "---" {
		offset, closed := len(first)+1, false
		for len(rest) > 0 {
			line, next, hasNewline := bytes.Cut(rest, []byte("\n"))
			marker := strings.TrimSuffix(string(line), "\r")
			if marker == "---" || marker == "..." {
				header := input[len(first)+1 : offset]
				if len(bytes.TrimSpace(header)) > 0 {
					var validate map[string]any
					if err := yaml.Unmarshal(header, &validate); err != nil {
						return nil, fmt.Errorf("invalid frontmatter: %w", err)
					}
					var document yaml.Node
					if err := yaml.Unmarshal(header, &document); err != nil {
						return nil, err
					}
					if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
						return nil, fmt.Errorf("expected a YAML mapping for frontmatter")
					}
					// Resolve YAML aliases and merge keys before replacing managed fields.
					// Otherwise a preserved alias could refer to an anchor we remove.
					if err := root.Encode(validate); err != nil {
						return nil, err
					}
				}
				end := offset + len(line)
				if hasNewline {
					end++
				}
				body = input[end:]
				closed = true
				break
			}
			offset += len(line) + 1
			rest = next
		}
		if !closed {
			return nil, fmt.Errorf("unterminated YAML frontmatter")
		}
	}
	id := DocumentID(project, filename)
	values := []struct {
		key   string
		value any
	}{
		{"title", filename}, {"doc_id", id}, {"description", "Project " + project + " — " + filename},
		{"tags", []string{"git"}}, {"site_url", site}, {"authors", []string{meta.Committer}},
		{"creation_date", meta.Created.UTC().Format(time.RFC3339)}, {"last_update_date", meta.Updated.UTC().Format(time.RFC3339)}, {"visibility", "Internal"},
	}
	for _, value := range values {
		node := &yaml.Node{}
		if err := node.Encode(value.value); err != nil {
			return nil, err
		}
		replaced := false
		for i := 0; i < len(root.Content); i += 2 {
			if root.Content[i].Value == value.key {
				root.Content[i+1] = node
				replaced = true
				break
			}
		}
		if !replaced {
			root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value.key}, node)
		}
	}
	// Canonical ordering makes repeated exports byte-for-byte stable.
	type pair struct{ key, value *yaml.Node }
	pairs := make([]pair, 0, len(root.Content)/2)
	for i := 0; i < len(root.Content); i += 2 {
		pairs = append(pairs, pair{root.Content[i], root.Content[i+1]})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].key.Value < pairs[j].key.Value })
	root.Content = nil
	for _, pair := range pairs {
		root.Content = append(root.Content, pair.key, pair.value)
	}
	var output bytes.Buffer
	output.Write(bom)
	output.WriteString("---\n")
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	output.WriteString("---\n")
	output.Write(body)
	return output.Bytes(), nil
}
