package vcs

import (
	"bufio"
	"bytes"
	"compress/zlib"
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
	fs   nasFS  // cómo se toca el disco de ese lado (el NAS se cuenta y se simula)
}

func (s side) has(h plumbing.Hash) bool {
	return s.repo.Storer.HasEncodedObject(h) == nil
}

func loosePath(gitDir string, h plumbing.Hash) string {
	s := h.String()
	return filepath.Join(gitDir, "objects", s[:2], s[2:])
}

func localGitDir(local string) string { return filepath.Join(local, ".git") }

// writeLoose guarda un objeto como archivo suelto. Es atómico (archivo
// temporal + rename), así que otro equipo nunca lee uno a medias. Con blind no
// pregunta si ya existe: en el NAS cada pregunta es un viaje de red, y escribir
// dos veces el mismo objeto es inofensivo.
func writeLoose(fsys nasFS, gitDir string, h plumbing.Hash, typ plumbing.ObjectType, size int64, src io.Reader, blind bool) error {
	dst := loosePath(gitDir, h)
	if !blind {
		if _, err := fsys.Stat(dst); err == nil {
			return nil
		}
	}
	dir := filepath.Dir(dst)
	tmp, err := fsys.CreateTemp(dir, "tmp_obj_")
	if errors.Is(err, os.ErrNotExist) {
		if err = fsys.MkdirAll(dir, 0o755); err == nil {
			tmp, err = fsys.CreateTemp(dir, "tmp_obj_")
		}
	}
	if err != nil {
		return err
	}
	name := tmp.Name()
	done := false
	defer func() {
		if !done {
			tmp.Close()
			_ = fsys.Remove(name)
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
	if err := renameRetry(fsys, name, dst); err != nil {
		if _, serr := fsys.Stat(dst); serr == nil { // otro equipo lo escribió primero
			_ = fsys.Remove(name)
			done = true
			return nil
		}
		return err
	}
	done = true
	return nil
}

func renameRetry(fsys nasFS, from, to string) error {
	var err error
	for i := 0; i < 3; i++ {
		if err = fsys.Rename(from, to); err == nil {
			return nil
		}
		if _, serr := fsys.Stat(to); serr == nil {
			return err // ya existe: quien llama decide
		}
		time.Sleep(time.Duration(25*(i+1)) * time.Millisecond)
	}
	return err
}

// copyRaw duplica un archivo suelto tal cual (ya viene comprimido).
func copyRaw(srcPath string, dstFS nasFS, dstPath string, blind bool) error {
	if !blind {
		if _, err := dstFS.Stat(dstPath); err == nil {
			return nil
		}
	}
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()
	dir := filepath.Dir(dstPath)
	tmp, err := dstFS.CreateTemp(dir, "tmp_obj_")
	if errors.Is(err, os.ErrNotExist) {
		if err = dstFS.MkdirAll(dir, 0o755); err == nil {
			tmp, err = dstFS.CreateTemp(dir, "tmp_obj_")
		}
	}
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := io.Copy(tmp, bufio.NewReaderSize(in, 1<<20)); err != nil {
		tmp.Close()
		_ = dstFS.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = dstFS.Remove(name)
		return err
	}
	if err := renameRetry(dstFS, name, dstPath); err != nil {
		_ = dstFS.Remove(name)
		if _, serr := dstFS.Stat(dstPath); serr == nil {
			return nil
		}
		return err
	}
	return nil
}

func copyObject(src, dst side, h plumbing.Hash) error { return copyObjectMode(src, dst, h, false) }

func copyObjectMode(src, dst side, h plumbing.Hash, blind bool) error {
	if !blind && dst.has(h) {
		return nil
	}
	if src.fs == nil || isOS(src.fs) {
		if err := copyRaw(loosePath(src.dir, h), dst.fs, loosePath(dst.dir, h), blind); err == nil {
			return nil
		}
	} else if raw, err := src.fs.ReadFile(loosePath(src.dir, h)); err == nil {
		// Origen en el NAS: el objeto suelto se lee y se guarda tal cual.
		return writeRawBytes(dst, h, raw, blind)
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
	return writeLoose(dst.fs, dst.dir, h, obj.Type(), obj.Size(), rd, blind)
}

func isOS(f nasFS) bool { _, ok := f.(osFS); return ok }

func writeRawBytes(dst side, h plumbing.Hash, raw []byte, blind bool) error {
	path := loosePath(dst.dir, h)
	if !blind {
		if _, err := dst.fs.Stat(path); err == nil {
			return nil
		}
	}
	dir := filepath.Dir(path)
	tmp, err := dst.fs.CreateTemp(dir, "tmp_obj_")
	if errors.Is(err, os.ErrNotExist) {
		if err = dst.fs.MkdirAll(dir, 0o755); err == nil {
			tmp, err = dst.fs.CreateTemp(dir, "tmp_obj_")
		}
	}
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		_ = dst.fs.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = dst.fs.Remove(name)
		return err
	}
	if err := renameRetry(dst.fs, name, path); err != nil {
		_ = dst.fs.Remove(name)
		if _, serr := dst.fs.Stat(path); serr == nil {
			return nil
		}
		return err
	}
	return nil
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
	return parallelCopyMode(src, dst, hashes, false)
}

func parallelCopyMode(src, dst side, hashes []plumbing.Hash, blind bool) error {
	if len(hashes) == 0 {
		return nil
	}
	if len(hashes) == 1 {
		return copyObjectMode(src, dst, hashes[0], blind)
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
				if err := copyObjectMode(src, dst, h, blind); err != nil {
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

// ---------------------------------------------------------------- publish

// publish deja disponible la punta de una rama en la carpeta compartida
// recorriendo el historial para copiar lo que falte (camino lento: se usa solo
// cuando hay commits locales sin publicar). Solo avanza (fast-forward).
func (r *Repo) publish(local *git.Repository, branch string, tip plumbing.Hash) error {
	defer slow("publish(recorrido)", "rama", branch)()
	bare, err := openBare(r.Bare)
	if err != nil {
		return err
	}
	defer closeRepo(bare)
	if err := transfer(side{local, localGitDir(r.Local), osFS{}}, side{bare, r.Bare, nas}, tip); err != nil {
		return err
	}
	cur, ok, err := bareRef(r.Bare, branch)
	if err != nil {
		return err
	}
	var old plumbing.Hash
	if ok {
		if cur == tip {
			return nil
		}
		okAnc := false
		if cc, err := local.CommitObject(cur); err == nil {
			if tc, err := local.CommitObject(tip); err == nil {
				okAnc, _ = cc.IsAncestor(tc)
			}
		}
		if !okAnc {
			return git.ErrNonFastForwardUpdate
		}
		old = cur
	}
	return r.finishPublish(local, branch, old, tip)
}

// finishPublish mueve la referencia y avisa a los demás equipos.
func (r *Repo) finishPublish(local *git.Repository, branch string, old, tip plumbing.Hash) error {
	if err := casRef(r.Bare, branch, old, tip); err != nil {
		return err
	}
	_ = local.Storer.SetReference(plumbing.NewHashReference(plumbing.NewRemoteReferenceName("origin", branch), tip))
	if st := touchState(r.Bare, func(refs map[string]string) { refs[branch] = tip.String() }); st != nil {
		seen, _ := seenStates.get(r.Local)
		seen.rev, seen.at = st.Rev, time.Now()
		seenStates.set(r.Local, seen)
	}
	return nil
}

// publishFast publica un commit recién armado copiando SOLO los objetos que
// se crearon para él (en vez de recorrer el historial preguntando al NAS qué
// falta). Supone que los padres ya están en el repositorio compartido.
func (r *Repo) publishFast(local *git.Repository, branch string, tip plumbing.Hash, sink *objSink, sent map[plumbing.Hash]bool) error {
	defer slow("publish", "rama", branch, "objetos", len(sink.blobs)+len(sink.others))()
	src := side{local, localGitDir(r.Local), osFS{}}
	dst := side{nil, r.Bare, nas}
	pending := func(list []plumbing.Hash) []plumbing.Hash {
		out := make([]plumbing.Hash, 0, len(list))
		for _, h := range list {
			if !sent[h] {
				out = append(out, h)
			}
		}
		return out
	}
	blobs, rest := pending(sink.blobs), pending(sink.others)
	// Blobs primero y en paralelo; árboles y commit al final.
	if err := parallelCopyMode(src, dst, blobs, true); err != nil {
		return err
	}
	for _, h := range blobs {
		sent[h] = true
	}
	if err := parallelCopyMode(src, dst, rest, true); err != nil {
		return err
	}
	for _, h := range rest {
		sent[h] = true
	}
	return nil
}

// ---------------------------------------------------------------- objetos

type change struct {
	data   []byte
	blob   plumbing.Hash // si data es nil: un blob que ya existe (se reutiliza sin leerlo)
	remove bool
}

// objSink recuerda los objetos que se crearon para un commit: son los únicos
// que hay que mandar al NAS.
type objSink struct{ blobs, others []plumbing.Hash }

func (s *objSink) add(h plumbing.Hash) {
	if s != nil {
		s.others = append(s.others, h)
	}
}

func (s *objSink) addBlob(h plumbing.Hash) {
	if s != nil {
		s.blobs = append(s.blobs, h)
	}
}

func putBlob(repo *git.Repository, gitDir string, content []byte, sink *objSink) (plumbing.Hash, error) {
	h := plumbing.ComputeHash(plumbing.BlobObject, content)
	sink.addBlob(h)
	if repo.Storer.HasEncodedObject(h) == nil {
		return h, nil
	}
	return h, writeLoose(osFS{}, gitDir, h, plumbing.BlobObject, int64(len(content)), bytes.NewReader(content), false)
}

type encoder interface {
	Encode(plumbing.EncodedObject) error
}

func putEncoded(repo *git.Repository, gitDir string, e encoder, sink *objSink) (plumbing.Hash, error) {
	obj := &plumbing.MemoryObject{}
	if err := e.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	h := obj.Hash()
	sink.add(h)
	if repo.Storer.HasEncodedObject(h) == nil {
		return h, nil
	}
	rd, err := obj.Reader()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	defer rd.Close()
	return h, writeLoose(osFS{}, gitDir, h, obj.Type(), obj.Size(), rd, false)
}

// putChanges devuelve el árbol que resulta de aplicar los cambios sobre base,
// reutilizando todo lo demás sin leerlo. empty indica que la carpeta quedó vacía.
func putChanges(repo *git.Repository, gitDir string, base *object.Tree, changes map[string]change, sink *objSink) (plumbing.Hash, bool, error) {
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
				h, err := putBlob(repo, gitDir, c.data, sink)
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
		h, empty, err := putChanges(repo, gitDir, child, sub, sink)
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
	h, err := putEncoded(repo, gitDir, &object.Tree{Entries: list}, sink)
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
	Target string   // rama que avanza
	From   string   // rama cuya punta es el padre (por defecto, Target)
	Need   []string // otras ramas que Build lee y hay que traer del NAS
	Msg    string
	Actor  string
	// Build recibe la punta sobre la que se construye (puede cambiar entre
	// intentos) y devuelve los cambios y padres adicionales (un merge).
	Build func(base plumbing.Hash) (map[string]change, []plumbing.Hash, error)
}

// commit arma el commit sin tocar el directorio de trabajo, lo publica y
// avanza la rama local. Solo lee del NAS las ramas que necesita (una lectura de
// archivo cada una) y solo manda los objetos que creó. Si otro equipo publicó
// antes, rehace sobre su punta.
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
	need := append([]string{from, spec.Target}, spec.Need...)
	sent := map[plumbing.Hash]bool{}

	for attempt := 0; attempt < 6; attempt++ {
		t0 := time.Now()
		if err := r.RefreshBranches(need...); err != nil {
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
		sink := &objSink{}
		tree, empty, err := putChanges(repo, gitDir, baseTree, changes, sink)
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
		hash, err := putEncoded(repo, gitDir, c, sink)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		tBuild := time.Since(t0) - tFetch

		err = r.publishCommit(repo, spec.Target, hash, c.ParentHashes, sink, sent)
		if total := time.Since(t0); total > 300*time.Millisecond {
			slog.Info("commit lento", "rama", spec.Target, "leer", tFetch.Round(time.Millisecond).String(), "armar", tBuild.Round(time.Millisecond).String(), "publicar", (total - tFetch - tBuild).Round(time.Millisecond).String())
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

// publishCommit elige el camino: si todos los padres son puntas que ya están en
// el NAS, solo se mandan los objetos nuevos; si no (hay commits locales sin
// publicar), se recorre el historial.
func (r *Repo) publishCommit(repo *git.Repository, target string, hash plumbing.Hash, parents []plumbing.Hash, sink *objSink, sent map[plumbing.Hash]bool) error {
	tracked := map[plumbing.Hash]bool{}
	if refs, err := repo.References(); err == nil {
		_ = refs.ForEach(func(ref *plumbing.Reference) error {
			if ref.Name().IsRemote() {
				tracked[ref.Hash()] = true
			}
			return nil
		})
		refs.Close()
	}
	fast := true
	for _, p := range parents {
		if !tracked[p] {
			fast = false
		}
	}
	if !fast {
		return r.publish(repo, target, hash)
	}
	if err := r.publishFast(repo, target, hash, sink, sent); err != nil {
		return err
	}
	// La referencia solo avanza desde la punta que acabamos de leer del NAS
	// (casRef verifica que siga ahí: si otro equipo se adelantó, se reintenta).
	var old plumbing.Hash
	if tr, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", target), true); err == nil {
		old = tr.Hash()
		okAnc := false
		if cc, err := repo.CommitObject(old); err == nil {
			if tc, err := repo.CommitObject(hash); err == nil {
				okAnc, _ = cc.IsAncestor(tc)
			}
		}
		if !okAnc {
			return git.ErrNonFastForwardUpdate
		}
	}
	return r.finishPublish(repo, target, old, hash)
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
