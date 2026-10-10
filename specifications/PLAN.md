# Go CLI for exporting Markdown from Git

## Summary

Create `git-exporter`, a standalone binary using Cobra, Viper, and [go-git](https://github.com/go-git/go-git). It will export Markdown files from local or remote repositories, enrich their frontmatter, and remove obsolete exports.

Use a recent stable Go version, with dependency versions pinned in `go.mod` and `go.sum`. No Git executable will be required at runtime.

## Interface and configuration

Main command:

```sh
git-exporter export --config config.yaml --output ./export
git-exporter export --repo documentation=https://github.com/org/docs.git \
  --ref documentation=main --output ./export
```

- Repeatable `--repo name=source` to define multiple projects; names must be unique and usable as directory names.
- Repeatable options keyed by project name: `--ref`, `--token-env`, `--username`, `--ssh-key`, `--ssh-passphrase-env`, `--site-url-template`.
- YAML configuration containing `output` and a `projects` list with the same settings.
- Precedence: explicitly provided CLI options, then configuration, then defaults. Merge projects by name; an option targeting an unknown project produces an error.
- Resolve relative YAML paths from the configuration directory and CLI paths from the current directory.
- HTTPS authentication using a token read from an environment variable; SSH using an agent or private key, with `known_hosts` verification. Do not display secrets.

## Export behavior

- Open local repositories read-only. For remote repositories, use the Git HTTPS/SSH protocol directly through go-git: fetch commits and trees with `blob:none`, then only the selected commit's Markdown blobs in deduplicated batches. Do not invoke a Git CLI or hosting provider APIs.
- If the server does not support filtering or explicitly rejects targeted objects, log the reason and perform a full clone (accessible branches and tags) in a new temporary directory, without a checkout, submodules, or Git LFS. Preserve the selected commit and clean up temporary storage. Network, authentication, and integrity errors do not trigger fallback.
- Export the commit identified by a branch, tag, or SHA; default to `HEAD`. Ignore local changes.
- Traverse regular files with a case-insensitive `.md` extension. Exclude symbolic links and submodules.
- Write to `<output>/<project>/<original-path>`.
- Separate responsibilities: configuration/CLI, Git access and history, Markdown transformation, and export synchronization.

### Frontmatter

Read and produce YAML frontmatter. Preserve unmanaged fields and the Markdown body; replace the following fields:

| Field | Value |
|---|---|
| `title` | Relative path including the filename and extension |
| `doc_id` | SHA-256 of the project name, a NUL separator, and the relative path |
| `description` | `Project <name> — <relative path>` |
| `tags` | List containing `git` |
| `site_url` | URL of the file at the exported SHA |
| `authors` | List containing the name of the last committer who changed the path |
| `creation_date` | Date when the path first appeared |
| `last_update_date` | Date of the last commit that changed the path |
| `visibility` | `Internal` |

- Use commit dates in RFC 3339 UTC format.
- Follow the exported commit's first-parent history; changes introduced by merges are attributed to the merge commit.
- A rename creates a new path and ID; deletion followed by recreation resets the creation date.
- Detect public GitHub/GitLab hosts from the remote URL, or `origin` for local repositories. Otherwise, use a configurable template with `{repo_url}`, `{commit}`, and `{path}`; encode the path correctly.
- If no usable URL is available, request configuration through an explicit error.
- Reject malformed or unsupported frontmatter before publishing the project's files.

### Synchronization and errors

- Maintain a manifest of generated files for each project in the destination.
- Prepare all files for a project before publication; then remove only its previous exports that are no longer present in the source.
- Refuse to overwrite existing files not recorded in the manifest. Verify that all paths remain within the destination and do not traverse symbolic links.
- A preparation error preserves the project's previous exports. Continue processing other projects and exit with a nonzero code if any project fails.
- Display a per-project summary: exported files, deleted files, and errors.

## Validation and delivery

- Protocol tests with Go HTTPS/SSH servers: transmitted objects, filtering, blob batches, automatic fallback, and no fallback on network/authentication/integrity errors.
- Tests using temporary repositories created with go-git: multiple projects, branches/tags, ignored local changes, additions, modifications, merges, renames, deletions, and recreation.
- Verify stable IDs, dates, encoded URLs, and preservation of the body and custom fields.
- Test CLI/configuration precedence, name collisions, authentication errors, and invalid frontmatter.
- Test successive exports, manifest-controlled deletion, and preservation of unmanaged files.
- Run `go test ./...`, `go vet ./...`, and build the binary.
- Provide a README and example configuration covering local repositories, HTTPS, SSH, and multiple projects.

## Assumptions

Process projects sequentially, without a persistent cache or scheduled execution in this first version. The configured project name is its stable identity: changing it changes the `doc_id` values and export directory.
