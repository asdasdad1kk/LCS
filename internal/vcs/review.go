package vcs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Todo lo que decide qué proyectos existen y quién aprueba vive en git, dentro
// de la carpeta compartida (el NAS): no hay servidor y la base SQLite de cada
// equipo es solo una copia para consultar rápido.

const (
	ProjectFilePath = MetaDir + "/project.json"
	ApprovalsDir    = MetaDir + "/approvals"
)

// Approver es un nodo de la cadena de aprobación de un proyecto.
type Approver struct {
	Email string `json:"email"`
	Label string `json:"label,omitempty"`
}

// ProjectFile es .lcs/project.json en main: identidad del proyecto y su
// cadena de aprobación (el orden de la lista es el orden de los nodos).
type ProjectFile struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Parent      string     `json:"parent,omitempty"`
	Owner       string     `json:"owner,omitempty"`
	Approvers   []Approver `json:"approvers"`
}

func (p ProjectFile) Marshal() []byte {
	if p.Approvers == nil {
		p.Approvers = []Approver{}
	}
	raw, _ := json.MarshalIndent(p, "", "  ")
	return append(raw, '\n')
}

// ReadProjectFile lee .lcs/project.json del main del clon local.
func (r *Repo) ReadProjectFile() (*ProjectFile, error) {
	raw, err := r.FileAt(MainBranch, ProjectFilePath)
	if err != nil {
		return nil, err
	}
	var pf ProjectFile
	if err := json.Unmarshal(raw, &pf); err != nil {
		return nil, err
	}
	return &pf, nil
}

// ReadBareProject lee .lcs/project.json directo del repositorio compartido,
// sin necesitar un clon: así otro equipo descubre proyectos que no creó él.
func ReadBareProject(bare string) (*ProjectFile, error) {
	repo, err := git.PlainOpen(bare)
	if err != nil {
		return nil, err
	}
	ref, err := repo.Reference(branchRef(MainBranch), true)
	if err != nil {
		return nil, err
	}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, err
	}
	f, err := commit.File(ProjectFilePath)
	if err != nil {
		return nil, err
	}
	content, err := f.Contents()
	if err != nil {
		return nil, err
	}
	var pf ProjectFile
	if err := json.Unmarshal([]byte(content), &pf); err != nil {
		return nil, err
	}
	return &pf, nil
}

// EnsureClone crea el clon local de un proyecto que ya existe en la carpeta
// compartida. Si el clon ya está, no hace nada.
func EnsureClone(bare, local string) error {
	if _, err := git.PlainOpen(local); err == nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(bare, "HEAD")); err != nil {
		return fmt.Errorf("no existe el repositorio compartido %s", bare)
	}
	_, err := git.PlainClone(local, false, &git.CloneOptions{URL: bare})
	if err != nil {
		_ = os.RemoveAll(local) // no dejar un clon a medias
		return fmt.Errorf("clonando %s: %w", bare, err)
	}
	return nil
}

// TryLock ejecuta fn solo si nadie más está usando el clon; si está ocupado
// devuelve false sin esperar.
func (r *Repo) TryLock(fn func() error) (bool, error) {
	mu := lockFor(r.Local)
	if !mu.TryLock() {
		return false, nil
	}
	defer mu.Unlock()
	return true, fn()
}

// ReturnToMain deja el directorio de trabajo en main. Una rama de trabajo no
// debe quedar abierta: si otro equipo la avanza, los archivos del directorio
// quedarían desfasados respecto de la rama y el siguiente commit los revertiría.
func (r *Repo) ReturnToMain() error {
	repo, err := open(r.Local)
	if err != nil {
		return err
	}
	w, err := repo.Worktree()
	if err != nil {
		return err
	}
	return checkoutIfNeeded(repo, w, MainBranch)
}

// DrawingsTreeHash identifica el contenido exacto de drawings/ en una
// revisión. Una aprobación se ata a este valor: cualquier cambio en un plano
// la deja obsoleta, y resubir los mismos bytes no.
func (r *Repo) DrawingsTreeHash(revision string) (string, error) {
	repo, err := open(r.Local)
	if err != nil {
		return "", err
	}
	h, err := resolveRevision(repo, revision)
	if err != nil {
		return "", err
	}
	tree, err := treeOf(repo, *h)
	if err != nil {
		return "", err
	}
	e, err := tree.FindEntry(DrawingsDir)
	if err != nil {
		return "", nil // sin drawings/ todavía
	}
	return e.Hash.String(), nil
}

// NamedFile es un archivo pequeño leído de una revisión.
type NamedFile struct {
	Name    string
	Content []byte
}

// ListFilesAt lee los archivos (no recursivo) de una carpeta en una revisión.
func (r *Repo) ListFilesAt(revision, dir string) ([]NamedFile, error) {
	repo, err := open(r.Local)
	if err != nil {
		return nil, err
	}
	h, err := resolveRevision(repo, revision)
	if err != nil {
		return nil, err
	}
	tree, err := treeOf(repo, *h)
	if err != nil {
		return nil, err
	}
	sub, err := tree.Tree(dir)
	if err != nil {
		return nil, nil // la carpeta no existe: no hay archivos
	}
	out := []NamedFile{}
	for _, e := range sub.Entries {
		if !e.Mode.IsFile() {
			continue
		}
		f, err := sub.TreeEntryFile(&e)
		if err != nil {
			continue
		}
		rd, err := f.Reader()
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(rd, 1<<20))
		rd.Close()
		if err != nil {
			continue
		}
		out = append(out, NamedFile{Name: e.Name, Content: b})
	}
	return out, nil
}

// IsMerged dice si el extremo de la rama ya forma parte de main. Es la forma
// de saber, desde cualquier equipo, que una tarea ya fue integrada.
func (r *Repo) IsMerged(branch string) bool {
	repo, err := open(r.Local)
	if err != nil {
		return false
	}
	tip, err := branchTip(repo, branch)
	if err != nil {
		return false
	}
	mainTip, err := branchTip(repo, MainBranch)
	if err != nil {
		return false
	}
	bc, err1 := repo.CommitObject(tip)
	mc, err2 := repo.CommitObject(mainTip)
	if err1 != nil || err2 != nil {
		return false
	}
	ok, err := bc.IsAncestor(mc)
	return err == nil && ok
}

// branchTip prefiere la copia remota (la compartida) y cae a la local.
func branchTip(repo *git.Repository, branch string) (plumbing.Hash, error) {
	if ref, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", branch), true); err == nil {
		return ref.Hash(), nil
	}
	ref, err := repo.Reference(branchRef(branch), true)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	return ref.Hash(), nil
}

// CommitFilesOnBranch agrega archivos a una rama y la publica, sin tocar el
// directorio de trabajo: los DWG de la rama no se reescriben en disco. Se usa
// para las aprobaciones, que son archivos nuevos con nombre único: dos
// personas aprobando a la vez nunca pisan el mismo archivo, y si la
// publicación pierde la carrera se reconstruye sobre la punta actual.
func (r *Repo) CommitFilesOnBranch(branch string, files map[string][]byte, msg, actor string) (string, error) {
	repo, err := open(r.Local)
	if err != nil {
		return "", err
	}
	refspec := config.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch)

	for attempt := 0; attempt < 6; attempt++ {
		_ = r.Fetch()

		localRef, _ := repo.Reference(branchRef(branch), true)
		remoteRef, _ := repo.Reference(plumbing.NewRemoteReferenceName("origin", branch), true)
		base, err := pickBase(repo, localRef, remoteRef)
		if err != nil {
			return "", fmt.Errorf("rama %s: %w", branch, err)
		}

		baseCommit, err := repo.CommitObject(base)
		if err != nil {
			return "", err
		}
		baseTree, err := baseCommit.Tree()
		if err != nil {
			return "", err
		}
		newTree, err := putFiles(repo, baseTree, files)
		if err != nil {
			return "", err
		}

		sig := signatureFor(actor)
		c := &object.Commit{
			Author: *sig, Committer: *sig, Message: msg,
			TreeHash: newTree, ParentHashes: []plumbing.Hash{base},
		}
		obj := repo.Storer.NewEncodedObject()
		if err := c.Encode(obj); err != nil {
			return "", err
		}
		hash, err := repo.Storer.SetEncodedObject(obj)
		if err != nil {
			return "", err
		}
		if err := repo.Storer.SetReference(plumbing.NewHashReference(branchRef(branch), hash)); err != nil {
			return "", err
		}

		err = repo.Push(&git.PushOptions{RemoteName: "origin", RefSpecs: []config.RefSpec{refspec}})
		if err == nil || errors.Is(err, git.NoErrAlreadyUpToDate) {
			return hash.String(), nil
		}
		if errors.Is(err, git.ErrNonFastForwardUpdate) {
			time.Sleep(time.Duration(50*(attempt+1)) * time.Millisecond)
			continue // otro equipo publicó antes: rehacer sobre su punta
		}
		return "", err
	}
	return "", errors.New("la rama cambia demasiado rápido; reintentá en unos segundos")
}

// pickBase elige sobre qué commit construir: la punta más avanzada. Si el
// local tiene commits sin publicar, se publican junto con el nuevo; si
// divergieron, manda lo compartido.
func pickBase(repo *git.Repository, local, remote *plumbing.Reference) (plumbing.Hash, error) {
	switch {
	case local == nil && remote == nil:
		return plumbing.ZeroHash, errors.New("no existe")
	case local == nil:
		return remote.Hash(), nil
	case remote == nil || local.Hash() == remote.Hash():
		return local.Hash(), nil
	}
	lc, err1 := repo.CommitObject(local.Hash())
	rc, err2 := repo.CommitObject(remote.Hash())
	if err1 != nil || err2 != nil {
		return remote.Hash(), nil
	}
	if ahead, err := rc.IsAncestor(lc); err == nil && ahead {
		return local.Hash(), nil
	}
	return remote.Hash(), nil
}

// putFiles devuelve el árbol que resulta de agregar (o reemplazar) archivos en
// base, reutilizando todo lo demás sin leerlo.
func putFiles(repo *git.Repository, base *object.Tree, files map[string][]byte) (plumbing.Hash, error) {
	entries := map[string]object.TreeEntry{}
	if base != nil {
		for _, e := range base.Entries {
			entries[e.Name] = e
		}
	}
	nested := map[string]map[string][]byte{}
	for p, content := range files {
		p = path.Clean(strings.TrimPrefix(p, "/"))
		head, rest, isDir := strings.Cut(p, "/")
		if !isDir {
			obj := repo.Storer.NewEncodedObject()
			obj.SetType(plumbing.BlobObject)
			w, err := obj.Writer()
			if err != nil {
				return plumbing.ZeroHash, err
			}
			if _, err := w.Write(content); err != nil {
				return plumbing.ZeroHash, err
			}
			if err := w.Close(); err != nil {
				return plumbing.ZeroHash, err
			}
			h, err := repo.Storer.SetEncodedObject(obj)
			if err != nil {
				return plumbing.ZeroHash, err
			}
			entries[head] = object.TreeEntry{Name: head, Mode: filemode.Regular, Hash: h}
			continue
		}
		if nested[head] == nil {
			nested[head] = map[string][]byte{}
		}
		nested[head][rest] = content
	}
	for dir, sub := range nested {
		var child *object.Tree
		if e, ok := entries[dir]; ok && e.Mode == filemode.Dir {
			t, err := repo.TreeObject(e.Hash)
			if err != nil {
				return plumbing.ZeroHash, err
			}
			child = t
		}
		h, err := putFiles(repo, child, sub)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		entries[dir] = object.TreeEntry{Name: dir, Mode: filemode.Dir, Hash: h}
	}

	list := make([]object.TreeEntry, 0, len(entries))
	for _, e := range entries {
		list = append(list, e)
	}
	// git ordena las carpetas como si su nombre terminara en "/".
	key := func(e object.TreeEntry) string {
		if e.Mode == filemode.Dir {
			return e.Name + "/"
		}
		return e.Name
	}
	sort.Slice(list, func(i, j int) bool { return key(list[i]) < key(list[j]) })

	t := &object.Tree{Entries: list}
	obj := repo.Storer.NewEncodedObject()
	if err := t.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return repo.Storer.SetEncodedObject(obj)
}

// signatureFor arma la firma de un commit. Si actor es un correo se usa tal
// cual; si es un nombre suelto se le inventa un dominio local.
func signatureFor(actor string) *object.Signature {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "lcs"
	}
	if i := strings.Index(actor, "@"); i > 0 {
		return &object.Signature{Name: actor[:i], Email: actor, When: time.Now()}
	}
	return &object.Signature{Name: actor, Email: actor + "@lcs.local", When: time.Now()}
}
