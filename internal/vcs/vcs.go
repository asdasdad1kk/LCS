package vcs

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

const (
	MainBranch     = "main"
	DrawingsDir    = "drawings"
	MetaDir        = ".lcs"
	BranchMetaFile = ".lcs/branch.json"
	BranchPrefix   = "work/"
)

// ErrConflict is returned when a merge would overwrite a drawing that moved on
// main while the branch was open. The caller decides how to resolve it.
type ErrConflict struct {
	Paths []string
}

func (e *ErrConflict) Error() string {
	return "conflicto en: " + strings.Join(e.Paths, ", ")
}

var ErrNothingToDo = errors.New("nada para actualizar")

// Repo is a local clone of a shared bare repository.
type Repo struct {
	Bare  string
	Local string
}

var repoLocks sync.Map

func lockFor(local string) *sync.Mutex {
	v, _ := repoLocks.LoadOrStore(local, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// WithLock serializes every write operation on a local clone.
func (r *Repo) WithLock(fn func() error) error {
	mu := lockFor(r.Local)
	mu.Lock()
	defer mu.Unlock()
	return fn()
}

func open(p string) (*git.Repository, error) {
	return git.PlainOpen(p)
}

func branchRef(name string) plumbing.ReferenceName {
	return plumbing.NewBranchReferenceName(name)
}

// ---------------------------------------------------------------- bootstrap

// Init creates the local clone and the shared bare repository with an initial
// commit. Safe to call on an existing repository.
func Init(bare, local string, pf ProjectFile, actor string) error {
	projectName := pf.Name
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		return err
	}

	r, err := git.PlainOpen(local)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		r, err = git.PlainInitWithOptions(local, &git.PlainInitOptions{
			InitOptions: git.InitOptions{DefaultBranch: plumbing.Main},
			Bare:        false,
		})
	}
	if err != nil {
		return fmt.Errorf("init local: %w", err)
	}

	sig := signatureFor(actor)

	w, err := r.Worktree()
	if err != nil {
		return err
	}

	if _, err := r.Head(); err != nil {
		// First run: seed the tree.
		for _, d := range []string{"drawings"} {
			if err := os.MkdirAll(filepath.Join(local, d), 0o755); err != nil {
				return err
			}
		}
		if err := os.MkdirAll(filepath.Join(local, MetaDir), 0o755); err != nil {
			return err
		}
		_ = os.WriteFile(filepath.Join(local, "README.md"), []byte(projectReadme(projectName)), 0o644)
		_ = os.WriteFile(filepath.Join(local, MetaDir, "project.json"), pf.Marshal(), 0o644)
		_ = os.WriteFile(filepath.Join(local, "drawings", ".gitkeep"), []byte(""), 0o644)

		if _, err := w.Add("."); err != nil {
			return err
		}
		if _, err := w.Commit("LCS: crear proyecto "+projectName, &git.CommitOptions{Author: sig}); err != nil {
			return err
		}
	}

	// Shared bare repo.
	if _, err := os.Stat(filepath.Join(bare, "HEAD")); err != nil {
		if _, err := git.PlainInitWithOptions(bare, &git.PlainInitOptions{
			InitOptions: git.InitOptions{DefaultBranch: plumbing.Main},
			Bare:        true,
		}); err != nil {
			return fmt.Errorf("init bare: %w", err)
		}
	}

	if _, err := r.Remote("origin"); err != nil {
		if _, err := r.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{bare}}); err != nil {
			return err
		}
	}

	return r.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{"refs/heads/main:refs/heads/main"},
		Force:      false,
	})
}

func projectReadme(name string) string {
	return "# " + name + "\n\n" +
		"Repositorio gestionado por **LCS — Layout Control System**.\n\n" +
		"- `drawings/` contiene los DWG vigentes.\n" +
		"- Cada rama `work/*` corresponde a una orden de trabajo (descargar → editar → subir).\n" +
		"- No edites este repositorio a mano: usa la aplicación LCS.\n"
}

// ---------------------------------------------------------------- state

func (r *Repo) HeadOf(branch string) (string, error) {
	repo, err := open(r.Local)
	if err != nil {
		return "", err
	}
	h, err := repo.ResolveRevision(plumbing.Revision(branchRef(branch)))
	if err != nil {
		return "", err
	}
	return h.String(), nil
}

func (r *Repo) ListWorkBranches() ([]BranchMeta, error) {
	repo, err := open(r.Local)
	if err != nil {
		return nil, err
	}
	iter, err := repo.Branches()
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	out := []BranchMeta{}
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().Short()
		if !strings.HasPrefix(name, BranchPrefix) {
			return nil
		}
		meta, err := r.readBranchMetaFile(name)
		if err != nil || meta == nil {
			meta = &BranchMeta{Name: name, Title: name, CreatedAt: time.Now().UnixMilli()}
		}
		meta.Name = name
		meta.HeadCommit = ref.Hash().String()
		out = append(out, *meta)
		return nil
	})
	return out, err
}

func (r *Repo) readBranchMetaFile(branch string) (*BranchMeta, error) {
	raw, err := r.FileAtRef(branch, BranchMetaFile)
	if err != nil {
		return nil, err
	}
	var m BranchMeta
	if err := jsonUnmarshal(raw, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// FileAtRef reads a file blob from a branch.
func (r *Repo) FileAtRef(branch, p string) ([]byte, error) {
	return r.FileAt(branch, p)
}

// FileAt reads a file blob at any revision (branch name or commit sha).
func (r *Repo) FileAt(revision, p string) ([]byte, error) {
	repo, err := open(r.Local)
	if err != nil {
		return nil, err
	}
	h, err := resolveRevision(repo, revision)
	if err != nil {
		return nil, fmt.Errorf("revisión %s: %w", revision, err)
	}
	return readFileAt(repo, *h, p)
}

// BlobReader opens a file blob at any revision without loading it in memory.
// Use it for downloads: un DWG de 150 MB no tiene por qué pasar por un []byte.
func (r *Repo) BlobReader(revision, p string) (io.ReadCloser, int64, error) {
	repo, err := open(r.Local)
	if err != nil {
		return nil, 0, err
	}
	h, err := resolveRevision(repo, revision)
	if err != nil {
		return nil, 0, fmt.Errorf("revisión %s: %w", revision, err)
	}
	tree, err := treeOf(repo, *h)
	if err != nil {
		return nil, 0, err
	}
	f, err := tree.File(p)
	if err != nil {
		return nil, 0, os.ErrNotExist
	}
	rd, err := f.Reader()
	if err != nil {
		return nil, 0, err
	}
	return rd, f.Size, nil
}

func resolveRevision(repo *git.Repository, revision string) (*plumbing.Hash, error) {
	if len(revision) >= 7 && isHex(revision) {
		if h, err := repo.ResolveRevision(plumbing.Revision(revision)); err == nil {
			return h, nil
		}
	}
	return repo.ResolveRevision(plumbing.Revision(branchRef(revision)))
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// DeleteBranch removes the work branch locally and from the shared repository.
func (r *Repo) DeleteBranch(name string) error {
	repo, err := open(r.Local)
	if err != nil {
		return err
	}
	_ = repo.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec(":refs/heads/" + name)},
	})
	return repo.Storer.RemoveReference(branchRef(name))
}

// ---------------------------------------------------------------- sync

func (r *Repo) Fetch() error {
	repo, err := open(r.Local)
	if err != nil {
		return err
	}
	err = repo.Fetch(&git.FetchOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{"+refs/heads/*:refs/remotes/origin/*"},
		Force:      true,
		// Una rama descartada en otro equipo desaparece también de aquí.
		Prune: true,
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}

// Pull brings the local clone up to date with the shared repository and leaves
// the worktree on the requested branch.
func (r *Repo) Pull(branch string) error {
	if err := r.SyncBranches(); err != nil {
		return err
	}
	repo, err := open(r.Local)
	if err != nil {
		return err
	}
	w, err := repo.Worktree()
	if err != nil {
		return err
	}
	// Checkout con Force reescribe el árbol completo: con un DWG de 150 MB eso
	// son cientos de megas de I/O. Si ya estamos parados en el commit que
	// toca, no hay nada que reescribir.
	return checkoutIfNeeded(repo, w, branch)
}

// checkoutIfNeeded mueve el worktree solo cuando HEAD no coincide ya con la rama.
func checkoutIfNeeded(repo *git.Repository, w *git.Worktree, branch string) error {
	if head, err := repo.Head(); err == nil {
		if target, err := repo.ResolveRevision(plumbing.Revision(branchRef(branch))); err == nil {
			if head.Hash() == *target {
				return nil
			}
		}
	}
	return w.Checkout(&git.CheckoutOptions{Branch: branchRef(branch), Force: true})
}

// SyncBranches mirrors every branch of the shared repository into the local
// clone. Branches with unpushed commits are kept as-is.
func (r *Repo) SyncBranches() error {
	if err := r.Fetch(); err != nil {
		return err
	}
	repo, err := open(r.Local)
	if err != nil {
		return err
	}
	remoteIter, err := repo.References()
	if err != nil {
		return err
	}
	type refPair struct {
		name   string
		remote plumbing.Hash
	}
	pending := []refPair{}
	err = remoteIter.ForEach(func(ref *plumbing.Reference) error {
		n := ref.Name()
		if !n.IsRemote() || n.IsTag() {
			return nil
		}
		short := strings.TrimPrefix(string(n), "refs/remotes/origin/")
		if short == "HEAD" || short == "" {
			return nil
		}
		pending = append(pending, refPair{name: short, remote: ref.Hash()})
		return nil
	})
	remoteIter.Close()
	if err != nil {
		return err
	}

	for _, p := range pending {
		localRef := branchRef(p.name)
		cur, err := repo.Reference(localRef, false)
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			if err := repo.Storer.SetReference(plumbing.NewHashReference(localRef, p.remote)); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if cur.Hash() == p.remote {
			continue
		}
		localCommit, err1 := repo.CommitObject(cur.Hash())
		remoteCommit, err2 := repo.CommitObject(p.remote)
		if err1 != nil || err2 != nil {
			continue
		}
		bases, err := localCommit.MergeBase(remoteCommit)
		if err != nil || len(bases) == 0 {
			if err := repo.Storer.SetReference(plumbing.NewHashReference(localRef, p.remote)); err != nil {
				return err
			}
			continue
		}
		switch bases[0].Hash {
		case cur.Hash():
			// Only the shared branch moved: fast-forward the local ref.
			if err := repo.Storer.SetReference(plumbing.NewHashReference(localRef, p.remote)); err != nil {
				return err
			}
		default:
			// Local carries unpushed commits (or diverged): keep them.
			continue
		}
	}

	// Toda escritura se publica al instante, así que una rama de trabajo local
	// que ya no existe en la carpeta compartida fue descartada en otro equipo.
	// Se retira para que no figure como tarea abierta.
	onRemote := map[string]bool{}
	for _, p := range pending {
		onRemote[p.name] = true
	}
	head, _ := repo.Head()
	branches, err := repo.Branches()
	if err != nil {
		return err
	}
	stale := []plumbing.ReferenceName{}
	_ = branches.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().Short()
		if strings.HasPrefix(name, BranchPrefix) && !onRemote[name] && (head == nil || head.Name() != ref.Name()) {
			stale = append(stale, ref.Name())
		}
		return nil
	})
	branches.Close()
	for _, n := range stale {
		_ = repo.Storer.RemoveReference(n)
	}
	return nil
}

func (r *Repo) Push(branch string) error {
	repo, err := open(r.Local)
	if err != nil {
		return err
	}
	err = repo.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch)},
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}

// ------------------------------------------------------------------ commit

func (r *Repo) checkout(branch string, create bool) (*git.Repository, *git.Worktree, error) {
	repo, err := open(r.Local)
	if err != nil {
		return nil, nil, err
	}
	w, err := repo.Worktree()
	if err != nil {
		return nil, nil, err
	}
	err = w.Checkout(&git.CheckoutOptions{Branch: branchRef(branch), Create: create, Force: true})
	return repo, w, err
}

func (r *Repo) commitAll(w *git.Worktree, msg, actor string, extraParents []plumbing.Hash) (plumbing.Hash, error) {
	repo, err := open(r.Local)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	sig := signatureFor(actor)

	// Stage everything (including deletions) before committing: go-git only
	// commits what the index already knows about.
	if err := w.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return plumbing.ZeroHash, err
	}

	// Normal commit: HEAD is the only parent.
	h, err := w.Commit(msg, &git.CommitOptions{Author: sig, Committer: sig, AllowEmptyCommits: true})
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if len(extraParents) <= 1 {
		return h, nil
	}

	// Merge commit: same tree, several parents. The single-parent commit above
	// is left unreferenced and gets pruned by git gc.
	base, err := repo.CommitObject(h)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	merge := &object.Commit{
		Author:       *sig,
		Committer:    *sig,
		Message:      msg,
		TreeHash:     base.TreeHash,
		ParentHashes: extraParents,
	}
	obj := repo.Storer.NewEncodedObject()
	if err := merge.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return repo.Storer.SetEncodedObject(obj)
}

// CommitOnBranch stages every change in the worktree and commits it, keeping
// the branch tip as the single parent.
func (r *Repo) CommitOnBranch(branch, msg, actor string) (string, error) {
	repo, w, err := r.checkout(branch, false)
	if err != nil {
		return "", err
	}
	if status, err := w.Status(); err == nil && status.IsClean() {
		return "", ErrNothingToDo
	}
	head, err := repo.Head()
	if err != nil {
		return "", err
	}
	h, err := r.commitAll(w, msg, actor, []plumbing.Hash{head.Hash()})
	if err != nil {
		return "", err
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference(branchRef(branch), h)); err != nil {
		return "", err
	}
	return h.String(), nil
}

// CreateBranch forks a new work branch from main and commits the task metadata
// so any machine can discover it straight from the shared repository.
func (r *Repo) CreateBranch(branch string, meta BranchMeta, actor string) (string, error) {
	if err := r.Pull(MainBranch); err != nil {
		return "", err
	}
	repo, w, err := r.checkout(branch, true)
	if err != nil {
		return "", err
	}
	raw, err := jsonMarshal(meta)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(r.Local, MetaDir), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(r.Local, BranchMetaFile), raw, 0o644); err != nil {
		return "", err
	}
	head, err := repo.Head()
	if err != nil {
		return "", err
	}
	h, err := r.commitAll(w, "LCS: abrir rama "+meta.Title, actor, []plumbing.Hash{head.Hash()})
	if err != nil {
		return "", err
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference(branchRef(branch), h)); err != nil {
		return "", err
	}
	if err := r.Push(branch); err != nil {
		return "", err
	}
	_ = r.ReturnToMain() // ver ReturnToMain: no dejar una rama de trabajo abierta
	return h.String(), nil
}

// WriteAndCommit replaces a file on the branch and commits the change.
func (r *Repo) WriteAndCommit(branch, relPath string, content []byte, msg, actor string) (commitSHA string, blobSHA string, err error) {
	res, commit, err := r.WriteFilesAndCommit(branch, map[string][]byte{relPath: content}, nil, msg, actor)
	if err != nil {
		return "", "", err
	}
	return commit, res[relPath], nil
}

// WriteFilesAndCommit writes several files on the branch and records one commit.
// It returns the blob hash of every written path.
func (r *Repo) WriteFilesAndCommit(branch string, files map[string][]byte, removes []string, msg, actor string) (map[string]string, string, error) {
	if err := r.Pull(branch); err != nil {
		return nil, "", err
	}
	repo, w, err := r.checkout(branch, false)
	if err != nil {
		return nil, "", err
	}
	blobs := map[string]string{}
	for p, content := range files {
		dst := filepath.Join(r.Local, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, "", err
		}
		if err := os.WriteFile(dst, content, 0o644); err != nil {
			return nil, "", err
		}
		blobs[p] = BlobHash(content)
	}
	for _, p := range removes {
		_ = os.Remove(filepath.Join(r.Local, filepath.FromSlash(p)))
	}
	head, err := repo.Head()
	if err != nil {
		return nil, "", err
	}
	h, err := r.commitAll(w, msg, actor, []plumbing.Hash{head.Hash()})
	if err != nil {
		return nil, "", err
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference(branchRef(branch), h)); err != nil {
		return nil, "", err
	}
	if err := r.Push(branch); err != nil {
		return nil, "", err
	}
	if branch != MainBranch {
		_ = r.ReturnToMain() // ver ReturnToMain: no dejar una rama de trabajo abierta
	}
	return blobs, h.String(), nil
}

// ---------------------------------------------------------------- merge

type MergeResult struct {
	Commit string   `json:"commit"`
	Paths  []string `json:"paths"`
}

// MergeBranch brings the branch content for every drawing it touched into main.
// It refuses to overwrite drawings that changed on main since the branch point
// unless strategy is "take-branch".
func (r *Repo) MergeBranch(branch, baseCommit, actor, strategy, message string) (*MergeResult, error) {
	if err := r.Pull(MainBranch); err != nil {
		return nil, err
	}
	repo, err := open(r.Local)
	if err != nil {
		return nil, err
	}

	branchHead, err := repo.ResolveRevision(plumbing.Revision(branchRef(branch)))
	if err != nil {
		return nil, err
	}
	mainHeadRef, err := repo.Head()
	if err != nil {
		return nil, err
	}

	baseHash := plumbing.NewHash(baseCommit)
	changedBranch, err := diffPaths(repo, baseHash, *branchHead)
	if err != nil {
		return nil, err
	}
	changedMain, err := diffPaths(repo, baseHash, mainHeadRef.Hash())
	if err != nil {
		return nil, err
	}

	mainSet := map[string]bool{}
	for _, p := range changedMain {
		mainSet[p] = true
	}
	conflicts := []string{}
	for _, p := range changedBranch {
		if mainSet[p] && !strings.HasPrefix(p, MetaDir+"/") {
			conflicts = append(conflicts, p)
		}
	}
	if len(conflicts) > 0 && strategy != "take-branch" {
		return nil, &ErrConflict{Paths: conflicts}
	}

	w, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	if err := checkoutIfNeeded(repo, w, MainBranch); err != nil {
		return nil, err
	}

	applied := []string{}
	for _, p := range changedBranch {
		if strings.HasPrefix(p, MetaDir+"/") {
			continue
		}
		dst := filepath.Join(r.Local, filepath.FromSlash(p))
		blob, err := readFileAt(repo, *branchHead, p)
		if errors.Is(err, os.ErrNotExist) {
			_ = os.Remove(dst)
			applied = append(applied, p)
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dst, blob, 0o644); err != nil {
			return nil, err
		}
		applied = append(applied, p)
	}

	// main never carries branch metadata
	_ = os.Remove(filepath.Join(r.Local, filepath.FromSlash(BranchMetaFile)))

	if message == "" {
		message = "LCS: integrar " + branch
	}
	h, err := r.commitAll(w, message, actor, []plumbing.Hash{mainHeadRef.Hash(), *branchHead})
	if err != nil {
		return nil, err
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference(branchRef(MainBranch), h)); err != nil {
		return nil, err
	}
	if err := w.Reset(&git.ResetOptions{Commit: h, Mode: git.HardReset}); err != nil {
		return nil, err
	}
	if err := r.Push(MainBranch); err != nil {
		return nil, err
	}
	sort.Strings(applied)
	return &MergeResult{Commit: h.String(), Paths: applied}, nil
}

func diffPaths(repo *git.Repository, from, to plumbing.Hash) ([]string, error) {
	fromTree, err := treeOf(repo, from)
	if err != nil {
		return nil, err
	}
	toTree, err := treeOf(repo, to)
	if err != nil {
		return nil, err
	}
	changes, err := object.DiffTree(fromTree, toTree)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, c := range changes {
		p := c.To.Name
		if p == "" {
			p = c.From.Name
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func treeOf(repo *git.Repository, h plumbing.Hash) (*object.Tree, error) {
	c, err := repo.CommitObject(h)
	if err != nil {
		return nil, err
	}
	return c.Tree()
}

func readFileAt(repo *git.Repository, commit plumbing.Hash, p string) ([]byte, error) {
	tree, err := treeOf(repo, commit)
	if err != nil {
		return nil, err
	}
	f, err := tree.File(p)
	if err != nil {
		return nil, os.ErrNotExist
	}
	rd, err := f.Reader()
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	return io.ReadAll(rd)
}

// ---------------------------------------------------------------- helpers

// ListTree returns every drawing file present at the given ref.
type TreeEntry struct {
	Path string
	Size int64
	Hash string
}

func (r *Repo) ListDrawings(branch string) ([]TreeEntry, error) {
	repo, err := open(r.Local)
	if err != nil {
		return nil, err
	}
	h, err := repo.ResolveRevision(plumbing.Revision(branchRef(branch)))
	if err != nil {
		return nil, err
	}
	commit, err := repo.CommitObject(*h)
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	out := []TreeEntry{}
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	for {
		name, entry, err := walker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if entry.Mode.IsFile() && strings.HasPrefix(name, DrawingsDir+"/") &&
			strings.EqualFold(path.Ext(name), ".dwg") {
			var size int64
			if blob, err := repo.BlobObject(entry.Hash); err == nil {
				size = blob.Size
			}
			out = append(out, TreeEntry{Path: name, Size: size, Hash: entry.Hash.String()})
		}
	}
	return out, nil
}

func (r *Repo) Log(branch string, limit int) ([]LogEntry, error) {
	repo, err := open(r.Local)
	if err != nil {
		return nil, err
	}
	h, err := repo.ResolveRevision(plumbing.Revision(branchRef(branch)))
	if err != nil {
		return nil, err
	}
	iter, err := repo.Log(&git.LogOptions{From: *h, Order: git.LogOrderCommitterTime})
	if err != nil {
		return nil, err
	}
	out := []LogEntry{}
	err = iter.ForEach(func(c *object.Commit) error {
		if len(out) >= limit {
			return storer.ErrStop
		}
		out = append(out, LogEntry{
			SHA:     c.Hash.String(),
			Message: strings.TrimSpace(c.Message),
			Author:  c.Author.Name,
			When:    c.Author.When.UnixMilli(),
		})
		return nil
	})
	return out, err
}

func BlobHash(content []byte) string {
	h := sha1.New()
	h.Write([]byte(fmt.Sprintf("blob %d\x00", len(content))))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

type LogEntry struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
	Author  string `json:"author"`
	When    int64  `json:"when"`
}

type BranchMeta struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Author      string   `json:"author"`
	AuthorEmail string   `json:"authorEmail,omitempty"`
	BaseCommit  string   `json:"baseCommit"`
	HeadCommit  string   `json:"headCommit"`
	CreatedAt   int64    `json:"createdAt"`
	Drawings    []string `json:"drawings"`
	Notes       string   `json:"notes"`
}

// Meta reads the task descriptor committed at the tip of a work branch.
func (r *Repo) Meta(branch string) (*BranchMeta, error) { return r.readBranchMetaFile(branch) }
