package vcs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
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
// compartida. Si el clon ya está, no hace nada. No hay directorio de trabajo:
// los planos se leen de la base de objetos.
func EnsureClone(bare, local string) error {
	if _, err := git.PlainOpen(local); err == nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(bare, "HEAD")); err != nil {
		return fmt.Errorf("no existe el repositorio compartido %s", bare)
	}
	fail := func(err error) error {
		_ = os.RemoveAll(local) // no dejar un clon a medias
		return fmt.Errorf("clonando %s: %w", bare, err)
	}
	r, err := git.PlainInitWithOptions(local, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: plumbing.Main},
	})
	if err != nil {
		return fail(err)
	}
	if _, err := r.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{bare}}); err != nil {
		return fail(err)
	}
	rp := &Repo{Bare: bare, Local: local}
	if err := rp.SyncBranches(); err != nil {
		return fail(err)
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

// DrawingsTreeHash identifica el contenido exacto de drawings/ en una
// revisión. Una aprobación se ata a este valor: cualquier cambio en un plano
// la deja obsoleta, y resubir los mismos bytes no.
func (r *Repo) DrawingsTreeHash(revision string) (string, error) {
	defer slow("DrawingsTreeHash")()
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
	return r.ListFilesWhere(revision, dir, nil)
}

// ListFilesWhere es ListFilesAt con un filtro por nombre: evita leer archivos
// que no interesan (p. ej. las imágenes al buscar solo los JSON).
func (r *Repo) ListFilesWhere(revision, dir string, keep func(name string) bool) ([]NamedFile, error) {
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
		if !e.Mode.IsFile() || (keep != nil && !keep(e.Name)) {
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
	defer slow("IsMerged")()
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
