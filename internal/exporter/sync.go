package exporter

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const manifestName = ".git-exporter-manifest.json"

type manifest struct {
	Version int      `json:"version"`
	Project string   `json:"project"`
	Files   []string `json:"files"`
}

// safeName reports whether name is a relative Markdown path without traversal,
// backslashes, or colons that could make it unsafe across platforms.
func safeName(name string) bool {
	return name != "." && fs.ValidPath(name) && !strings.Contains(name, "\\") && !strings.Contains(name, ":") && strings.EqualFold(path.Ext(name), ".md")
}

// noSymlinks rejects symbolic links at filename or any existing ancestor.
func noSymlinks(filename string) error {
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return err
	}
	for current := absolute; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link not allowed: %s", current)
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return nil
}

// Synchronize validates and stages a project's export under a destination lock,
// replaces managed files, removes obsolete exports, and updates the ownership manifest.
// It rejects collisions with unmanaged files and returns the number of files deleted.
func Synchronize(output, project string, files map[string][]byte) (int, error) {
	if project == "" || project == "." || project == ".." || strings.ContainsAny(project, "/\\:") {
		return 0, fmt.Errorf("invalid project name")
	}
	destination := filepath.Join(output, project)
	if err := noSymlinks(destination); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(destination, 0755); err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	const lockName = ".git-exporter.lock"
	if err := root.Mkdir(lockName, 0700); err != nil {
		return 0, fmt.Errorf("destination locked or inaccessible: %w", err)
	}
	defer root.Remove(lockName)
	if err := noSymlinks(filepath.Join(destination, manifestName)); err != nil {
		return 0, err
	}
	previous := manifest{Version: 1, Project: project}
	data, err := root.ReadFile(manifestName)
	if err == nil {
		if err := json.Unmarshal(data, &previous); err != nil {
			return 0, fmt.Errorf("invalid manifest: %w", err)
		}
		if previous.Version != 1 || previous.Project != project {
			return 0, fmt.Errorf("incompatible manifest")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	owned := map[string]bool{}
	for _, name := range previous.Files {
		if !safeName(name) || owned[name] {
			return 0, fmt.Errorf("invalid or duplicate path in manifest: %q", name)
		}
		owned[name] = true
	}
	names := make([]string, 0, len(files))
	all := map[string]bool{}
	for name := range owned {
		all[name] = true
	}
	for name := range files {
		if !safeName(name) {
			return 0, fmt.Errorf("invalid Markdown path: %q", name)
		}
		names = append(names, name)
		all[name] = true
	}
	sort.Strings(names)
	for name := range all {
		if err := noSymlinks(filepath.Join(destination, filepath.FromSlash(name))); err != nil {
			return 0, err
		}
		info, err := root.Lstat(filepath.FromSlash(name))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return 0, err
		}
		if err == nil {
			if !info.Mode().IsRegular() {
				return 0, fmt.Errorf("destination is not a regular file: %s", name)
			}
			if !owned[name] {
				return 0, fmt.Errorf("refusing to overwrite an unmanaged file: %s", name)
			}
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if _, conflict := files[parent]; conflict {
				return 0, fmt.Errorf("file/directory conflict: %s", parent)
			}
			if info, err := root.Stat(filepath.FromSlash(parent)); err == nil && !info.IsDir() {
				return 0, fmt.Errorf("expected a directory: %s", parent)
			} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return 0, err
			}
		}
	}
	stage, err := os.MkdirTemp(destination, ".git-exporter-stage-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(stage)
	stageName := filepath.Base(stage)
	for _, name := range names {
		target := filepath.Join(stage, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return 0, err
		}
		if err := os.WriteFile(target, files[name], 0644); err != nil {
			return 0, err
		}
	}
	encoded, err := json.MarshalIndent(manifest{1, project, names}, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(filepath.Join(stage, manifestName), append(encoded, '\n'), 0644); err != nil {
		return 0, err
	}
	for _, name := range names {
		local := filepath.FromSlash(name)
		if err := root.MkdirAll(filepath.Dir(local), 0755); err != nil {
			return 0, err
		}
		if err := root.Rename(filepath.Join(stageName, local), local); err != nil {
			return 0, err
		}
	}
	deleted := 0
	for _, name := range previous.Files {
		if _, exists := files[name]; exists {
			continue
		}
		if err := root.Remove(filepath.FromSlash(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return deleted, err
		} else if err == nil {
			deleted++
		}
	}
	if err := root.Rename(filepath.Join(stageName, manifestName), manifestName); err != nil {
		return deleted, err
	}
	return deleted, nil
}
