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

	// Shared bare repo: la estructura mínima a mano (go-git crea decenas de
	// archivos y carpetas; sobre un NAS cada uno es un viaje de red).
	if _, err := nas.Stat(filepath.Join(bare, "HEAD")); err != nil {
		for _, d := range []string{"objects", filepath.Join("refs", "heads")} {
			if err := nas.MkdirAll(filepath.Join(bare, d), 0o755); err != nil {
				return fmt.Errorf("init bare: %w", err)
			}
		}
		if err := nas.WriteFile(filepath.Join(bare, "config"), []byte("[core]\n\trepositoryformatversion = 0\n\tfilemode = false\n\tbare = true\n"), 0o644); err != nil {
			return fmt.Errorf("init bare: %w", err)
		}
		// HEAD al final: es lo que marca que el repositorio existe.
		if err := nas.WriteFile(filepath.Join(bare, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
			return fmt.Errorf("init bare: %w", err)
		}
	}

	if _, err := r.Remote("origin"); err != nil {
		if _, err := r.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{bare}}); err != nil {
			return err
		}
	}

	head, err := r.Head()
	if err != nil {
		return err
	}
	rp := &Repo{Bare: bare, Local: local}
	return rp.publish(r, MainBranch, head.Hash())
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
	defer slow("ListWorkBranches")()
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
	defer closeRepo(repo)
	if err := nas.Remove(refPath(r.Bare, name)); err == nil {
		markGone(r.Bare, name)
	}
	_ = repo.Storer.RemoveReference(plumbing.NewRemoteReferenceName("origin", name))
	return repo.Storer.RemoveReference(branchRef(name))
}

// ---------------------------------------------------------------- sync

// Pull brings the local clone up to date with the shared repository. Ya no se
// mueve ningún directorio de trabajo: todo se lee de la base de objetos.
func (r *Repo) Pull(branch string) error {
	return r.SyncBranches()
}

// SyncBranches mirrors every branch of the shared repository into the local
// clone. Branches with unpushed commits are kept as-is.
func (r *Repo) SyncBranches() error {
	defer slow("SyncBranches")()
	if err := r.Fetch(); err != nil {
		return err
	}
	repo, err := open(r.Local)
	if err != nil {
		return err
	}
	defer closeRepo(repo)
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
	branches, err := repo.Branches()
	if err != nil {
		return err
	}
	stale := []plumbing.ReferenceName{}
	_ = branches.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().Short()
		if strings.HasPrefix(name, BranchPrefix) && !onRemote[name] {
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

// Push publica una rama local en la carpeta compartida.
func (r *Repo) Push(branch string) error {
	repo, err := open(r.Local)
	if err != nil {
		return err
	}
	defer closeRepo(repo)
	ref, err := repo.Reference(branchRef(branch), true)
	if err != nil {
		return err
	}
	return r.publish(repo, branch, ref.Hash())
}

// ------------------------------------------------------------------ commit

func contentChanges(files map[string][]byte, removes []string) map[string]change {
	out := make(map[string]change, len(files)+len(removes))
	for p, content := range files {
		if content == nil {
			content = []byte{}
		}
		out[p] = change{data: content}
	}
	for _, p := range removes {
		out[p] = change{remove: true}
	}
	return out
}

// CommitFilesOnBranch agrega archivos a una rama y la publica (las
// aprobaciones son archivos nuevos con nombre único: nadie pisa a nadie).
func (r *Repo) CommitFilesOnBranch(branch string, files map[string][]byte, msg, actor string) (string, error) {
	h, err := r.commit(commitSpec{
		Target: branch, Msg: msg, Actor: actor,
		Build: func(plumbing.Hash) (map[string]change, []plumbing.Hash, error) {
			return contentChanges(files, nil), nil, nil
		},
	})
	if err != nil {
		return "", err
	}
	return h.String(), nil
}

// CreateBranch forks a new work branch from main and commits the task metadata
// so any machine can discover it straight from the shared repository. meta se
// completa con el commit base real (la punta de main al momento de crear).
func (r *Repo) CreateBranch(branch string, meta *BranchMeta, actor string) (string, error) {
	h, err := r.commit(commitSpec{
		Target: branch, From: MainBranch,
		Msg: "LCS: abrir rama " + meta.Title, Actor: actor,
		Build: func(base plumbing.Hash) (map[string]change, []plumbing.Hash, error) {
			meta.BaseCommit = base.String()
			raw, err := jsonMarshal(meta)
			if err != nil {
				return nil, nil, err
			}
			return map[string]change{BranchMetaFile: {data: raw}}, nil, nil
		},
	})
	if err != nil {
		return "", err
	}
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
	h, err := r.commit(commitSpec{
		Target: branch, Msg: msg, Actor: actor,
		Build: func(plumbing.Hash) (map[string]change, []plumbing.Hash, error) {
			return contentChanges(files, removes), nil, nil
		},
	})
	if err != nil {
		return nil, "", err
	}
	blobs := make(map[string]string, len(files))
	for p, content := range files {
		blobs[p] = BlobHash(content)
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
// unless strategy is "take-branch". Los planos se reutilizan por su hash: no se
// leen ni se vuelven a escribir.
func (r *Repo) MergeBranch(branch, baseCommit, actor, strategy, message string) (*MergeResult, error) {
	if message == "" {
		message = "LCS: integrar " + branch
	}
	repo, err := open(r.Local)
	if err != nil {
		return nil, err
	}
	defer closeRepo(repo)

	var applied []string
	h, err := r.commit(commitSpec{
		Target: MainBranch, Msg: message, Actor: actor, Need: []string{branch},
		Build: func(mainHead plumbing.Hash) (map[string]change, []plumbing.Hash, error) {
			branchHead, err := r.tip(repo, branch)
			if err != nil {
				return nil, nil, err
			}
			baseHash := plumbing.NewHash(baseCommit)
			changedBranch, err := diffPaths(repo, baseHash, branchHead)
			if err != nil {
				return nil, nil, err
			}
			changedMain, err := diffPaths(repo, baseHash, mainHead)
			if err != nil {
				return nil, nil, err
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
				return nil, nil, &ErrConflict{Paths: conflicts}
			}

			tree, err := treeOf(repo, branchHead)
			if err != nil {
				return nil, nil, err
			}
			changes := map[string]change{}
			applied = applied[:0]
			for _, p := range changedBranch {
				if strings.HasPrefix(p, MetaDir+"/") {
					continue
				}
				e, err := tree.FindEntry(p)
				if err != nil || !e.Mode.IsFile() {
					changes[p] = change{remove: true}
				} else {
					changes[p] = change{blob: e.Hash}
				}
				applied = append(applied, p)
			}
			// main never carries branch metadata
			changes[BranchMetaFile] = change{remove: true}
			return changes, []plumbing.Hash{branchHead}, nil
		},
	})
	if err != nil {
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
	defer slow("ListDrawings")()
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
			out = append(out, TreeEntry{Path: name, Size: r.blobSize(repo, entry.Hash), Hash: entry.Hash.String()})
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
