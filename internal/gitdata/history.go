package gitdata

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type File struct {
	Path string
	Hash plumbing.Hash
	Mode filemode.FileMode
}
type Metadata struct {
	Created, Updated time.Time
	Committer        string
}

// ordinary reports whether a tree entry represents a regular or executable file.
func ordinary(mode filemode.FileMode) bool {
	return mode == filemode.Regular || mode == filemode.Executable || mode == filemode.Deprecated
}

// MarkdownFiles recursively lists regular Markdown files in a commit, ignoring extension case.
// It walks tree entries without loading blobs, unlike FileIter.
func MarkdownFiles(commit *object.Commit) ([]File, error) {
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	var files []File
	var walk func(*object.Tree, string) error
	walk = func(tree *object.Tree, prefix string) error {
		for _, entry := range tree.Entries {
			name := prefix + entry.Name
			if entry.Mode == filemode.Dir {
				child, err := tree.Tree(entry.Name)
				if err != nil {
					return err
				}
				if err := walk(child, name+"/"); err != nil {
					return err
				}
			} else if ordinary(entry.Mode) && strings.EqualFold(path.Ext(name), ".md") {
				files = append(files, File{name, entry.Hash, entry.Mode})
			}
		}
		return nil
	}
	err = walk(tree, "")
	return files, err
}

// History follows first-parent history to find each file's creation and last change,
// including the last committer, since its most recent creation at the current path.
// It compares tree entries without reading historical file contents.
func History(ctx context.Context, commit *object.Commit, files []File) (map[string]Metadata, error) {
	result := map[string]Metadata{}
	active := map[string]File{}
	for _, file := range files {
		active[file.Path] = file
	}
	for len(active) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var parent *object.Commit
		var parentTree *object.Tree
		var err error
		if commit.NumParents() > 0 {
			parent, err = commit.Parent(0)
			if err != nil {
				return nil, err
			}
			parentTree, err = parent.Tree()
			if err != nil {
				return nil, err
			}
		}
		for name, current := range active {
			var previous *object.TreeEntry
			if parentTree != nil {
				previous, err = parentTree.FindEntry(name)
				if err != nil && !errors.Is(err, object.ErrEntryNotFound) && !errors.Is(err, object.ErrDirectoryNotFound) {
					return nil, err
				}
				if err != nil {
					previous = nil
				}
			}
			meta, found := result[name]
			exists := previous != nil && ordinary(previous.Mode)
			changed := !exists || previous.Hash != current.Hash || previous.Mode != current.Mode
			if changed && !found {
				meta.Updated = commit.Committer.When.UTC()
				meta.Committer = commit.Committer.Name
			}
			if !exists {
				meta.Created = commit.Committer.When.UTC()
				delete(active, name)
			} else {
				active[name] = File{name, previous.Hash, previous.Mode}
			}
			if changed || found {
				result[name] = meta
			}
		}
		if parent == nil {
			break
		}
		commit = parent
	}
	return result, nil
}
