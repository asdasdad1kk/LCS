package vcs

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Capa rápida de intercambio con la carpeta compartida.
//
// go-git habla el protocolo de git hasta con una ruta local: en cada push arma
// un packfile (con búsqueda de deltas sobre DWG de decenas de MB), lo comprime y
// el otro lado lo vuelve a desempaquetar. Sobre un NAS, además, cada archivo que
// toca es un viaje de red. Como el remoto es una carpeta, acá se copian
// directamente los objetos (archivos sueltos, comprimidos al nivel más rápido) y
// al final se mueve la referencia. Las escrituras tampoco pasan por el
// directorio de trabajo: los árboles y commits se arman a mano, así que no hay
// status/add/checkout que lean o reescriban los DWG.

// side es un repositorio con el directorio donde viven sus objetos.
type side struct {
	repo *git.Repository
	dir  string // directorio .git (clon) o el repositorio bare
}

func (s side) has(h plumbing.Hash) bool {
	return s.repo.Storer.HasEncodedObject(h) == nil
}

func loosePath(gitDir string, h plumbing.Hash) string {
	s := h.String()
	return filepath.Join(gitDir, "objects", s[:2], s[2:])
}

func localGitDir(local string) string { return filepath.Join(local, ".git") }

// writeLoose guarda un objeto como archivo suelto. Es idempotente y atómico
// (archivo temporal + rename), así que otro equipo nunca lee uno a medias.
func writeLoose(gitDir string, h plumbing.Hash, typ plumbing.ObjectType, size int64, src io.Reader) error {
	dst := loosePath(gitDir, h)
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "tmp_obj_")
	if err != nil {
		return err
	}
	name := tmp.Name()
	done := false
	defer func() {
		if !done {
			tmp.Close()
			os.Remove(name)
		}
	}()
	bw := bufio.NewWriterSize(tmp, 1<<20)
	zw, err := zlib.NewWriterLevel(bw, zlib.BestSpeed)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(zw, "%s %d\x00", typ.String(), size); err != nil {
		return err
	}
	if _, err := io.Copy(zw, src); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		if _, serr := os.Stat(dst); serr == nil { // otro equipo lo escribió primero
			os.Remove(name)
			done = true
			return nil
		}
		return err
	}
	done = true
	return nil
}

// copyRaw duplica un archivo suelto tal cual (ya viene comprimido).
func copyRaw(srcPath, dstPath string) error {
	if _, err := os.Stat(dstPath); err == nil {
		return nil
	}
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()
	dir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "tmp_obj_")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := io.Copy(tmp, bufio.NewReaderSize(in, 1<<20)); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, dstPath); err != nil {
		os.Remove(name)
		if _, serr := os.Stat(dstPath); serr == nil {
			return nil
		}
		return err
	}
	return nil
}

func copyObject(src, dst side, h plumbing.Hash) error {
	if dst.has(h) {
		return nil
	}
	if err := copyRaw(loosePath(src.dir, h), loosePath(dst.dir, h)); err == nil {
		return nil
	}
	// Empaquetado en el origen (p. ej. tras un gc): se reescribe suelto.
	obj, err := src.repo.Storer.EncodedObject(plumbing.AnyObject, h)
	if err != nil {
		return fmt.Errorf("objeto %s: %w", h, err)
	}
	rd, err := obj.Reader()
	if err != nil {
		return err
	}
	defer rd.Close()
	return writeLoose(dst.dir, h, obj.Type(), obj.Size(), rd)
}

// plan junta, sin copiar nada todavía, los objetos que le faltan a dst.
type plan struct {
	blobs   []plumbing.Hash // hojas: se copian en paralelo
	trees   []plumbing.Hash // de abajo hacia arriba
	commits []plumbing.Hash // del más viejo al más nuevo
	seen    map[plumbing.Hash]bool
}

func (p *plan) tree(src, dst side, h plumbing.Hash) error {
	if p.seen[h] || dst.has(h) { // un árbol presente en dst está completo (se escribe de abajo hacia arriba)
		return nil
	}
	p.seen[h] = true
	t, err := object.GetTree(src.repo.Storer, h)
	if err != nil {
		return err
	}
	for _, e := range t.Entries {
		switch e.Mode {
		case filemode.Dir:
			if err := p.tree(src, dst, e.Hash); err != nil {
				return err
			}
		case filemode.Submodule:
		default:
			if !p.seen[e.Hash] && !dst.has(e.Hash) {
				p.seen[e.Hash] = true
				p.blobs = append(p.blobs, e.Hash)
			}
		}
	}
	p.trees = append(p.trees, h)
	return nil
}

// transfer copia a dst todo lo que falta para que tip quede completo. Se
// detiene en los commits que dst ya tiene. Los blobs viajan en paralelo (cada
// archivo es una espera de red); árboles y commits después, en orden, para que
// nunca exista un árbol sin sus hijos.
func transfer(src, dst side, tip plumbing.Hash) error {
	defer slow("transfer")()
	defer slow("transfer", "objetos", "copiar")()
	if tip.IsZero() || dst.has(tip) {
		return nil
	}
	var order []*object.Commit
	seen := map[plumbing.Hash]bool{}
	stack := []plumbing.Hash{tip}
	for len(stack) > 0 {
		h := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[h] || dst.has(h) {
			continue
		}
		seen[h] = true
		c, err := object.GetCommit(src.repo.Storer, h)
		if err != nil {
			return fmt.Errorf("commit %s: %w", h, err)
		}
		order = append(order, c)
		stack = append(stack, c.ParentHashes...)
	}
	pl := &plan{seen: map[plumbing.Hash]bool{}}
	for i := len(order) - 1; i >= 0; i-- {
		if err := pl.tree(src, dst, order[i].TreeHash); err != nil {
			return err
		}
		pl.commits = append(pl.commits, order[i].Hash)
	}

	if err := parallelCopy(src, dst, pl.blobs); err != nil {
		return err
	}
	for _, h := range pl.trees {
		if err := copyObject(src, dst, h); err != nil {
			return err
		}
	}
	for _, h := range pl.commits {
		if err := copyObject(src, dst, h); err != nil {
			return err
		}
	}
	return nil
}

func parallelCopy(src, dst side, hashes []plumbing.Hash) error {
	if len(hashes) == 0 {
		return nil
	}
	if len(hashes) == 1 {
		return copyObject(src, dst, hashes[0])
	}
	const workers = 8
	jobs := make(chan plumbing.Hash)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	for i := 0; i < workers && i < len(hashes); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for h := range jobs {
				if err := copyObject(src, dst, h); err != nil {
					mu.Lock()
					if first == nil {
						first = err
					}
					mu.Unlock()
				}
			}
		}()
	}
	for _, h := range hashes {
		jobs <- h
	}
	close(jobs)
	wg.Wait()
	return first
}

func closeRepo(r *git.Repository) {
	if c, ok := r.Storer.(io.Closer); ok {
		_ = c.Close()
	}
}

// ---------------------------------------------------------------- fetch

var fetchedAt timeMap

// Fetch trae del repositorio compartido las ramas que cambiaron. Si nada se
// movió no copia nada (solo lee las referencias).
func (r *Repo) Fetch() error {
	return r.fetch(0)
}

// fetchFresh evita repetir el viaje a la red dentro de una misma operación
// encadenada (integrar → sincronizar → reconciliar).
func (r *Repo) fetchFresh(maxAge time.Duration) error {
	return r.fetch(maxAge)
}

func (r *Repo) fetch(maxAge time.Duration) error {
	if maxAge > 0 {
		if t, ok := fetchedAt.get(r.Local); ok && time.Since(t) < maxAge {
			return nil
		}
	}
	// Sobre un NAS cada archivo leído es un viaje de red: recorrer todas las
	// referencias cuesta decenas. Cada escritura de LCS cambia un archivo token
	// en el repositorio compartido; si sigue igual, nada se movió desde la última
	// vez y alcanza con esta única lectura. Cada minuto se hace igual el
	// recorrido completo, por si alguien escribió con git a mano.
	token := readToken(r.Bare)
	if token == "" {
		// Repositorio anterior al token: se crea para que desde ahora las
		// lecturas sin cambios cuesten un solo archivo.
		bumpToken(r.Bare)
		token = readToken(r.Bare)
	}
	if token != "" {
		if last, ok := tokens.get(r.Local); ok && last.token == token && time.Since(last.at) < time.Minute {
			return nil
		}
	}
	if err := r.fetchFull(); err != nil {
		return err
	}
	if token != "" {
		tokens.set(r.Local, tokenState{token: token, at: time.Now()})
	}
	return nil
}

func (r *Repo) fetchFull() error {
	defer slow("fetchFull")()
	bare, err := git.PlainOpen(r.Bare)
	if err != nil {
		return err
	}
	defer closeRepo(bare)
	local, err := open(r.Local)
	if err != nil {
		return err
	}
	defer closeRepo(local)

	remote := map[string]plumbing.Hash{}
	iter, err := bare.References()
	if err != nil {
		return err
	}
	_ = iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Name().IsBranch() && ref.Type() == plumbing.HashReference {
			remote[ref.Name().Short()] = ref.Hash()
		}
		return nil
	})
	iter.Close()

	from := side{repo: bare, dir: r.Bare}
	to := side{repo: local, dir: localGitDir(r.Local)}
	for name, h := range remote {
		tracking := plumbing.NewRemoteReferenceName("origin", name)
		if cur, err := local.Reference(tracking, false); err == nil && cur.Hash() == h {
			continue
		}
		if err := transfer(from, to, h); err != nil {
			return err
		}
		if err := local.Storer.SetReference(plumbing.NewHashReference(tracking, h)); err != nil {
			return err
		}
	}

	// Una rama descartada en otro equipo desaparece también de aquí.
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
	fetchedAt.set(r.Local, time.Now())
	return nil
}

// ---------------------------------------------------------------- publish

// publish deja disponible la punta de una rama en la carpeta compartida:
// primero los objetos, al final la referencia. Solo avanza (fast-forward); si
// otro equipo movió la rama antes, devuelve git.ErrNonFastForwardUpdate.
func (r *Repo) publish(local *git.Repository, branch string, tip plumbing.Hash) error {
	bare, err := git.PlainOpen(r.Bare)
	if err != nil {
		return err
	}
	defer closeRepo(bare)
	if err := transfer(side{local, localGitDir(r.Local)}, side{bare, r.Bare}, tip); err != nil {
		return err
	}
	name := branchRef(branch)
	var old *plumbing.Reference
	cur, err := bare.Reference(name, true)
	switch {
	case errors.Is(err, plumbing.ErrReferenceNotFound):
	case err != nil:
		return err
	default:
		if cur.Hash() == tip {
			return nil
		}
		ok := false
		if cc, err := local.CommitObject(cur.Hash()); err == nil {
			if tc, err := local.CommitObject(tip); err == nil {
				ok, _ = cc.IsAncestor(tc)
			}
		}
		if !ok {
			return git.ErrNonFastForwardUpdate
		}
		old = cur
	}
	if err := bare.Storer.CheckAndSetReference(plumbing.NewHashReference(name, tip), old); err != nil {
		return git.ErrNonFastForwardUpdate
	}
	bumpToken(r.Bare)
	_ = local.Storer.SetReference(plumbing.NewHashReference(plumbing.NewRemoteReferenceName("origin", branch), tip))
	fetchedAt.set(r.Local, time.Now())
	tokens.m.Delete(r.Local) // el próximo fetch relee: el token cambió por nuestra propia escritura
	return nil
}

// ---------------------------------------------------------------- objetos

type change struct {
	data   []byte
	blob   plumbing.Hash // si data es nil: un blob que ya existe (se reutiliza sin leerlo)
	remove bool
}

func putBlob(repo *git.Repository, gitDir string, content []byte) (plumbing.Hash, error) {
	h := plumbing.ComputeHash(plumbing.BlobObject, content)
	if repo.Storer.HasEncodedObject(h) == nil {
		return h, nil
	}
	return h, writeLoose(gitDir, h, plumbing.BlobObject, int64(len(content)), bytes.NewReader(content))
}

type encoder interface {
	Encode(plumbing.EncodedObject) error
}

func putEncoded(repo *git.Repository, gitDir string, e encoder) (plumbing.Hash, error) {
	obj := &plumbing.MemoryObject{}
	if err := e.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	h := obj.Hash()
	if repo.Storer.HasEncodedObject(h) == nil {
		return h, nil
	}
	rd, err := obj.Reader()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	defer rd.Close()
	return h, writeLoose(gitDir, h, obj.Type(), obj.Size(), rd)
}

// putChanges devuelve el árbol que resulta de aplicar los cambios sobre base,
// reutilizando todo lo demás sin leerlo. empty indica que la carpeta quedó vacía.
func putChanges(repo *git.Repository, gitDir string, base *object.Tree, changes map[string]change) (plumbing.Hash, bool, error) {
	entries := map[string]object.TreeEntry{}
	if base != nil {
		for _, e := range base.Entries {
			entries[e.Name] = e
		}
	}
	nested := map[string]map[string]change{}
	for p, c := range changes {
		p = path.Clean(strings.TrimPrefix(filepath.ToSlash(p), "/"))
		head, rest, isDir := strings.Cut(p, "/")
		if !isDir {
			switch {
			case c.remove:
				delete(entries, head)
			case c.data != nil || c.blob.IsZero():
				h, err := putBlob(repo, gitDir, c.data)
				if err != nil {
					return plumbing.ZeroHash, false, err
				}
				entries[head] = object.TreeEntry{Name: head, Mode: filemode.Regular, Hash: h}
			default:
				entries[head] = object.TreeEntry{Name: head, Mode: filemode.Regular, Hash: c.blob}
			}
			continue
		}
		if nested[head] == nil {
			nested[head] = map[string]change{}
		}
		nested[head][rest] = c
	}
	for dir, sub := range nested {
		var child *object.Tree
		if e, ok := entries[dir]; ok && e.Mode == filemode.Dir {
			t, err := object.GetTree(repo.Storer, e.Hash)
			if err != nil {
				return plumbing.ZeroHash, false, err
			}
			child = t
		}
		h, empty, err := putChanges(repo, gitDir, child, sub)
		if err != nil {
			return plumbing.ZeroHash, false, err
		}
		if empty {
			delete(entries, dir)
			continue
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
	if len(list) == 0 {
		return plumbing.ZeroHash, true, nil
	}
	h, err := putEncoded(repo, gitDir, &object.Tree{Entries: list})
	return h, false, err
}

// tip devuelve la punta más avanzada de una rama entre la copia local y la
// compartida.
func (r *Repo) tip(repo *git.Repository, branch string) (plumbing.Hash, error) {
	local, _ := repo.Reference(branchRef(branch), true)
	remote, _ := repo.Reference(plumbing.NewRemoteReferenceName("origin", branch), true)
	return pickBase(repo, local, remote)
}

// commitSpec describe un commit que mueve la rama target.
type commitSpec struct {
	Target string // rama que avanza
	From   string // rama cuya punta es el padre (por defecto, Target)
	Msg    string
	Actor  string
	// Build recibe la punta sobre la que se construye (puede cambiar entre
	// intentos) y devuelve los cambios y padres adicionales (un merge).
	Build func(base plumbing.Hash) (map[string]change, []plumbing.Hash, error)
}

var errRetry = errors.New("reintentar")

// commit arma el commit sin tocar el directorio de trabajo, lo publica y
// avanza la rama local. Si otro equipo publicó antes, rehace sobre su punta.
func (r *Repo) commit(spec commitSpec) (plumbing.Hash, error) {
	repo, err := open(r.Local)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	defer closeRepo(repo)
	gitDir := localGitDir(r.Local)
	from := spec.From
	if from == "" {
		from = spec.Target
	}

	for attempt := 0; attempt < 6; attempt++ {
		t0 := time.Now()
		if err := r.Fetch(); err != nil {
			return plumbing.ZeroHash, err
		}
		tFetch := time.Since(t0)
		base, err := r.tip(repo, from)
		if err != nil {
			return plumbing.ZeroHash, fmt.Errorf("rama %s: %w", from, err)
		}
		changes, extra, err := spec.Build(base)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		baseCommit, err := repo.CommitObject(base)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		baseTree, err := baseCommit.Tree()
		if err != nil {
			return plumbing.ZeroHash, err
		}
		tree, empty, err := putChanges(repo, gitDir, baseTree, changes)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		if empty {
			return plumbing.ZeroHash, errors.New("el commit dejaría el repositorio vacío")
		}
		sig := signatureFor(spec.Actor)
		c := &object.Commit{
			Author: *sig, Committer: *sig, Message: spec.Msg,
			TreeHash: tree, ParentHashes: append([]plumbing.Hash{base}, extra...),
		}
		hash, err := putEncoded(repo, gitDir, c)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		tBuild := time.Since(t0) - tFetch
		err = r.publish(repo, spec.Target, hash)
		if total := time.Since(t0); total > 300*time.Millisecond {
			slog.Info("commit lento", "rama", spec.Target, "fetch", tFetch.Round(time.Millisecond).String(), "armar", tBuild.Round(time.Millisecond).String(), "publicar", (total - tFetch - tBuild).Round(time.Millisecond).String())
		}
		if errors.Is(err, git.ErrNonFastForwardUpdate) {
			time.Sleep(time.Duration(50*(attempt+1)) * time.Millisecond)
			continue // otro equipo publicó antes: rehacer sobre su punta
		}
		if err != nil {
			return plumbing.ZeroHash, err
		}
		if err := repo.Storer.SetReference(plumbing.NewHashReference(branchRef(spec.Target), hash)); err != nil {
			return plumbing.ZeroHash, err
		}
		return hash, nil
	}
	return plumbing.ZeroHash, errors.New("la rama cambia demasiado rápido; reintentá en unos segundos")
}

// FileInfoAt devuelve el hash del blob y su tamaño sin leer el contenido.
func (r *Repo) FileInfoAt(revision, p string) (string, int64, error) {
	defer slow("FileInfoAt")()
	repo, err := open(r.Local)
	if err != nil {
		return "", 0, err
	}
	defer closeRepo(repo)
	h, err := resolveRevision(repo, revision)
	if err != nil {
		return "", 0, fmt.Errorf("revisión %s: %w", revision, err)
	}
	tree, err := treeOf(repo, *h)
	if err != nil {
		return "", 0, err
	}
	e, err := tree.FindEntry(p)
	if err != nil || !e.Mode.IsFile() {
		return "", 0, os.ErrNotExist
	}
	return e.Hash.String(), r.blobSize(repo, e.Hash), nil
}

const tokenFile = "lcs-rev"

type tokenState struct {
	token string
	at    time.Time
}

type tokenMap struct{ m sync.Map }

func (t *tokenMap) get(k string) (tokenState, bool) {
	v, ok := t.m.Load(k)
	if !ok {
		return tokenState{}, false
	}
	return v.(tokenState), true
}

func (t *tokenMap) set(k string, v tokenState) { t.m.Store(k, v) }

var tokens tokenMap

func readToken(bare string) string {
	b, err := os.ReadFile(filepath.Join(bare, tokenFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// bumpToken avisa a los demás equipos que el repositorio cambió. Se escribe
// DESPUÉS de mover la referencia: quien lea el token nuevo ya ve la referencia.
func bumpToken(bare string) {
	var rnd [8]byte
	_, _ = rand.Read(rnd[:])
	val := fmt.Sprintf("%d-%x", time.Now().UnixNano(), rnd)
	tmp := filepath.Join(bare, tokenFile+".tmp-"+fmt.Sprintf("%x", rnd[:4]))
	if err := os.WriteFile(tmp, []byte(val), 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, filepath.Join(bare, tokenFile)); err != nil {
		_ = os.Remove(tmp)
		_ = os.WriteFile(filepath.Join(bare, tokenFile), []byte(val), 0o644)
	}
}

// timeMap guarda cuándo se trajo por última vez cada clon.
type timeMap struct{ m sync.Map }

func (t *timeMap) get(k string) (time.Time, bool) {
	v, ok := t.m.Load(k)
	if !ok {
		return time.Time{}, false
	}
	return v.(time.Time), true
}

func (t *timeMap) set(k string, v time.Time) { t.m.Store(k, v) }

// blobSize lee el tamaño de un blob sin leer su contenido: la cabecera del
// archivo suelto o, si está empaquetado, el tamaño que registra el paquete.
// BlobObject de go-git descomprime el plano entero (un DWG de 150 MB) solo para
// decir cuánto pesa. El resultado se recuerda: un blob nunca cambia.
func (r *Repo) blobSize(repo *git.Repository, h plumbing.Hash) int64 {
	if v, ok := sizeCache.Load(h); ok {
		return v.(int64)
	}
	size := int64(-1)
	if f, err := os.Open(loosePath(localGitDir(r.Local), h)); err == nil {
		if zr, err := zlib.NewReader(f); err == nil {
			hdr := make([]byte, 0, 32)
			one := make([]byte, 1)
			for len(hdr) < 32 {
				if _, err := io.ReadFull(zr, one); err != nil {
					break
				}
				if one[0] == 0 {
					if i := bytes.IndexByte(hdr, ' '); i > 0 {
						var n int64
						if _, err := fmt.Sscanf(string(hdr[i+1:]), "%d", &n); err == nil {
							size = n
						}
					}
					break
				}
				hdr = append(hdr, one[0])
			}
			zr.Close()
		}
		f.Close()
	}
	if size < 0 {
		if n, err := repo.Storer.EncodedObjectSize(h); err == nil {
			size = n
		}
	}
	if size < 0 {
		return 0
	}
	sizeCache.Store(h, size)
	return size
}

// RemoteToken es el token de cambios del repositorio compartido ("" si no existe).
func (r *Repo) RemoteToken() string { return readToken(r.Bare) }

// RefsSignature resume en qué commit está cada rama local (y main). Si no
// cambió, tampoco cambió nada de lo que LCS indexa.
func (r *Repo) RefsSignature() string {
	repo, err := open(r.Local)
	if err != nil {
		return ""
	}
	iter, err := repo.References()
	if err != nil {
		return ""
	}
	defer iter.Close()
	var parts []string
	_ = iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Name().IsBranch() && ref.Type() == plumbing.HashReference {
			parts = append(parts, ref.Name().Short()+"="+ref.Hash().String())
		}
		return nil
	})
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

var sizeCache sync.Map // hash del blob → tamaño (un blob nunca cambia)

// slow registra en el log lo que tarda más de medio segundo, con el detalle de
// qué paso fue. Sirve para ubicar en la próxima revisión dónde se va el tiempo.
func slow(op string, kv ...any) func() {
	start := time.Now()
	return func() {
		if d := time.Since(start); d > 500*time.Millisecond {
			slog.Info("git lento", append([]any{"op", op, "dur", d.Round(time.Millisecond).String()}, kv...)...)
		}
	}
}
