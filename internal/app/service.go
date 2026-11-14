package app

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"lcs/internal/config"
	"lcs/internal/identity"
	"lcs/internal/store"
	"lcs/internal/vcs"
)

type Service struct {
	cfg *config.Config
	st  *store.Store

	// Cuándo se trajo por última vez lo de la carpeta compartida (ver
	// RefreshAllReviews y MaybeDiscover).
	refreshMu    sync.Mutex
	refreshedAt  time.Time
	discoveredAt time.Time
}

func New(cfg *config.Config, st *store.Store) *Service {
	return &Service{cfg: cfg, st: st}
}

func (s *Service) Config() *config.Config { return s.cfg }
func (s *Service) Store() *store.Store    { return s.st }

// Repo exposes the git repository of a project.
func (s *Service) Repo(projectID string) *vcs.Repo { return s.repo(projectID) }

func (s *Service) repo(projectID string) *vcs.Repo {
	return &vcs.Repo{
		Bare:  filepath.Join(s.cfg.ProjectsDir(), projectID, "repo.git"),
		Local: filepath.Join(s.cfg.ClonesDir(), projectID),
	}
}

// ---------------------------------------------------------------- projects

type CreateProjectInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// ParentID makes the new project a zone (subproject) of that master.
	ParentID string `json:"parentId"`
}

func (s *Service) CreateProject(in CreateProjectInput) (*store.Project, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errors.New("el nombre del proyecto es obligatorio")
	}
	parentID := strings.TrimSpace(in.ParentID)
	if parentID != "" {
		parent, err := s.st.GetProject(parentID)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			return nil, errors.New("el proyecto master no existe")
		}
		if parent.ParentID != "" {
			return nil, errors.New("un subproyecto no puede tener subproyectos: elegí el master")
		}
	}
	id := s.uniqueID(name)
	repo := s.repo(id)

	if err := repo.WithLock(func() error {
		return vcs.Init(repo.Bare, repo.Local, vcs.ProjectFile{
			ID: id, Name: name, Description: in.Description, Parent: parentID,
			Owner: identity.Normalize(s.cfg.Email),
		}, s.cfg.Actor)
	}); err != nil {
		return nil, fmt.Errorf("creando repositorio: %w", err)
	}

	p := &store.Project{ID: id, Name: name, Description: in.Description, ParentID: parentID,
		Owner: identity.Normalize(s.cfg.Email)}
	if err := s.st.CreateProject(p); err != nil {
		return nil, err
	}
	_ = s.st.AddEvent(id, "", "project.created", s.cfg.Actor, map[string]any{"name": name, "parent": parentID})
	if parentID != "" {
		_ = s.st.AddEvent(parentID, "", "subproject.created", s.cfg.Actor, map[string]any{"name": name, "zone": id})
	}
	return p, nil
}

func (s *Service) uniqueID(name string) string {
	base := slugify(name)
	if base == "" {
		base = "proyecto"
	}
	id := base
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(s.cfg.ProjectsDir(), id)); os.IsNotExist(err) {
			if existing, _ := s.st.GetProject(id); existing == nil {
				return id
			}
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
}

var slugFold = map[rune]string{
	'á': "a", 'à': "a", 'ä': "a", 'â': "a", 'ã': "a", 'å': "a",
	'é': "e", 'è': "e", 'ë': "e", 'ê': "e",
	'í': "i", 'ì': "i", 'ï': "i", 'î': "i",
	'ó': "o", 'ò': "o", 'ö': "o", 'ô': "o", 'õ': "o",
	'ú': "u", 'ù': "u", 'ü': "u", 'û': "u",
	'ñ': "n", 'ç': "c", 'ý': "y",
}

func slugify(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		if folded, ok := slugFold[r]; ok {
			if prevDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteString(folded)
			prevDash = false
			continue
		}
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if prevDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			prevDash = false
		default:
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Sync refreshes the local clone and rebuilds the SQLite index from git.
func (s *Service) Sync(projectID string) error {
	project, err := s.st.GetProject(projectID)
	if err != nil {
		return err
	}
	if project == nil {
		return fmt.Errorf("proyecto %s no existe", projectID)
	}
	repo := s.repo(projectID)
	return repo.WithLock(func() error { return s.reconcileLocked(projectID, repo) })
}

func (s *Service) reconcileLocked(projectID string, repo *vcs.Repo) error {
	// Un proyecto descubierto en la carpeta compartida todavía no tiene clon.
	if err := vcs.EnsureClone(repo.Bare, repo.Local); err != nil {
		return err
	}
	if err := repo.Pull(vcs.MainBranch); err != nil {
		return err
	}

	entries, err := repo.ListDrawings(vcs.MainBranch)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Path] = true
		existing, err := s.st.GetDrawingByPath(projectID, e.Path)
		if err != nil {
			return err
		}
		d := &store.Drawing{
			ID:        drawingID(projectID, e.Path),
			ProjectID: projectID,
			Path:      e.Path,
			Name:      displayName(e.Path),
			MainBlob:  e.Hash,
			Size:      e.Size,
		}
		if existing != nil {
			d.ID = existing.ID
			d.LayoutsJSON = existing.LayoutsJSON
			d.MetaJSON = existing.MetaJSON
		}
		if d.LayoutsJSON == "" {
			d.LayoutsJSON = "[]"
		}
		if d.MetaJSON == "" {
			d.MetaJSON = "{}"
		}
		if err := s.st.UpsertDrawing(d); err != nil {
			return err
		}
	}

	// Drawings that left main (removed, or moved out by an integration) must
	// leave the index too.
	if stale, err := s.st.ListDrawings(projectID); err == nil {
		for _, d := range stale {
			if !seen[d.Path] {
				_ = s.st.DeleteDrawing(d.ID)
			}
		}
	}

	// Revisions of main.
	if err := s.backfill(projectID, repo, vcs.MainBranch, ""); err != nil {
		return err
	}

	// Work branches.
	if err := repo.SyncBranches(); err != nil {
		return err
	}
	metas, err := repo.ListWorkBranches()
	if err != nil {
		return err
	}
	active := map[string]bool{}
	for _, m := range metas {
		active[m.Name] = true
		if err := s.upsertWorkBranch(projectID, repo, m, true); err != nil {
			return err
		}
	}

	// Una tarea abierta aquí cuya rama ya no está en la carpeta compartida fue
	// descartada en otro equipo.
	open, err := s.st.ListBranches(projectID, "active")
	if err == nil {
		for i := range open {
			if !active[open[i].Name] && strings.HasPrefix(open[i].Name, vcs.BranchPrefix) {
				s.syncBranchState(repo, &open[i])
			}
		}
	}
	_ = s.refreshProjectFile(projectID, repo)

	// Refresh drawing size/blob from main (they may have been merged in).
	return s.st.TouchProject(projectID)
}

func (s *Service) backfill(projectID string, repo *vcs.Repo, branch, branchID string) error {
	hist, err := repo.History(branch, 200)
	if err != nil {
		return nil // brand new / empty branch
	}
	for _, ch := range hist {
		_ = s.st.AddRevision(&store.Revision{
			ID:          revisionID(ch.Commit, ch.Path),
			ProjectID:   projectID,
			DrawingPath: ch.Path,
			CommitSHA:   ch.Commit,
			BlobSHA:     ch.Blob,
			Size:        ch.Size,
			Author:      ch.Author,
			Message:     ch.Message,
			BranchID:    branchID,
			CreatedAt:   ch.When,
		})
	}
	return nil
}

func (s *Service) branchFileStates(repo *vcs.Repo, branch, base, head string, declared []string) ([]store.BranchFile, error) {
	paths := declared
	if len(paths) == 0 {
		changed, err := repo.ChangedSince(base, head)
		if err != nil {
			return nil, err
		}
		paths = changed
	}
	out := []store.BranchFile{}
	seen := map[string]bool{}
	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true
		if !strings.HasPrefix(p, vcs.DrawingsDir+"/") || !strings.EqualFold(path.Ext(p), ".dwg") {
			continue
		}
		f := store.BranchFile{BranchID: branchID(branch), Path: p}
		if base != "" {
			if b, err := repo.FileAt(base, p); err == nil {
				f.BaseBlob = vcs.BlobHash(b)
			}
		}
		if b, err := repo.FileAt(head, p); err == nil {
			f.HeadBlob = vcs.BlobHash(b)
			f.SizeBytes = int64(len(b))
		}
		f.Changed = f.BaseBlob != f.HeadBlob
		out = append(out, f)
	}
	return out, nil
}

// ---------------------------------------------------------------- drawings

type IncomingFile struct {
	Name    string
	Content []byte
}

// Import copies DWG files into drawings/ on main and publishes them.
func (s *Service) Import(projectID string, files []IncomingFile, actor string) (map[string]string, error) {
	if len(files) == 0 {
		return nil, errors.New("no se recibió ningún archivo")
	}
	repo := s.repo(projectID)
	var blobs map[string]string
	err := repo.WithLock(func() error {
		if err := repo.Pull(vcs.MainBranch); err != nil {
			return err
		}
		payload := map[string][]byte{}
		for _, f := range files {
			if !strings.EqualFold(filepath.Ext(f.Name), ".dwg") {
				return fmt.Errorf("%s: solo se aceptan archivos .dwg", f.Name)
			}
			if !looksLikeDWG(f.Content) {
				return fmt.Errorf("%s: no parece un DWG válido (recibí %d bytes, cabecera %q)", f.Name, len(f.Content), head(f.Content, 6))
			}
			name := sanitizeFileName(f.Name)
			payload[vcs.DrawingsDir+"/"+name] = f.Content
		}
		names := make([]string, 0, len(payload))
		for p := range payload {
			names = append(names, path.Base(p))
		}
		sort.Strings(names)
		var err error
		blobs, _, err = repo.WriteFilesAndCommit(vcs.MainBranch, payload, nil,
			"LCS: importar "+strings.Join(names, ", "), actor)
		if err != nil {
			return err
		}
		return s.reconcileLocked(projectID, repo)
	})
	if err != nil {
		return nil, err
	}
	_ = s.st.AddEvent(projectID, "", "drawings.imported", actor, map[string]any{"count": len(files)})
	return blobs, nil
}

func (s *Service) DrawingBytes(projectID, drawingPath, ref string) ([]byte, error) {
	repo := s.repo(projectID)
	if ref == "" {
		ref = vcs.MainBranch
	}
	return repo.FileAtRef(ref, drawingPath)
}

// DrawingReader streams a DWG at any ref so the HTTP layer never buffers the
// whole plan (los planos de 150 MB se sirven de a bloques).
func (s *Service) DrawingReader(projectID, drawingPath, ref string) (io.ReadCloser, int64, error) {
	repo := s.repo(projectID)
	if ref == "" {
		ref = vcs.MainBranch
	}
	return repo.BlobReader(ref, drawingPath)
}

// Usage reports how much disk a project occupies: the bare repository on the
// shared folder and this machine's clone.
func (s *Service) Usage(projectID string) (repoBytes, cloneBytes int64, err error) {
	repoBytes, err = dirSize(filepath.Join(s.cfg.ProjectsDir(), projectID, "repo.git"))
	if err != nil {
		return 0, 0, err
	}
	cloneBytes, _ = dirSize(filepath.Join(s.cfg.ClonesDir(), projectID))
	return repoBytes, cloneBytes, nil
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// ---------------------------------------------------------------- branches

type CreateBranchInput struct {
	ProjectID string   `json:"projectId"`
	Drawings  []string `json:"drawings"`
	Title     string   `json:"title"`
	Notes     string   `json:"notes"`
	Author    string   `json:"author"`
}

type BranchView struct {
	store.Branch
	Files     []store.BranchFile `json:"files"`
	Revisions []store.Revision   `json:"revisions"`
	Ahead     int                `json:"ahead"`
}

func (s *Service) CreateBranch(in CreateBranchInput) (*store.Branch, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, errors.New("la tarea necesita un título")
	}
	if len(in.Drawings) == 0 {
		return nil, errors.New("elegí al menos un plano")
	}
	project, err := s.st.GetProject(in.ProjectID)
	if err != nil || project == nil {
		return nil, fmt.Errorf("proyecto inválido")
	}

	repo := s.repo(in.ProjectID)
	name := fmt.Sprintf("%s%s-%s", vcs.BranchPrefix, slugify(title), randomHex(3))
	author := in.Author
	if strings.TrimSpace(author) == "" {
		author = s.cfg.Actor
	}

	var out *store.Branch
	err = repo.WithLock(func() error {
		if err := repo.Pull(vcs.MainBranch); err != nil {
			return err
		}
		base, err := repo.HeadOf(vcs.MainBranch)
		if err != nil {
			return err
		}
		normalized := make([]string, 0, len(in.Drawings))
		for _, p := range in.Drawings {
			if !strings.HasPrefix(p, vcs.DrawingsDir+"/") {
				p = vcs.DrawingsDir + "/" + path.Base(p)
			}
			if _, err := repo.FileAtRef(vcs.MainBranch, p); err != nil {
				return fmt.Errorf("%s no existe en el proyecto", path.Base(p))
			}
			normalized = append(normalized, p)
		}

		meta := vcs.BranchMeta{
			Name:        name,
			Title:       title,
			Author:      author,
			AuthorEmail: identity.Normalize(s.cfg.Email),
			BaseCommit:  base,
			CreatedAt:   time.Now().UnixMilli(),
			Drawings:    normalized,
			Notes:       in.Notes,
		}
		head, err := repo.CreateBranch(name, meta, author)
		if err != nil {
			return err
		}

		tree, _ := repo.DrawingsTreeHash(name)
		b := &store.Branch{
			ID:          branchID(name),
			ProjectID:   in.ProjectID,
			Name:        name,
			Title:       title,
			Author:      author,
			AuthorEmail: meta.AuthorEmail,
			Status:      "open",
			BaseCommit:  base,
			HeadCommit:  head,
			TreeHash:    tree,
			// La misma fecha que queda escrita en git, para que todos los
			// equipos coincidan en cuándo se abrió.
			CreatedAt: meta.CreatedAt,
		}
		if err := s.st.CreateBranch(b); err != nil {
			return err
		}
		files, err := s.branchFileStates(repo, name, base, head, normalized)
		if err == nil {
			_ = s.st.SetBranchFiles(b.ID, files)
		}
		out = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	_ = s.st.AddEvent(in.ProjectID, out.ID, "branch.created", author, map[string]any{
		"title": title, "drawings": in.Drawings,
	})
	return out, nil
}

func (s *Service) BranchView(branchID string) (*BranchView, error) {
	b, err := s.st.GetBranch(branchID)
	if err != nil || b == nil {
		return nil, fmt.Errorf("rama no encontrada")
	}
	files, _ := s.st.ListBranchFiles(b.ID)
	revs, _ := s.st.ListRevisions(b.ProjectID, "", 40)
	own := []store.Revision{}
	for _, r := range revs {
		if r.BranchID == b.ID {
			own = append(own, r)
		}
	}
	if len(own) == 0 {
		own = revs
	}
	return &BranchView{Branch: *b, Files: files, Revisions: own, Ahead: len(files)}, nil
}

// Upload replaces the drawing content of a work branch.
func (s *Service) Upload(branchID string, file IncomingFile, message, actor string) (*store.Branch, error) {
	b, err := s.st.GetBranch(branchID)
	if err != nil || b == nil {
		return nil, errors.New("rama no encontrada")
	}
	if b.Status == "merged" || b.Status == "closed" {
		return nil, errors.New("la rama ya está cerrada")
	}
	if !looksLikeDWG(file.Content) {
		return nil, errors.New("el archivo no parece un DWG válido")
	}

	files, err := s.st.ListBranchFiles(b.ID)
	if err != nil {
		return nil, err
	}
	target := matchTarget(files, file.Name)
	if target == "" {
		return nil, fmt.Errorf("no pude asociar %s a ningún plano de la rama", file.Name)
	}

	repo := s.repo(b.ProjectID)
	var commit string
	err = repo.WithLock(func() error {
		c, _, err := repo.WriteAndCommit(b.Name, target, file.Content, commitMessage(message, path.Base(target)), actor)
		commit = c
		return err
	})
	if err != nil {
		return nil, err
	}

	b.HeadCommit = commit
	if b.Status == "open" {
		b.Status = "review"
	}
	// Un plano nuevo cambia el contenido revisado: las aprobaciones previas
	// quedan obsoletas (se comparan contra este hash).
	if tree, err := repo.DrawingsTreeHash(b.Name); err == nil {
		b.TreeHash = tree
	}
	if err := s.st.UpdateBranch(b); err != nil {
		return nil, err
	}
	declared := []string{}
	if meta, err := repo.Meta(b.Name); err == nil && meta != nil {
		declared = meta.Drawings
	}
	if states, err := s.branchFileStates(repo, b.Name, b.BaseCommit, commit, declared); err == nil && len(states) > 0 {
		_ = s.st.SetBranchFiles(b.ID, states)
	} else {
		_ = s.st.SetBranchFiles(b.ID, []store.BranchFile{{
			BranchID: b.ID, Path: target, HeadBlob: vcs.BlobHash(file.Content), SizeBytes: int64(len(file.Content)),
		}})
	}
	_ = s.st.AddRevision(&store.Revision{
		ID:          revisionID(commit, target),
		ProjectID:   b.ProjectID,
		DrawingPath: target,
		CommitSHA:   commit,
		BlobSHA:     vcs.BlobHash(file.Content),
		Size:        int64(len(file.Content)),
		Author:      actor,
		Message:     message,
		BranchID:    b.ID,
	})
	_ = s.st.AddEvent(b.ProjectID, b.ID, "branch.uploaded", actor, map[string]any{
		"branch": b.Title, "file": path.Base(target), "size": len(file.Content),
	})
	return b, nil
}

func matchTarget(files []store.BranchFile, uploaded string) string {
	if len(files) == 1 {
		return files[0].Path
	}
	base := strings.ToLower(sanitizeFileName(uploaded))
	for _, f := range files {
		if strings.ToLower(path.Base(f.Path)) == base {
			return f.Path
		}
	}
	for _, f := range files {
		if strings.HasPrefix(strings.ToLower(base), strings.TrimSuffix(strings.ToLower(path.Base(f.Path)), ".dwg")) {
			return f.Path
		}
	}
	return ""
}

// Merge approves a work branch and publishes it to main.
func (s *Service) Merge(branchID, strategy, actor string) (*vcs.MergeResult, error) {
	b, err := s.st.GetBranch(branchID)
	if err != nil || b == nil {
		return nil, errors.New("rama no encontrada")
	}
	if b.Status == "merged" {
		return nil, errors.New("la rama ya fue integrada")
	}
	repo := s.repo(b.ProjectID)
	var res *vcs.MergeResult
	err = repo.WithLock(func() error {
		// Compuerta de aprobaciones. Se decide con lo último de la carpeta
		// compartida, no con la copia local: otro equipo pudo aprobar, rechazar
		// o subir un plano nuevo hace un minuto.
		if err := repo.SyncBranches(); err != nil {
			return err
		}
		_ = s.refreshProjectFile(b.ProjectID, repo)
		if err := s.refreshReview(repo, b); err != nil {
			return err
		}
		if st := s.reviewState(b); st.Required && !st.Complete {
			return &ErrNeedsApproval{State: st}
		}

		changed, err := repo.ChangedSince(b.BaseCommit, b.Name)
		if err != nil {
			return err
		}
		if len(changed) == 0 {
			return errors.New("la rama no tiene cambios para integrar")
		}
		res, err = repo.MergeBranch(b.Name, b.BaseCommit, actor, strategy, "LCS: integrar "+b.Title)
		if err != nil {
			return err
		}
		return s.reconcileLocked(b.ProjectID, repo)
	})
	if err != nil {
		return nil, err
	}

	b.Status = "merged"
	b.HeadCommit = res.Commit
	b.MergedAt = time.Now().UnixMilli()
	_ = s.st.UpdateBranch(b)
	_ = s.st.AddEvent(b.ProjectID, b.ID, "branch.merged", actor, map[string]any{
		"branch": b.Title, "strategy": strategy, "paths": res.Paths,
	})
	return res, nil
}

// RestoreDrawing puts the content of an old revision back on main. History is
// never rewritten: the restore shows up as a new commit and a new revision.
func (s *Service) RestoreDrawing(drawingID, commit, actor string) (*store.Drawing, error) {
	d, err := s.st.GetDrawing(drawingID)
	if err != nil || d == nil {
		return nil, errors.New("plano no encontrado")
	}
	if len(commit) < 7 {
		return nil, errors.New("revisión inválida")
	}
	repo := s.repo(d.ProjectID)
	var content []byte
	var newCommit string
	msg := fmt.Sprintf("LCS: restaurar %s a %s", d.Name, commit[:10])

	err = repo.WithLock(func() error {
		b, err := repo.FileAt(commit, d.Path)
		if err != nil {
			return errors.New("esa revisión no contiene este plano")
		}
		content = b
		_, sha, err := repo.WriteFilesAndCommit(
			vcs.MainBranch,
			map[string][]byte{d.Path: b},
			nil,
			msg,
			actor,
		)
		if err != nil {
			return err
		}
		newCommit = sha
		return s.reconcileLocked(d.ProjectID, repo)
	})
	if err != nil {
		return nil, err
	}

	_ = s.st.AddRevision(&store.Revision{
		ID:          revisionID(newCommit, d.Path),
		ProjectID:   d.ProjectID,
		DrawingPath: d.Path,
		CommitSHA:   newCommit,
		BlobSHA:     vcs.BlobHash(content),
		Size:        int64(len(content)),
		Author:      actor,
		Message:     msg,
	})
	_ = s.st.AddEvent(d.ProjectID, "", "drawing.restored", actor, map[string]any{
		"drawing": d.Name,
		"from":    commit[:10],
	})

	return s.st.GetDrawing(drawingID)
}

func (s *Service) CloseBranch(branchID, actor string) error {
	b, err := s.st.GetBranch(branchID)
	if err != nil || b == nil {
		return errors.New("rama no encontrada")
	}
	repo := s.repo(b.ProjectID)
	_ = repo.WithLock(func() error {
		return repo.DeleteBranch(b.Name)
	})
	b.Status = "closed"
	_ = s.st.UpdateBranch(b)
	_ = s.st.AddEvent(b.ProjectID, b.ID, "branch.closed", actor, map[string]any{"branch": b.Title})
	return nil
}

// BundleName returns the download name for a task without building the ZIP.
func (s *Service) BundleName(branchID string) (string, error) {
	b, err := s.st.GetBranch(branchID)
	if err != nil || b == nil {
		return "", errors.New("rama no encontrada")
	}
	return slugify(b.Title) + ".zip", nil
}

// WriteBundle streams the ZIP a drafter downloads to edit a task. Streaming
// matters: con planos de 150 MB, armar el ZIP en memoria cuesta 300 MB de RAM.
func (s *Service) WriteBundle(w io.Writer, branchID string) error {
	b, err := s.st.GetBranch(branchID)
	if err != nil || b == nil {
		return errors.New("rama no encontrada")
	}
	files, err := s.st.ListBranchFiles(b.ID)
	if err != nil {
		return err
	}
	repo := s.repo(b.ProjectID)

	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	if len(paths) == 0 {
		if meta, err := repo.Meta(b.Name); err == nil && meta != nil {
			paths = meta.Drawings
		}
	}

	zw := zip.NewWriter(w)

	manifest := map[string]any{
		"lcs":       "LCS — Layout Control System",
		"branch":    b.Name,
		"branchId":  b.ID,
		"title":     b.Title,
		"author":    b.Author,
		"base":      b.BaseCommit,
		"createdAt": b.CreatedAt,
		"drawings":  []string{},
		"instructions": []string{
			"1. Abrí los DWG de la carpeta drawings/ en AutoCAD.",
			"2. Editá los layouts necesarios y guardá el archivo con el MISMO nombre.",
			"3. Subí el DWG desde LCS → tarea → Subir cambios.",
		},
	}

	names := []string{}
	for _, p := range paths {
		if err := copyBlobToZip(zw, repo, b.Name, p); err != nil {
			if err := copyBlobToZip(zw, repo, vcs.MainBranch, p); err != nil {
				continue
			}
		}
		names = append(names, path.Base(p))
	}
	manifest["drawings"] = names

	raw, _ := json.MarshalIndent(manifest, "", "  ")
	if err := addZip(zw, "TAREA-LCS.json", raw); err != nil {
		return err
	}
	if err := addZip(zw, "LEEME.txt", []byte(bundleReadme(b))); err != nil {
		return err
	}
	return zw.Close()
}

// copyBlobToZip streams one blob into the archive without buffering it whole.
func copyBlobToZip(zw *zip.Writer, repo *vcs.Repo, revision, p string) error {
	rd, _, err := repo.BlobReader(revision, p)
	if err != nil {
		return err
	}
	defer rd.Close()
	dst, err := zw.Create(path.Base(p))
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, rd)
	return err
}

func bundleReadme(b *store.Branch) string {
	return fmt.Sprintf(`LCS — Layout Control System
===========================

Tarea:   %s
Rama:    %s
Autor:   %s
Base:    %s

Pasos
-----
1. Extraé los DWG de la carpeta drawings/.
2. Editalos en AutoCAD sin cambiarles el nombre.
3. Volvé a la tarea en LCS y subí el archivo con "Subir cambios".
4. Cuando esté conforme, aprobá la tarea para publicarla en main.

Importante: no renombres los archivos. LCS usa el nombre para
asociar el DWG subido con el plano de la rama.
`, b.Title, b.Name, b.Author, shortSHA(b.BaseCommit))
}

func (s *Service) Stats() (map[string]any, error) {
	projects, _ := s.st.ListProjects()
	var drawings, openBranches int
	for _, p := range projects {
		ds, _ := s.st.ListDrawings(p.ID)
		drawings += len(ds)
		bs, _ := s.st.ListBranches(p.ID, "active")
		openBranches += len(bs)
	}
	return map[string]any{
		"projects":     len(projects),
		"drawings":     drawings,
		"openBranches": openBranches,
		"workspace":    s.cfg.Workspace,
		"actor":        s.cfg.Actor,
	}, nil
}

// ---------------------------------------------------------------- helpers

// drawingID keeps the folder in the slug so that drawings/norte/planta.dwg and
// drawings/sur/planta.dwg (a master holds one folder per zone) do not collide.
// For flat paths the result is the same as always.
func drawingID(projectID, p string) string {
	return projectID + "--" + slugify(drawingRel(p))
}

// drawingRel is the path inside drawings/ without extension.
func drawingRel(p string) string {
	return strings.TrimSuffix(strings.TrimPrefix(p, vcs.DrawingsDir+"/"), path.Ext(p))
}

func branchID(name string) string { return slugify(strings.TrimPrefix(name, vcs.BranchPrefix)) }

func revisionID(commit, p string) string {
	c := commit
	if len(c) > 10 {
		c = c[:10]
	}
	return c + "-" + slugify(strings.TrimSuffix(path.Base(p), path.Ext(p)))
}

func displayName(p string) string { return drawingRel(p) }

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func commitMessage(userMessage, file string) string {
	m := strings.TrimSpace(userMessage)
	if m == "" {
		return "LCS: actualizar " + file
	}
	return "LCS: " + m
}

func sanitizeFileName(name string) string {
	name = filepath.Base(name)
	repl := strings.NewReplacer(" ", "_", "(", "", ")", "", "#", "", "&", "y")
	name = repl.Replace(name)
	if name == "" || name == "." {
		name = "plano.dwg"
	}
	return name
}

// looksLikeDWG validates the 6-byte AutoCAD signature ("AC1015" … "AC1032").
func head(b []byte, n int) string {
	if len(b) < n {
		n = len(b)
	}
	return string(b[:n])
}

func looksLikeDWG(b []byte) bool {
	if len(b) < 6 {
		return false
	}
	return b[0] == 'A' && b[1] == 'C' && b[2] >= '1' && b[2] <= '9'
}

func addZip(zw *zip.Writer, name string, content []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(content)
	return err
}

// Seed imports the DWGs found in LCS_SEED_DIR into a project (used for demos).
func (s *Service) Seed(projectID, dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	files := []IncomingFile{}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".dwg") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		files = append(files, IncomingFile{Name: e.Name(), Content: raw})
	}
	if len(files) == 0 {
		return 0, nil
	}
	if _, err := s.Import(projectID, files, s.cfg.Actor); err != nil {
		return 0, err
	}
	return len(files), nil
}
