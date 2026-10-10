package exporter

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"git-exporter/internal/config"
	"git-exporter/internal/gitdata"
	"git-exporter/internal/markdown"
)

type Result struct {
	Mode              string
	Exported, Deleted int
	Files             []ExportedFile
}

// ExportedFile describes a published Markdown document and its source URL.
type ExportedFile struct {
	PageID string
	URL    string
	Path   string
}

// SafeError redacts configured token and SSH passphrase values, including their
// URL-encoded forms, from an error message before it is displayed.
func SafeError(err error, p config.Project) string {
	message := err.Error()
	for _, name := range []string{p.TokenEnv, p.SSHPassphraseEnv} {
		if secret := os.Getenv(name); secret != "" {
			for _, value := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
				message = strings.ReplaceAll(message, value, "[redacted]")
			}
		}
	}
	return message
}

// Run reads a project's committed Markdown files, enriches them with Git metadata,
// and synchronizes the destination only after all files have been prepared successfully.
func Run(ctx context.Context, output string, p config.Project, log io.Writer) (Result, error) {
	source, err := gitdata.Open(ctx, p, log)
	if err != nil {
		return Result{}, err
	}
	defer source.Close()
	result := Result{Mode: source.Mode}
	commit, err := source.Repo.CommitObject(source.Commit)
	if err != nil {
		return result, err
	}
	files, err := gitdata.MarkdownFiles(commit)
	if err != nil {
		return result, err
	}
	metadata, err := gitdata.History(ctx, commit, files)
	if err != nil {
		return result, err
	}
	prepared := map[string][]byte{}
	var exportedFiles []ExportedFile
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		site, err := markdown.SiteURL(source.URL, p.SiteURLTemplate, commit.Hash.String(), file.Path)
		if err != nil {
			return result, err
		}
		blob, err := source.Repo.BlobObject(file.Hash)
		if err != nil {
			return result, err
		}
		reader, err := blob.Reader()
		if err != nil {
			return result, err
		}
		content, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return result, readErr
		}
		if closeErr != nil {
			return result, closeErr
		}
		transformed, err := markdown.Transform(content, p.Name, file.Path, site, metadata[file.Path])
		if err != nil {
			return result, fmt.Errorf("%s: %w", file.Path, err)
		}
		prepared[file.Path] = transformed
		exportedFiles = append(exportedFiles, ExportedFile{
			PageID: markdown.DocumentID(p.Name, file.Path),
			URL:    site,
			Path:   filepath.Join(output, p.Name, filepath.FromSlash(file.Path)),
		})
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Deleted, err = Synchronize(output, p.Name, prepared)
	if err != nil {
		return result, err
	}
	result.Exported = len(prepared)
	result.Files = exportedFiles
	return result, nil
}
