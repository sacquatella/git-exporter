# git-exporter

A Go CLI that exports Markdown files from multiple Git repositories and enriches their YAML frontmatter. The binary runs without an installed Git executable or GitHub/GitLab APIs.

## Build and run

Go **1.25 or later** is required to build. Dependencies are pinned in `go.mod` and `go.sum`.

```sh
go build -o bin/git-exporter .
./bin/git-exporter --version
./bin/git-exporter export --repo docs=https://github.com/organization/docs.git --output ./export
./bin/git-exporter export --config config.example.yaml
./bin/git-exporter export --help
```

`--version` (or `-v`) displays the binary version. It defaults to `dev` when
building directly with `go build`. The Makefile uses
`git describe --tags --always --dirty` (override it with `make VERSION=v1.2.3 build-linux-amd`),
and releases use their Git tag. To inject a version manually:
`go build -ldflags "-X git-exporter/internal/cli.Version=v1.2.3" -o bin/git-exporter .`.

The example file contains placeholder addresses that must be replaced. The compiled binary does not require Go at runtime.

## Configuration

```yaml
output: ./export
projects:
  - name: docs
    source: https://github.com/organization/docs.git
    ref: main
    token_env: DOCS_TOKEN
  - name: local
    source: ../my-project
    site_url_template: "https://git.example.com/team/project/blob/{commit}/{path}"
```

`name` is a stable, unique identifier containing ASCII letters, digits, periods, hyphens, and underscores, starting with a letter or digit. It determines the export subdirectory and is used to calculate document IDs.

Explicit CLI options override YAML values. Projects are merged by name; `--repo` adds a project or replaces its source. An option targeting an unknown project produces an error. YAML paths are relative to the configuration file; CLI paths are relative to the current directory. No configuration file is loaded implicitly.

| Repeatable option | YAML field | Example |
| --- | --- | --- |
| `--repo` | `name`, `source` | `--repo docs=../docs` |
| `--ref` | `ref` | `--ref docs=main` |
| `--username` | `username` | `--username docs=git` |
| `--token-env` | `token_env` | `--token-env docs=DOCS_TOKEN` |
| `--ssh-key` | `ssh_key` | `--ssh-key docs=./keys/id_ed25519` |
| `--ssh-passphrase-env` | `ssh_passphrase_env` | `--ssh-passphrase-env docs=KEY_PASSPHRASE` |
| `--site-url-template` | `site_url_template` | `--site-url-template 'docs={repo_url}/-/blob/{commit}/{path}'` |

`--config` can also be written as `-c`, and `--output` as `-o`:

```sh
./bin/git-exporter export -c config.example.yaml -o ./export
```

`--output` (`-o`) or `output` is required. The reference can be a branch, tag, or full commit SHA. The default is local `HEAD` or the remote default branch. Local changes and untracked files are ignored.

## Authentication

- **HTTPS**: `token_env` names an environment variable containing the token; `username` defaults to `git`. Public repositories do not require a token. Passwords embedded in URLs are rejected.
- **SSH**: use a URL such as `ssh://git@server/path.git` or `git@server:path.git`. Authentication uses `ssh_key`, optionally encrypted with a passphrase from `ssh_passphrase_env`, or the SSH agent if no key is configured.
- The SSH server key is checked against `known_hosts`. Set `SSH_KNOWN_HOSTS` to use a specific file; otherwise, go-git's usual locations are used. TLS is verified for HTTPS.
- Displayed errors redact the values of configured token and passphrase variables. External Git credential helpers are not executed.

For an encrypted SSH private key, `DOCS_SSH_PASSPHRASE` is an example environment
variable containing the key's passphrase. Export it before running the CLI, then
pass its **name** through `--ssh-passphrase-env` or the YAML `ssh_passphrase_env`
field alongside `ssh_key`:

```sh
# POSIX shell: replace the placeholder with your key's passphrase.
export DOCS_SSH_PASSPHRASE='<your SSH key passphrase>'
git-exporter export --repo docs=git@github.com:organization/docs.git \
  --ssh-key docs=./keys/id_ed25519 \
  --ssh-passphrase-env docs=DOCS_SSH_PASSPHRASE -o ./export
```

The variable name is configurable and is not read automatically. With a key file
configured, a missing named variable produces an error. Omit this setting for an
unencrypted key. When no key file is configured, the SSH agent is used and this
setting is ignored. Run `git-exporter export -h` for more usage examples.

## Remote fetching

The CLI uses the Git protocol directly:

1. Resolve the reference and pin the commit to export.
2. Request commits and trees reachable from that commit using the `blob:none` filter.
3. Discover `.md` files (case-insensitive extension).
4. Fetch only the selected commit's Markdown blobs, deduplicated and grouped into batches of 128.

Non-Markdown contents and older file versions are not requested. History metadata can still be substantial, especially in older repositories with many merged branches.

If the server does not advertise the filter or explicitly rejects filtering or targeted object requests, the CLI logs the reason, discards the partial storage, and performs a **full clone** in a new temporary directory. This clone retrieves all accessible branches and tags, without a checkout, submodules, or Git LFS objects. The originally selected commit is still the one exported.

Network, authentication, SSH host key, cancellation, and pack reading or integrity errors are reported without fallback. All temporary storage is cleaned up when finished. No persistent cache is retained.

## Generated documents

Regular files are written to `<output>/<project>/<original-path>`. Symbolic links and submodules are excluded. Images, attachments, and relative links in the Markdown body are neither copied nor rewritten. An LFS pointer tracked as a `.md` file is exported as its stored Git content; no LFS object is downloaded.

| Field | Generated value |
| --- | --- |
| `title` | Full relative path, including the extension |
| `doc_id` | SHA-256 of `project-name + NUL + relative-path` |
| `description` | `Project <name> — <relative path>` |
| `tags` | List containing `git` |
| `site_url` | URL of the file at the exported SHA |
| `authors` | List containing the last committer's name |
| `creation_date` | Date when the path was created |
| `last_update_date` | Date of its last change |
| `visibility` | `Internal` |

Dates use the **committer** timestamp in RFC 3339 UTC format. History follows first parents: a change introduced by a merge is attributed to the merge commit. A rename creates a new identity; deletion followed by recreation resets the creation date. Incomplete local history produces an error rather than an invented date.

Managed fields are replaced. Other YAML values are preserved, including values from aliases; their formatting and comments may be normalized. The Markdown body is preserved byte for byte. Only YAML frontmatter delimited by `---` is supported; invalid, TOML, or JSON frontmatter is rejected.

Public GitHub and GitLab URLs are inferred from the remote source, or from `origin` for a local repository. For a private instance or another host, provide `site_url_template` using `{repo_url}`, `{commit}`, and `{path}`. `{repo_url}` is the repository's HTTPS URL without credentials or the `.git` suffix. The document path is URL-encoded. An absolute template without `{repo_url}` also works for a local repository without a remote.

## Repeated exports and errors

Each project subdirectory contains `.git-exporter-manifest.json`, which lists its exports. A subsequent export updates current files and removes only previously recorded exports that are no longer present. Unmanaged files are preserved; a collision produces an error. Do not delete or modify the manifest for an existing export.

All contents and paths are validated and prepared before publication. A preparation error leaves previous files intact. Files are published through individual renames; publication is not an atomic transaction for the entire project if a system failure occurs. Symbolic-link ancestors and paths escaping the destination are rejected. A `.git-exporter.lock` directory prevents concurrent publication: after an abrupt shutdown, remove it only after checking that no export is still running.

Projects are processed sequentially. A failure does not prevent subsequent projects from running. The exit code is 0 if all projects succeed, or 1 otherwise. The summary reports the mode (`local`, `selective`, or `full`) and the numbers of exported and deleted files.

## Verification

```sh
go test ./...
go vet ./...
go build -o bin/git-exporter .
```

Tests create repositories, packs, and HTTPS/SSH Git servers in Go without running the Git CLI or contacting a hosting provider. They cover transmitted objects, fallback, metadata, blob batches, SSH verification, configuration, frontmatter, and manifest-controlled deletions.
