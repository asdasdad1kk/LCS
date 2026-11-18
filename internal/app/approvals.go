package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"lcs/internal/identity"
	"lcs/internal/store"
	"lcs/internal/vcs"
)

const (
	maxApprovers   = 12
	maxLabelLength = 40
)

// Identity es quien usa esta máquina (sesión de Windows o LCS_EMAIL).
func (s *Service) Identity() identity.Identity {
	return identity.Identity{
		Email:    s.cfg.Email,
		Name:     identity.NameOf(s.cfg.Email),
		Verified: s.cfg.EmailVerified,
		Source:   s.cfg.EmailSource,
	}
}

// approvalFile es lo que se guarda en .lcs/approvals/<ms>-<correo>.json dentro
// de la rama de trabajo. Cada decisión es un archivo nuevo: nadie pisa a nadie.
type approvalFile struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Decision string `json:"decision"`
	Comment  string `json:"comment,omitempty"`
	Tree     string `json:"tree"`
	Commit   string `json:"commit"`
	At       int64  `json:"at"`
}

func parseApprovals(files []vcs.NamedFile) []store.Approval {
	out := []store.Approval{}
	for _, f := range files {
		if !strings.HasSuffix(f.Name, ".json") {
			continue
		}
		var af approvalFile
		if json.Unmarshal(f.Content, &af) != nil {
			continue
		}
		email := identity.Normalize(af.Email)
		if !identity.IsEmail(email) || (af.Decision != "approved" && af.Decision != "rejected") {
			continue
		}
		out = append(out, store.Approval{
			ID: strings.TrimSuffix(f.Name, ".json"), Email: email, Name: af.Name,
			Decision: af.Decision, Comment: af.Comment, Tree: af.Tree, Commit: af.Commit, At: af.At,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At != out[j].At {
			return out[i].At < out[j].At
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// ---------------------------------------------------------------- mirror
//
// La verdad está en git (carpeta compartida); SQLite es una copia para
// consultar rápido. Estas funciones la ponen al día. Se llaman con el clon
// bloqueado.

// refreshReview relee de git el contenido de drawings/ y las decisiones de una
// rama.
func (s *Service) refreshReview(repo *vcs.Repo, b *store.Branch) error {
	tree, err := repo.DrawingsTreeHash(b.Name)
	if err != nil {
		return err
	}
	files, err := repo.ListFilesAt(b.Name, vcs.ApprovalsDir)
	if err != nil {
		return err
	}
	if err := s.st.ReplaceApprovals(b.ID, parseApprovals(files)); err != nil {
		return err
	}
	b.TreeHash = tree
	if err := s.st.SetBranchTree(b.ID, tree); err != nil {
		return err
	}
	if b.Status == "open" || b.Status == "review" {
		s.refreshBranchFiles(repo, b)
	}
	return nil
}

// refreshBranchFiles recalcula, desde git, qué planos de la tarea difieren de la
// base. La copia en SQLite podía quedar vieja (p. ej. tras subir una revisión) y
// entonces la tarea decía "no hay cambios subidos" aunque sí los hubiera. Solo
// compara hashes de blobs: no lee los DWG.
func (s *Service) refreshBranchFiles(repo *vcs.Repo, b *store.Branch) {
	base := b.BaseCommit
	declared := []string{}
	if meta, err := repo.Meta(b.Name); err == nil && meta != nil {
		declared = meta.Drawings
		if base == "" {
			base = meta.BaseCommit
		}
	}
	files, err := s.branchFileStates(repo, b.Name, base, b.Name, declared)
	if err != nil || len(files) == 0 {
		return
	}
	_ = s.st.SetBranchFiles(b.ID, files)
}

// refreshProjectFile relee la cadena de aprobación y el dueño del proyecto
// desde .lcs/project.json en main.
func (s *Service) refreshProjectFile(projectID string, repo *vcs.Repo) error {
	pf, err := repo.ReadProjectFile()
	if err != nil {
		return err // proyecto anterior a las aprobaciones: no hay archivo
	}
	chain := make([]store.Approver, 0, len(pf.Approvers))
	for _, a := range pf.Approvers {
		chain = append(chain, store.Approver{Email: identity.Normalize(a.Email), Label: a.Label})
	}
	if err := s.st.ReplaceApprovers(projectID, chain); err != nil {
		return err
	}
	if pf.Owner != "" {
		return s.st.SetProjectOwner(projectID, identity.Normalize(pf.Owner))
	}
	return nil
}

// upsertWorkBranch pone al día la fila de una tarea con lo que dice git (una
// rama de trabajo con su .lcs/branch.json). Si history es false no recorre el
// historial de commits de una tarea que ya se conocía y no avanzó.
func (s *Service) upsertWorkBranch(projectID string, repo *vcs.Repo, m vcs.BranchMeta, history bool) error {
	head, err := repo.HeadOf(m.Name)
	if err != nil {
		return nil // la rama ya no está
	}
	b, err := s.st.GetBranchByName(m.Name)
	if err != nil {
		return err
	}
	// Las aprobaciones mueven la punta de la rama pero nunca drawings/: releer
	// los DWG de la tarea solo hace falta cuando el contenido cambió de verdad.
	tree, _ := repo.DrawingsTreeHash(m.Name)
	isNew := b == nil
	moved := isNew
	contentChanged := isNew
	if isNew {
		b = &store.Branch{
			ID:          branchID(m.Name),
			ProjectID:   projectID,
			Name:        m.Name,
			Title:       m.Title,
			Author:      m.Author,
			AuthorEmail: identity.Normalize(m.AuthorEmail),
			Status:      "open",
			BaseCommit:  m.BaseCommit,
			HeadCommit:  head,
			CreatedAt:   m.CreatedAt,
		}
		if err := s.st.CreateBranch(b); err != nil {
			return err
		}
	} else {
		moved = b.HeadCommit != head
		contentChanged = tree != b.TreeHash
		b.HeadCommit = head
		if b.Title == "" {
			b.Title = m.Title
		}
		if b.AuthorEmail == "" {
			b.AuthorEmail = identity.Normalize(m.AuthorEmail)
		}
		if history || moved {
			if err := s.st.UpdateBranch(b); err != nil {
				return err
			}
		}
	}

	if history || contentChanged {
		if files, err := s.branchFileStates(repo, m.Name, m.BaseCommit, head, m.Drawings); err == nil {
			_ = s.st.SetBranchFiles(b.ID, files)
			// "En revisión" es que ya hay una versión subida; otro equipo lo
			// sabe porque los planos de la rama difieren de main.
			if b.Status == "open" {
				for _, f := range files {
					if f.Changed {
						b.Status = "review"
						_ = s.st.UpdateBranch(b)
						break
					}
				}
			}
		}
		_ = s.backfill(projectID, repo, m.Name, b.ID)
	}

	// Lo que otros equipos hicieron con esta tarea: integrarla, aprobarla.
	s.syncBranchState(repo, b)
	if b.Status == "open" || b.Status == "review" {
		_ = s.refreshReview(repo, b)
	}
	return nil
}

// syncBranchState corrige el estado de una tarea con lo que dice git: si su
// contenido ya está en main fue integrada (aunque lo hiciera otro equipo), y
// si la rama desapareció de la carpeta compartida fue descartada.
func (s *Service) syncBranchState(repo *vcs.Repo, b *store.Branch) {
	if b.Status != "open" && b.Status != "review" {
		return
	}
	if _, err := repo.HeadOf(b.Name); err != nil {
		b.Status = "closed"
		_ = s.st.UpdateBranch(b)
		return
	}
	if repo.IsMerged(b.Name) {
		b.Status = "merged"
		if b.MergedAt == 0 {
			b.MergedAt = time.Now().UnixMilli()
		}
		_ = s.st.UpdateBranch(b)
	}
}

// RefreshReviews trae lo último de la carpeta compartida para un proyecto:
// ramas nuevas, aprobaciones de otros equipos, cambios en la cadena. Si el
// clon está ocupado con otra operación no espera: sirve lo que ya hay.
func (s *Service) RefreshReviews(projectID string) {
	defer trace("RefreshReviews")()
	repo := s.repo(projectID)
	_, err := repo.TryLock(func() error {
		if err := vcs.EnsureClone(repo.Bare, repo.Local); err != nil {
			return err
		}
		if err := repo.SyncBranches(); err != nil {
			return err
		}
		_ = s.refreshProjectFile(projectID, repo)

		// Tareas nuevas o avanzadas desde otros equipos.
		metas, err := repo.ListWorkBranches()
		if err != nil {
			return err
		}
		inGit := map[string]bool{}
		for _, m := range metas {
			inGit[m.Name] = true
			if err := s.upsertWorkBranch(projectID, repo, m, false); err != nil {
				return err
			}
		}
		// Tareas conocidas cuya rama desapareció: se descartaron en otro equipo.
		known, err := s.st.ListBranches(projectID, "active")
		if err != nil {
			return err
		}
		for i := range known {
			if !inGit[known[i].Name] {
				s.syncBranchState(repo, &known[i])
			}
		}
		return nil
	})
	if err != nil {
		slog.Debug("refrescar aprobaciones", "project", projectID, "err", err)
	}
}

// RefreshAllReviews hace RefreshReviews de todos los proyectos, a lo sumo una
// vez por ttl.
func (s *Service) RefreshAllReviews(ttl time.Duration) {
	s.refreshMu.Lock()
	if time.Since(s.refreshedAt) < ttl {
		s.refreshMu.Unlock()
		return
	}
	s.refreshedAt = time.Now()
	s.refreshMu.Unlock()

	projects, err := s.st.ListProjects()
	if err != nil {
		return
	}
	for _, p := range projects {
		s.RefreshReviews(p.ID)
	}
}

// ---------------------------------------------------------------- estado

func (s *Service) reviewState(b *store.Branch) ReviewState {
	chain, _ := s.st.ListApprovers(b.ProjectID)
	decisions, _ := s.st.ListApprovals(b.ID)
	files, _ := s.st.ListBranchFiles(b.ID)
	hasChanges := false
	for _, f := range files {
		if f.Changed {
			hasChanges = true
			break
		}
	}
	st := computeReview(chain, decisions, b.TreeHash, b.AuthorEmail, hasChanges)
	st.Uploaded = b.Status == "review"
	st.markMe(s.cfg.Email)
	return st
}

type TimelineItem struct {
	Kind    string `json:"kind"` // opened | upload | approved | rejected | merged | closed
	At      int64  `json:"at"`
	Actor   string `json:"actor"`
	Email   string `json:"email"`
	Message string `json:"message"`
	Commit  string `json:"commit,omitempty"`
	Size    int64  `json:"size,omitempty"`
	// Node: posición del aprobador en la cadena (-1 si ya no está en ella).
	Node  int    `json:"node"`
	Label string `json:"label,omitempty"`
	// Stale: la decisión es de una versión anterior de los planos.
	Stale bool `json:"stale"`
}

type Overlap struct {
	BranchID    string   `json:"branchId"`
	Title       string   `json:"title"`
	Author      string   `json:"author"`
	AuthorEmail string   `json:"authorEmail"`
	Status      string   `json:"status"`
	Paths       []string `json:"paths"`
}

type ReviewView struct {
	ReviewState
	Me       identity.Identity `json:"me"`
	MyIndex  int               `json:"myIndex"`
	CanAct   bool              `json:"canAct"`
	Timeline []TimelineItem    `json:"timeline"`
	Overlaps []Overlap         `json:"overlaps"`
}

// Review arma todo lo que la pantalla de una tarea necesita sobre aprobaciones.
func (s *Service) Review(branchID string) (*ReviewView, error) {
	b, err := s.st.GetBranch(branchID)
	if err != nil || b == nil {
		return nil, errors.New("rama no encontrada")
	}
	s.RefreshReviews(b.ProjectID)
	if fresh, err := s.st.GetBranch(branchID); err == nil && fresh != nil {
		b = fresh
	}
	return s.reviewView(b), nil
}

// reviewView arma la vista desde el índice local, sin ir a la red.
func (s *Service) reviewView(b *store.Branch) *ReviewView {
	st := s.reviewState(b)
	my := st.myIndex(s.cfg.Email)
	active := b.Status == "open" || b.Status == "review"
	return &ReviewView{
		ReviewState: st,
		Me:          s.Identity(),
		MyIndex:     my,
		CanAct:      active && st.HasChanges && my >= 0 && st.Next == my,
		Timeline:    s.timeline(b, st),
		Overlaps:    s.overlaps(b),
	}
}

func (s *Service) timeline(b *store.Branch, st ReviewState) []TimelineItem {
	items := []TimelineItem{{
		Kind: "opened", At: b.CreatedAt, Actor: b.Author, Email: b.AuthorEmail, Message: b.Title, Node: -1,
	}}
	if revs, err := s.st.ListRevisions(b.ProjectID, "", 400); err == nil {
		for _, r := range revs {
			if r.BranchID != b.ID {
				continue
			}
			msg := strings.TrimSpace(strings.TrimPrefix(r.Message, "LCS:"))
			items = append(items, TimelineItem{
				Kind: "upload", At: r.CreatedAt, Actor: r.Author, Message: msg,
				Commit: r.CommitSHA, Size: r.Size, Node: -1,
			})
		}
	}
	position := map[string]store.Approver{}
	index := map[string]int{}
	for i, n := range st.Nodes {
		position[n.Email] = store.Approver{Email: n.Email, Label: n.Label}
		index[n.Email] = i
	}
	decisions, _ := s.st.ListApprovals(b.ID)
	for _, d := range decisions {
		node := -1
		if i, ok := index[d.Email]; ok {
			node = i
		}
		name := d.Name
		if name == "" {
			name = identity.NameOf(d.Email)
		}
		items = append(items, TimelineItem{
			Kind: d.Decision, At: d.At, Actor: name, Email: d.Email, Message: d.Comment,
			Commit: d.Commit, Node: node, Label: position[d.Email].Label,
			Stale: d.Tree != b.TreeHash || b.TreeHash == "",
		})
	}
	switch b.Status {
	case "merged":
		at := b.MergedAt
		if at == 0 {
			at = b.UpdatedAt
		}
		items = append(items, TimelineItem{Kind: "merged", At: at, Message: "Integrada en main", Node: -1})
	case "closed":
		items = append(items, TimelineItem{Kind: "closed", At: b.UpdatedAt, Message: "Tarea descartada", Node: -1})
	}
	// git guarda las horas de los commits en segundos y la apertura en
	// milisegundos: dentro del mismo segundo la apertura podría quedar después
	// de la primera subida. La apertura va siempre primero y el cierre al final.
	rank := func(k string) int {
		switch k {
		case "opened":
			return 0
		case "merged", "closed":
			return 2
		}
		return 1
	}
	sort.SliceStable(items, func(i, j int) bool {
		if ri, rj := rank(items[i].Kind), rank(items[j].Kind); ri != rj {
			return ri < rj
		}
		return items[i].At < items[j].At
	})
	return items
}

// overlaps lista otras tareas abiertas del proyecto que editan los mismos
// planos: dos ediciones en paralelo de un DWG no se pueden fusionar.
func (s *Service) overlaps(b *store.Branch) []Overlap {
	mine, _ := s.st.ListBranchFiles(b.ID)
	paths := map[string]bool{}
	for _, f := range mine {
		paths[f.Path] = true
	}
	out := []Overlap{}
	others, err := s.st.ListBranches(b.ProjectID, "active")
	if err != nil {
		return out
	}
	for _, o := range others {
		if o.ID == b.ID {
			continue
		}
		files, _ := s.st.ListBranchFiles(o.ID)
		shared := []string{}
		for _, f := range files {
			if paths[f.Path] {
				shared = append(shared, f.Path)
			}
		}
		if len(shared) > 0 {
			out = append(out, Overlap{
				BranchID: o.ID, Title: o.Title, Author: o.Author, AuthorEmail: o.AuthorEmail,
				Status: o.Status, Paths: shared,
			})
		}
	}
	return out
}

// Editor es alguien que ya tiene una tarea abierta sobre un plano.
type Editor struct {
	BranchID    string `json:"branchId"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	AuthorEmail string `json:"authorEmail"`
	CreatedAt   int64  `json:"createdAt"`
}

// Editing dice, por plano, quién lo tiene en edición. Con fresh=true consulta
// la carpeta compartida (ve tareas abiertas desde otros equipos hace segundos);
// si no, usa la copia local.
func (s *Service) Editing(projectID string, fresh bool) map[string][]Editor {
	out := map[string][]Editor{}
	if fresh {
		s.RefreshReviews(projectID)
	}
	branches, err := s.st.ListBranches(projectID, "active")
	if err != nil {
		return out
	}
	for _, b := range branches {
		files, _ := s.st.ListBranchFiles(b.ID)
		for _, f := range files {
			out[f.Path] = append(out[f.Path], Editor{
				BranchID: b.ID, Title: b.Title, Author: b.Author, AuthorEmail: b.AuthorEmail, CreatedAt: b.CreatedAt,
			})
		}
	}
	return out
}

// ---------------------------------------------------------------- decidir

// Decide registra la aprobación o el rechazo del usuario de esta máquina.
func (s *Service) Decide(branchID, decision, comment string) (*ReviewView, error) {
	defer trace("Decide")()
	decision = strings.TrimSpace(decision)
	comment = strings.TrimSpace(comment)
	if decision != "approved" && decision != "rejected" {
		return nil, errors.New("decisión inválida")
	}
	if decision == "rejected" && comment == "" {
		return nil, errors.New("al rechazar hay que explicar el motivo")
	}
	me := identity.Normalize(s.cfg.Email)
	if !identity.IsEmail(me) {
		return nil, errors.New("no pude identificarte: configura LCS_EMAIL")
	}

	b, err := s.st.GetBranch(branchID)
	if err != nil || b == nil {
		return nil, errors.New("rama no encontrada")
	}
	repo := s.repo(b.ProjectID)

	err = repo.WithLock(func() error {
		// Decidir sobre datos viejos es decidir a ciegas: traer lo último.
		if err := repo.SyncBranches(); err != nil {
			return err
		}
		_ = s.refreshProjectFile(b.ProjectID, repo)
		s.syncBranchState(repo, b)
		if fresh, err := s.st.GetBranch(branchID); err == nil && fresh != nil {
			b = fresh
		}
		if b.Status != "open" && b.Status != "review" {
			return errors.New("la tarea ya está cerrada")
		}
		if err := s.refreshReview(repo, b); err != nil {
			return err
		}

		st := s.reviewState(b)
		switch {
		case !st.Required:
			return errors.New("el proyecto no tiene aprobadores definidos")
		case !st.HasChanges && st.Uploaded:
			return errors.New("el plano subido es idéntico al de main: no hay diferencias que aprobar. Subí una versión con cambios")
		case !st.HasChanges:
			return errors.New("la tarea no tiene cambios para aprobar")
		}
		idx := st.myIndex(me)
		if idx < 0 {
			return fmt.Errorf("%s no está en la cadena de aprobación de este proyecto", me)
		}
		if st.Next != idx {
			node := st.Nodes[idx]
			if node.Status == NodeApproved {
				return errors.New("ya aprobaste esta versión")
			}
			next := st.Nodes[st.Next]
			return fmt.Errorf("todavía no es tu turno: falta que decida %s", next.Email)
		}

		head, err := repo.HeadOf(b.Name)
		if err != nil {
			return err
		}
		now := time.Now().UnixMilli()
		raw, _ := json.MarshalIndent(approvalFile{
			Email: me, Name: identity.NameOf(me), Decision: decision, Comment: comment,
			Tree: b.TreeHash, Commit: head, At: now,
		}, "", "  ")
		file := fmt.Sprintf("%s/%013d-%s.json", vcs.ApprovalsDir, now, slugify(me))
		verb := map[string]string{"approved": "aprobar", "rejected": "rechazar"}[decision]
		commit, err := repo.CommitFilesOnBranch(b.Name,
			map[string][]byte{file: raw}, fmt.Sprintf("LCS: %s %s", verb, b.Title), me)
		if err != nil {
			return err
		}
		b.HeadCommit = commit
		_ = s.st.UpdateBranch(b)
		return s.refreshReview(repo, b)
	})
	if err != nil {
		return nil, err
	}

	_ = s.st.AddEvent(b.ProjectID, b.ID, "branch."+decision, me, map[string]any{
		"branch": b.Title, "comment": comment,
	})
	// Ya se refrescó todo dentro del bloqueo: no se repite el viaje al NAS.
	if fresh, err := s.st.GetBranch(branchID); err == nil && fresh != nil {
		b = fresh
	}
	return s.reviewView(b), nil
}

// ---------------------------------------------------------------- cadena

type ApproversView struct {
	Approvers []store.Approver  `json:"approvers"`
	Owner     string            `json:"owner"`
	CanEdit   bool              `json:"canEdit"`
	Me        identity.Identity `json:"me"`
}

func canEditChain(me, owner string, chain []store.Approver) bool {
	if len(chain) == 0 || (owner != "" && owner == me) {
		return true
	}
	for _, a := range chain {
		if identity.Normalize(a.Email) == me {
			return true
		}
	}
	return false
}

// Approvers devuelve la cadena del proyecto y si este usuario puede editarla.
func (s *Service) Approvers(projectID string) (*ApproversView, error) {
	p, err := s.st.GetProject(projectID)
	if err != nil || p == nil {
		return nil, errors.New("proyecto no encontrado")
	}
	s.RefreshReviews(projectID)
	p, _ = s.st.GetProject(projectID)
	chain, _ := s.st.ListApprovers(projectID)
	me := identity.Normalize(s.cfg.Email)
	return &ApproversView{
		Approvers: chain, Owner: p.Owner, Me: s.Identity(),
		CanEdit: canEditChain(me, identity.Normalize(p.Owner), chain),
	}, nil
}

// SetApprovers define el orden de aprobación del proyecto. Puede hacerlo el
// dueño o cualquiera de los aprobadores actuales (o cualquiera, si todavía no
// hay cadena). Queda registrado como un commit en main.
func (s *Service) SetApprovers(projectID string, in []vcs.Approver) (*ApproversView, error) {
	p, err := s.st.GetProject(projectID)
	if err != nil || p == nil {
		return nil, errors.New("proyecto no encontrado")
	}
	me := identity.Normalize(s.cfg.Email)
	if !identity.IsEmail(me) {
		return nil, errors.New("no pude identificarte: configura LCS_EMAIL")
	}
	if len(in) > maxApprovers {
		return nil, fmt.Errorf("máximo %d aprobadores", maxApprovers)
	}
	chain := make([]vcs.Approver, 0, len(in))
	seen := map[string]bool{}
	for _, a := range in {
		email := identity.Normalize(a.Email)
		if !identity.IsEmail(email) {
			return nil, fmt.Errorf("%q no es un correo válido", a.Email)
		}
		if seen[email] {
			return nil, fmt.Errorf("%s está repetido en la cadena", email)
		}
		seen[email] = true
		label := strings.TrimSpace(a.Label)
		if len([]rune(label)) > maxLabelLength {
			label = string([]rune(label)[:maxLabelLength])
		}
		chain = append(chain, vcs.Approver{Email: email, Label: label})
	}

	repo := s.repo(projectID)
	err = repo.WithLock(func() error {
		var last error
		for attempt := 0; attempt < 3; attempt++ {
			if err := repo.Pull(vcs.MainBranch); err != nil {
				return err
			}
			pf, err := repo.ReadProjectFile()
			if err != nil { // proyecto anterior a las aprobaciones
				pf = &vcs.ProjectFile{ID: p.ID, Name: p.Name, Description: p.Description, Parent: p.ParentID}
			}
			current := make([]store.Approver, 0, len(pf.Approvers))
			for _, a := range pf.Approvers {
				current = append(current, store.Approver{Email: identity.Normalize(a.Email)})
			}
			if !canEditChain(me, identity.Normalize(pf.Owner), current) {
				return errors.New("solo el dueño del proyecto o un aprobador puede cambiar la cadena")
			}
			if pf.Owner == "" {
				pf.Owner = me
			}
			pf.Approvers = chain
			_, _, err = repo.WriteFilesAndCommit(vcs.MainBranch,
				map[string][]byte{vcs.ProjectFilePath: pf.Marshal()}, nil,
				"LCS: definir aprobadores", me)
			if err == nil {
				return s.refreshProjectFile(projectID, repo)
			}
			last = err
		}
		return last
	})
	if err != nil {
		return nil, err
	}
	emails := make([]string, len(chain))
	for i, a := range chain {
		emails[i] = a.Email
	}
	_ = s.st.AddEvent(projectID, "", "project.approvers", me, map[string]any{"approvers": emails})
	return s.Approvers(projectID)
}

// ---------------------------------------------------------------- panel

type PanelItem struct {
	Branch  store.BranchRow `json:"branch"`
	Files   int             `json:"files"`
	// Changed: los planos nuevos de esta versión, para abrirlos y compararlos.
	Changed []ChangedFile `json:"changed"`
	Nodes   []Node          `json:"nodes"`
	MyIndex int             `json:"myIndex"`
	Next    int             `json:"next"`
}

type Panel struct {
	Me identity.Identity `json:"me"`
	// Pending: le toca decidir al usuario.
	Pending []PanelItem `json:"pending"`
	// Queued: está en la cadena pero esperan a un nodo anterior.
	Queued []PanelItem `json:"queued"`
	// Decided: ya aprobó esta versión; falta que sigan los demás o integrar.
	Decided []PanelItem `json:"decided"`
}

// ApprovalsPanel reúne las tareas de todos los proyectos donde el usuario es
// aprobador.
func (s *Service) ApprovalsPanel() (*Panel, error) {
	s.RefreshAllReviews(15 * time.Second)

	panel := &Panel{Me: s.Identity(), Pending: []PanelItem{}, Queued: []PanelItem{}, Decided: []PanelItem{}}
	me := identity.Normalize(s.cfg.Email)
	rows, err := s.st.ListAllBranches("active")
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		b := row.Branch
		st := s.reviewState(&b)
		idx := st.myIndex(me)
		if !st.Required || idx < 0 || !st.HasChanges {
			continue
		}
		files, _ := s.st.ListBranchFiles(b.ID)
		changed := 0
		for _, f := range files {
			if f.Changed {
				changed++
			}
		}
		item := PanelItem{Branch: row, Files: changed, Changed: s.changedFiles(&b), Nodes: st.Nodes, MyIndex: idx, Next: st.Next}
		switch {
		case st.Next == idx:
			panel.Pending = append(panel.Pending, item)
		case st.Nodes[idx].Status == NodeApproved:
			panel.Decided = append(panel.Decided, item)
		default:
			panel.Queued = append(panel.Queued, item)
		}
	}
	return panel, nil
}
