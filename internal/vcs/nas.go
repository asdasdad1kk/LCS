package vcs

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// Todo el acceso al repositorio compartido (el NAS) pasa por nasFS. Sobre un
// recurso de red cada operación sobre un archivo es un viaje de ida y vuelta
// de decenas o cientos de milisegundos: lo que importa es cuántas se hacen, no
// cuántos bytes. Tener una sola puerta permite contarlas (y simular la
// latencia) en las pruebas.
type nasFS interface {
	ReadFile(string) ([]byte, error)
	WriteFile(string, []byte, os.FileMode) error
	Rename(oldPath, newPath string) error
	Remove(string) error
	Stat(string) (os.FileInfo, error)
	MkdirAll(string, os.FileMode) error
	OpenFile(string, int, os.FileMode) (*os.File, error)
	CreateTemp(dir, pattern string) (*os.File, error)
}

type osFS struct{}

func (osFS) ReadFile(p string) ([]byte, error)                         { return os.ReadFile(p) }
func (osFS) WriteFile(p string, b []byte, m os.FileMode) error         { return os.WriteFile(p, b, m) }
func (osFS) Rename(a, b string) error                                  { return os.Rename(a, b) }
func (osFS) Remove(p string) error                                     { return os.Remove(p) }
func (osFS) Stat(p string) (os.FileInfo, error)                        { return os.Stat(p) }
func (osFS) MkdirAll(p string, m os.FileMode) error                    { return os.MkdirAll(p, m) }
func (osFS) OpenFile(p string, f int, m os.FileMode) (*os.File, error) { return os.OpenFile(p, f, m) }
func (osFS) CreateTemp(d, pat string) (*os.File, error)                { return os.CreateTemp(d, pat) }

var nas nasFS = osFS{}

// Contadores para las pruebas: operaciones contra el NAS y aperturas de go-git
// sobre el repositorio compartido (cada una cuesta decenas de operaciones).
var (
	nasOps    atomic.Int64
	bareOpens atomic.Int64
)

type countFS struct {
	osFS
	delay time.Duration
}

func (c countFS) tick() {
	nasOps.Add(1)
	if c.delay > 0 {
		time.Sleep(c.delay)
	}
}
func (c countFS) ReadFile(p string) ([]byte, error) { c.tick(); return c.osFS.ReadFile(p) }
func (c countFS) WriteFile(p string, b []byte, m os.FileMode) error {
	c.tick()
	return c.osFS.WriteFile(p, b, m)
}
func (c countFS) Rename(a, b string) error               { c.tick(); return c.osFS.Rename(a, b) }
func (c countFS) Remove(p string) error                  { c.tick(); return c.osFS.Remove(p) }
func (c countFS) Stat(p string) (os.FileInfo, error)     { c.tick(); return c.osFS.Stat(p) }
func (c countFS) MkdirAll(p string, m os.FileMode) error { c.tick(); return c.osFS.MkdirAll(p, m) }
func (c countFS) OpenFile(p string, f int, m os.FileMode) (*os.File, error) {
	c.tick()
	return c.osFS.OpenFile(p, f, m)
}
func (c countFS) CreateTemp(d, pat string) (*os.File, error) {
	c.tick()
	return c.osFS.CreateTemp(d, pat)
}

// InstrumentNAS cuenta las operaciones contra el repositorio compartido y les
// agrega una latencia simulada. Devuelve cómo leer el contador y cómo
// restaurar el comportamiento normal. Solo para pruebas.
func InstrumentNAS(delay time.Duration) (ops func() (fileOps, bareOpens int64), restore func()) {
	prev := nas
	nas = countFS{delay: delay}
	nasOps.Store(0)
	bareOpens.Store(0)
	return func() (int64, int64) { return nasOps.Load(), bareOpens.Load() }, func() { nas = prev }
}

func openBare(path string) (*git.Repository, error) {
	bareOpens.Add(1)
	return git.PlainOpen(path)
}

// ---------------------------------------------------------------- estado

// El repositorio compartido guarda un archivo lcs-state con la revisión y el
// commit de cada rama. Leerlo cuesta una operación; recorrer las referencias
// una por una (un archivo cada una) cuesta decenas. Es solo una pista para
// leer rápido: las escrituras verifican la referencia real, y cada equipo hace
// de vez en cuando el recorrido completo que corrige cualquier hueco.
const stateFile = "lcs-state"

type repoState struct {
	Rev  string            `json:"rev"`
	Refs map[string]string `json:"refs"`
	// Gone: ramas descartadas (nombre → cuándo), para que los demás equipos las
	// retiren sin recorrer todo. Se olvidan al día.
	Gone map[string]int64 `json:"gone,omitempty"`
}

func newRev() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%d-%x", time.Now().UnixNano(), b)
}

func readState(bare string) (*repoState, error) {
	raw, err := nas.ReadFile(filepath.Join(bare, stateFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st repoState
	if json.Unmarshal(raw, &st) != nil || st.Rev == "" {
		return nil, nil // ilegible: se reconstruye
	}
	if st.Refs == nil {
		st.Refs = map[string]string{}
	}
	return &st, nil
}

func writeState(bare string, st *repoState) error {
	raw, _ := json.Marshal(st)
	final := filepath.Join(bare, stateFile)
	tmp := final + ".tmp-" + newRev()[:8]
	if err := nas.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	var err error
	for i := 0; i < 4; i++ { // otro equipo puede tenerlo abierto un instante
		if err = nas.Rename(tmp, final); err == nil {
			return nil
		}
		time.Sleep(time.Duration(30*(i+1)) * time.Millisecond)
	}
	_ = nas.Remove(tmp)
	return err
}

// touchState anota un cambio de ramas: sube la revisión para que los demás
// equipos sepan que hay algo nuevo.
func touchState(bare string, mutate func(refs map[string]string)) *repoState {
	st, _ := readState(bare)
	if st == nil {
		st = &repoState{Refs: map[string]string{}}
		if refs, err := fullRefs(bare); err == nil {
			for n, h := range refs {
				st.Refs[n] = h.String()
			}
		}
	}
	mutate(st.Refs)
	for n, at := range st.Gone {
		if time.Since(time.UnixMilli(at)) > 24*time.Hour {
			delete(st.Gone, n)
		}
	}
	st.Rev = newRev()
	if err := writeState(bare, st); err != nil {
		return nil
	}
	return st
}

// fullRefs recorre todas las referencias del repositorio compartido.
func fullRefs(bare string) (map[string]plumbing.Hash, error) {
	repo, err := openBare(bare)
	if err != nil {
		return nil, err
	}
	defer closeRepo(repo)
	iter, err := repo.References()
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	out := map[string]plumbing.Hash{}
	_ = iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Name().IsBranch() && ref.Type() == plumbing.HashReference {
			out[ref.Name().Short()] = ref.Hash()
		}
		return nil
	})
	return out, nil
}

// ---------------------------------------------------------------- referencias

func refPath(bare, branch string) string {
	return filepath.Join(bare, "refs", "heads", filepath.FromSlash(branch))
}

func parseHash(s string) (plumbing.Hash, bool) {
	s = strings.TrimSpace(s)
	if len(s) != 40 {
		return plumbing.ZeroHash, false
	}
	h := plumbing.NewHash(s)
	return h, !h.IsZero()
}

// bareRef lee una sola rama del repositorio compartido: un archivo (o dos si
// está empaquetada), sin abrir el repositorio entero.
func bareRef(bare, branch string) (plumbing.Hash, bool, error) {
	raw, err := nas.ReadFile(refPath(bare, branch))
	if err == nil {
		if h, ok := parseHash(string(raw)); ok {
			return h, true, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return plumbing.ZeroHash, false, err
	}
	packed, err := nas.ReadFile(filepath.Join(bare, "packed-refs"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return plumbing.ZeroHash, false, nil
		}
		return plumbing.ZeroHash, false, err
	}
	want := "refs/heads/" + branch
	for _, line := range strings.Split(string(packed), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == want {
			if h, ok := parseHash(f[0]); ok {
				return h, true, nil
			}
		}
	}
	return plumbing.ZeroHash, false, nil
}

var errRefMoved = git.ErrNonFastForwardUpdate

// casRef mueve la rama de old a new solo si sigue en old (old cero: no debe
// existir). Usa el mismo protocolo de bloqueo que git (archivo .lock que se
// renombra sobre la referencia), así convive con quien use git directamente.
func casRef(bare, branch string, old, newHash plumbing.Hash) error {
	path := refPath(bare, branch)
	lock := path + ".lock"
	if err := nas.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var f *os.File
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		f, err = nas.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		// Un bloqueo viejo (un equipo se cayó a mitad) se retira.
		if fi, serr := nas.Stat(lock); serr == nil && time.Since(fi.ModTime()) > 30*time.Second {
			_ = nas.Remove(lock)
			continue
		}
		return errRefMoved // otro equipo está escribiendo esta rama: se reintenta sobre su resultado
	}
	if f == nil {
		return errRefMoved
	}
	committed := false
	defer func() {
		if !committed {
			f.Close()
			_ = nas.Remove(lock)
		}
	}()
	cur, ok, err := bareRef(bare, branch)
	if err != nil {
		return err
	}
	if (ok && cur != old) || (!ok && !old.IsZero()) {
		return errRefMoved
	}
	if _, err := io.WriteString(f, newHash.String()+"\n"); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	var rerr error
	for i := 0; i < 4; i++ {
		if rerr = nas.Rename(lock, path); rerr == nil {
			committed = true
			return nil
		}
		time.Sleep(time.Duration(30*(i+1)) * time.Millisecond)
	}
	return rerr
}

// ---------------------------------------------------------------- traer

type stateSeen struct {
	rev    string
	at     time.Time
	scanAt time.Time
}

type stateMap struct{ m sync.Map }

func (t *stateMap) get(k string) (stateSeen, bool) {
	v, ok := t.m.Load(k)
	if !ok {
		return stateSeen{}, false
	}
	return v.(stateSeen), true
}
func (t *stateMap) set(k string, v stateSeen) { t.m.Store(k, v) }

var seenStates stateMap

// scanEvery: cada cuánto cada equipo recorre todas las referencias (en segundo
// plano) para corregir lo que el archivo de estado pudiera haber perdido.
const scanEvery = 10 * time.Minute

var scanning sync.Map

// Fetch trae del repositorio compartido lo que cambió. Con el estado al día
// cuesta una operación (leer lcs-state).
func (r *Repo) Fetch() error {
	defer slow("Fetch")()
	st, err := readState(r.Bare)
	if err != nil {
		return err
	}
	if st == nil {
		// Repositorio anterior al archivo de estado: se arma una vez.
		st = touchState(r.Bare, func(map[string]string) {})
		if st == nil {
			return r.fullScan()
		}
	}
	seen, known := seenStates.get(r.Local)
	if known && seen.rev == st.Rev {
		r.maybeScan(seen)
		return nil
	}
	if err := r.applyRefs(st.Refs); err != nil {
		return err
	}
	r.dropGone(st)
	if !known {
		// Primera vez en este arranque: el recorrido completo (que además retira
		// las ramas descartadas) corre en segundo plano a los 2 minutos, para no
		// cargar el NAS justo al abrir con todos los proyectos a la vez.
		seen.scanAt = time.Now().Add(-(scanEvery - 2*time.Minute))
	}
	seen.rev, seen.at = st.Rev, time.Now()
	seenStates.set(r.Local, seen)
	r.maybeScan(seen)
	return nil
}

func (r *Repo) maybeScan(seen stateSeen) {
	if time.Since(seen.scanAt) < scanEvery {
		return
	}
	if _, running := scanning.LoadOrStore(r.Local, true); running {
		return
	}
	// Se marca de inmediato para no relanzarlo mientras corre.
	seen.scanAt = time.Now()
	seenStates.set(r.Local, seen)
	go func() {
		defer scanning.Delete(r.Local)
		_ = r.fullScan()
	}()
}

// applyRefs copia lo que falta de las ramas que dice el estado. Nunca borra:
// retirar ramas descartadas es cosa del recorrido completo.
func (r *Repo) applyRefs(refs map[string]string) error {
	local, err := open(r.Local)
	if err != nil {
		return err
	}
	defer closeRepo(local)
	var bare *git.Repository
	defer func() {
		if bare != nil {
			closeRepo(bare)
		}
	}()
	for name, hs := range refs {
		h, ok := parseHash(hs)
		if !ok {
			continue
		}
		tracking := plumbing.NewRemoteReferenceName("origin", name)
		if cur, err := local.Reference(tracking, false); err == nil && cur.Hash() == h {
			continue
		}
		if bare == nil {
			if bare, err = openBare(r.Bare); err != nil {
				return err
			}
		}
		if err := transfer(side{bare, r.Bare, nas}, side{local, localGitDir(r.Local), osFS{}}, h); err != nil {
			return err
		}
		if err := local.Storer.SetReference(plumbing.NewHashReference(tracking, h)); err != nil {
			return err
		}
	}
	return nil
}

// RefreshBranches trae solo estas ramas: una lectura de archivo por rama. Es lo
// que usan aprobar, integrar y subir: no necesitan saber de las demás.
func (r *Repo) RefreshBranches(names ...string) error {
	defer slow("RefreshBranches")()
	local, err := open(r.Local)
	if err != nil {
		return err
	}
	defer closeRepo(local)
	var bare *git.Repository
	defer func() {
		if bare != nil {
			closeRepo(bare)
		}
	}()
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		h, ok, err := bareRef(r.Bare, name)
		if err != nil {
			return err
		}
		tracking := plumbing.NewRemoteReferenceName("origin", name)
		if !ok {
			_ = local.Storer.RemoveReference(tracking)
			continue
		}
		if cur, err := local.Reference(tracking, false); err == nil && cur.Hash() == h {
			continue
		}
		if bare == nil {
			if bare, err = openBare(r.Bare); err != nil {
				return err
			}
		}
		if err := transfer(side{bare, r.Bare, nas}, side{local, localGitDir(r.Local), osFS{}}, h); err != nil {
			return err
		}
		if err := local.Storer.SetReference(plumbing.NewHashReference(tracking, h)); err != nil {
			return err
		}
	}
	return nil
}

// SyncSome es RefreshBranches más poner al día la copia local de esas ramas.
func (r *Repo) SyncSome(names ...string) error {
	if err := r.RefreshBranches(names...); err != nil {
		return err
	}
	local, err := open(r.Local)
	if err != nil {
		return err
	}
	defer closeRepo(local)
	for _, name := range names {
		tracking, err := local.Reference(plumbing.NewRemoteReferenceName("origin", name), true)
		if err != nil {
			continue
		}
		localRef := branchRef(name)
		cur, err := local.Reference(localRef, false)
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			_ = local.Storer.SetReference(plumbing.NewHashReference(localRef, tracking.Hash()))
			continue
		}
		if err != nil || cur.Hash() == tracking.Hash() {
			continue
		}
		lc, e1 := local.CommitObject(cur.Hash())
		tc, e2 := local.CommitObject(tracking.Hash())
		if e1 != nil || e2 != nil {
			continue
		}
		if ok, _ := lc.IsAncestor(tc); ok {
			_ = local.Storer.SetReference(plumbing.NewHashReference(localRef, tracking.Hash()))
		}
	}
	return nil
}

// fullScan recorre todas las referencias del repositorio compartido, trae lo
// que falte y retira las ramas que ya no existen. Cuesta un archivo por rama.
func (r *Repo) fullScan() error {
	defer slow("fullScan")()
	remote, err := fullRefs(r.Bare)
	if err != nil {
		return err
	}
	strs := make(map[string]string, len(remote))
	for n, h := range remote {
		strs[n] = h.String()
	}
	if err := r.applyRefs(strs); err != nil {
		return err
	}
	local, err := open(r.Local)
	if err != nil {
		return err
	}
	defer closeRepo(local)
	stale := []plumbing.ReferenceName{}
	if refs, err := local.References(); err == nil {
		_ = refs.ForEach(func(ref *plumbing.Reference) error {
			n := ref.Name()
			if !n.IsRemote() {
				return nil
			}
			short := strings.TrimPrefix(string(n), "refs/remotes/origin/")
			if short == string(n) || short == "HEAD" || short == "" {
				return nil
			}
			if _, ok := remote[short]; !ok {
				stale = append(stale, n)
			}
			return nil
		})
		refs.Close()
	}
	for _, n := range stale {
		_ = local.Storer.RemoveReference(n)
	}
	return nil
}

// RemoteToken es la revisión del repositorio compartido ("" si no hay estado).
func (r *Repo) RemoteToken() string {
	st, _ := readState(r.Bare)
	if st == nil {
		return ""
	}
	return st.Rev
}

// dropGone retira de este clon las ramas que otro equipo descartó.
func (r *Repo) dropGone(st *repoState) {
	if len(st.Gone) == 0 {
		return
	}
	local, err := open(r.Local)
	if err != nil {
		return
	}
	defer closeRepo(local)
	for name := range st.Gone {
		if _, alive := st.Refs[name]; alive {
			continue
		}
		_ = local.Storer.RemoveReference(plumbing.NewRemoteReferenceName("origin", name))
	}
}

// markGone anota una rama descartada en el estado compartido.
func markGone(bare, name string) {
	st, _ := readState(bare)
	if st == nil {
		return
	}
	delete(st.Refs, name)
	if st.Gone == nil {
		st.Gone = map[string]int64{}
	}
	st.Gone[name] = time.Now().UnixMilli()
	st.Rev = newRev()
	_ = writeState(bare, st)
}
