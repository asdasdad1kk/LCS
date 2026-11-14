package vcs

import (
	"encoding/json"
	"io"
	"path"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func jsonMarshal(v any) ([]byte, error)   { return json.MarshalIndent(v, "", "  ") }
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// DrawingChange is one drawing version introduced by a commit.
type DrawingChange struct {
	Commit  string `json:"commit"`
	Path    string `json:"path"`
	Blob    string `json:"blob"`
	Size    int64  `json:"size"`
	Author  string `json:"author"`
	Message string `json:"message"`
	When    int64  `json:"when"`
}

// History walks the commit log of a branch and returns every drawing revision
// it can derive, newest first. Used to rebuild the SQLite index from git.
func (r *Repo) History(branch string, limit int) ([]DrawingChange, error) {
	repo, err := open(r.Local)
	if err != nil {
		return nil, err
	}
	h, err := repo.ResolveRevision(plumbing.Revision(branchRef(branch)))
	if err != nil {
		return nil, err
	}
	iter, err := repo.Log(&git.LogOptions{From: *h})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	empty := &object.Tree{}
	out := []DrawingChange{}
	count := 0
	for {
		if limit > 0 && count >= limit {
			break
		}
		c, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		count++

		tree, err := c.Tree()
		if err != nil {
			return nil, err
		}
		parentTree := empty
		if c.NumParents() > 0 {
			if p, err := c.Parent(0); err == nil {
				if pt, err := p.Tree(); err == nil {
					parentTree = pt
				}
			}
		}
		changes, err := object.DiffTree(parentTree, tree)
		if err != nil {
			return nil, err
		}
		for _, ch := range changes {
			name := ch.To.Name
			if name == "" || ch.To.TreeEntry.Hash.IsZero() {
				continue
			}
			if !strings.HasPrefix(name, DrawingsDir+"/") || !strings.EqualFold(path.Ext(name), ".dwg") {
				continue
			}
			var size int64
			if blob, err := repo.BlobObject(ch.To.TreeEntry.Hash); err == nil {
				size = blob.Size
			}
			out = append(out, DrawingChange{
				Commit:  c.Hash.String(),
				Path:    name,
				Blob:    ch.To.TreeEntry.Hash.String(),
				Size:    size,
				Author:  c.Author.Name,
				Message: strings.TrimSpace(c.Message),
				When:    c.Author.When.UnixMilli(),
			})
		}
	}
	return out, nil
}

// ChangedSince lists the files that differ between two revisions.
func (r *Repo) ChangedSince(from, to string) ([]string, error) {
	repo, err := open(r.Local)
	if err != nil {
		return nil, err
	}
	fromHash, err := resolveRevision(repo, from)
	if err != nil {
		return nil, err
	}
	toHash, err := resolveRevision(repo, to)
	if err != nil {
		return nil, err
	}
	return diffPaths(repo, *fromHash, *toHash)
}

// CommitTime returns the author time of the branch tip.
func (r *Repo) CommitTime(branch string) (time.Time, error) {
	repo, err := open(r.Local)
	if err != nil {
		return time.Time{}, err
	}
	h, err := resolveRevision(repo, branch)
	if err != nil {
		return time.Time{}, err
	}
	c, err := repo.CommitObject(*h)
	if err != nil {
		return time.Time{}, err
	}
	return c.Author.When, nil
}
