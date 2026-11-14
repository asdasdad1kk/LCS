package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"lcs/internal/app"
	"lcs/internal/store"
	"lcs/internal/vcs"
)

type API struct {
	svc *app.Service
}

func New(svc *app.Service) *API { return &API{svc: svc} }

type H func(w http.ResponseWriter, r *http.Request) error

func (a *API) Routes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/bootstrap", a.wrap(a.bootstrap))
	mux.HandleFunc("GET /api/stats", a.wrap(a.stats))
	mux.HandleFunc("GET /api/activity", a.wrap(a.activity))

	mux.HandleFunc("GET /api/projects", a.wrap(a.listProjects))
	mux.HandleFunc("POST /api/projects", a.wrap(a.createProject))
	mux.HandleFunc("GET /api/projects/{id}", a.wrap(a.getProject))
	mux.HandleFunc("POST /api/projects/{id}/sync", a.wrap(a.syncProject))
	mux.HandleFunc("POST /api/projects/{id}/integrate", a.wrap(a.integrateProject))
	mux.HandleFunc("GET /api/projects/{id}/approvers", a.wrap(a.getApprovers))
	mux.HandleFunc("PUT /api/projects/{id}/approvers", a.wrap(a.putApprovers))
	mux.HandleFunc("GET /api/projects/{id}/editing", a.wrap(a.editing))
	mux.HandleFunc("GET /api/me", a.wrap(a.me))
	mux.HandleFunc("GET /api/approvals", a.wrap(a.approvalsPanel))
	mux.HandleFunc("GET /api/branches/{id}/review", a.wrap(a.review))
	mux.HandleFunc("POST /api/branches/{id}/approve", a.wrap(a.approve))
	mux.HandleFunc("POST /api/branches/{id}/reject", a.wrap(a.reject))
	mux.HandleFunc("POST /api/projects/{id}/import", a.wrap(a.importDrawings))
	mux.HandleFunc("GET /api/projects/{id}/branches", a.wrap(a.listBranches))
	mux.HandleFunc("POST /api/projects/{id}/branches", a.wrap(a.createBranch))

	mux.HandleFunc("GET /api/drawings/{id}", a.wrap(a.getDrawing))
	mux.HandleFunc("GET /api/drawings/{id}/file", a.wrap(a.getDrawingFile))
	mux.HandleFunc("GET /api/drawings/{id}/revisions", a.wrap(a.listRevisions))
	mux.HandleFunc("PUT /api/drawings/{id}/layouts", a.wrap(a.putLayouts))
	mux.HandleFunc("POST /api/drawings/{id}/restore", a.wrap(a.restoreDrawing))

	mux.HandleFunc("GET /api/branches", a.wrap(a.allBranches))
	mux.HandleFunc("GET /api/branches/{id}", a.wrap(a.getBranch))
	mux.HandleFunc("POST /api/branches/{id}/upload", a.wrap(a.upload))
	mux.HandleFunc("GET /api/branches/{id}/download", a.wrap(a.download))
	mux.HandleFunc("POST /api/branches/{id}/merge", a.wrap(a.merge))
	mux.HandleFunc("POST /api/branches/{id}/close", a.wrap(a.closeBranch))

	return mux
}

func (a *API) wrap(h H) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			var conflict *vcs.ErrConflict
			var needs *app.ErrNeedsApproval
			switch {
			case errors.As(err, &needs):
				writeJSON(w, http.StatusConflict, map[string]any{
					"error":         needs.Error(),
					"needsApproval": true,
					"nodes":         needs.State.Nodes,
				})
			case errors.As(err, &conflict):
				writeJSON(w, http.StatusConflict, map[string]any{
					"error":  "conflicto de revisión",
					"paths":  conflict.Paths,
					"detail": "main cambió el mismo plano desde que se abrió la rama",
				})
			default:
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	return dec.Decode(v)
}

// ---------------------------------------------------------------- meta

func (a *API) bootstrap(w http.ResponseWriter, r *http.Request) error {
	stats, err := a.svc.Stats()
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{
		"app":      "LCS",
		"version":  "0.1.0",
		"actor":    a.svc.Config().Actor,
		"me":       a.svc.Identity(),
		"localUrl": strings.TrimSuffix(a.svc.Config().LocalURL(), "/"),
		"config":   stats,
	})
	return nil
}

func (a *API) stats(w http.ResponseWriter, r *http.Request) error {
	stats, err := a.svc.Stats()
	if err != nil {
		return err
	}
	writeJSON(w, 200, stats)
	return nil
}

func (a *API) activity(w http.ResponseWriter, r *http.Request) error {
	projectID := r.URL.Query().Get("project")
	events, err := a.svc.Store().ListEvents(projectID, 60)
	if err != nil {
		return err
	}
	writeJSON(w, 200, events)
	return nil
}

// ---------------------------------------------------------------- projects

func (a *API) listProjects(w http.ResponseWriter, r *http.Request) error {
	// Proyectos que otras personas crearon en la carpeta compartida.
	a.svc.MaybeDiscover(30 * time.Second)
	projects, err := a.svc.Store().ListProjects()
	if err != nil {
		return err
	}
	type projectCard struct {
		store.Project
		Drawings int `json:"drawings"`
		Open     int `json:"openBranches"`
		// Zonas: cuántos planos tiene la zona sin copiar al master, y para un
		// master cuántas de sus zonas están atrasadas.
		PendingChanges int `json:"pendingChanges"`
		Zones          int `json:"zones"`
		PendingZones   int `json:"pendingZones"`
	}
	out := make([]projectCard, 0, len(projects))
	index := map[string]int{}
	for _, p := range projects {
		ds, _ := a.svc.Store().ListDrawings(p.ID)
		bs, _ := a.svc.Store().ListBranches(p.ID, "active")
		card := projectCard{Project: p, Drawings: len(ds), Open: len(bs)}
		if p.ParentID != "" {
			if ch, err := a.svc.ZoneChanges(&p); err == nil {
				card.PendingChanges = ch.Count()
			}
		}
		index[p.ID] = len(out)
		out = append(out, card)
	}
	for _, c := range out {
		if i, ok := index[c.ParentID]; ok && c.ParentID != "" {
			out[i].Zones++
			if c.PendingChanges > 0 {
				out[i].PendingZones++
			}
		}
	}
	writeJSON(w, 200, out)
	return nil
}

func (a *API) createProject(w http.ResponseWriter, r *http.Request) error {
	var in app.CreateProjectInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	p, err := a.svc.CreateProject(in)
	if err != nil {
		return err
	}

	if seed := a.svc.Config().SeedDir; seed != "" {
		_, _ = a.svc.Seed(p.ID, seed)
		_ = a.svc.Sync(p.ID)
	}
	writeJSON(w, 201, p)
	return nil
}

func (a *API) getProject(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	p, err := a.svc.Store().GetProject(id)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("proyecto no encontrado")
	}
	drawings, err := a.svc.Store().ListDrawings(id)
	if err != nil {
		return err
	}
	branches, err := a.svc.Store().ListBranches(id, "all")
	if err != nil {
		return err
	}
	events, _ := a.svc.Store().ListEvents(id, 20)

	repo := a.svc.Repo(id)
	mainHead, _ := repo.HeadOf(vcs.MainBranch)

	repoBytes, cloneBytes, _ := a.svc.Usage(id)

	// Master ↔ zonas.
	type zoneCard struct {
		store.Project
		Drawings       int `json:"drawings"`
		PendingChanges int `json:"pendingChanges"`
	}
	var parent *store.Project
	children := []zoneCard{}
	var integration any
	if p.ParentID != "" {
		parent, _ = a.svc.Store().GetProject(p.ParentID)
		ch, _ := a.svc.ZoneChanges(p)
		integration = map[string]any{
			"changed":      nonNil(ch.Changed),
			"removed":      nonNil(ch.Removed),
			"integratedAt": p.IntegratedAt,
		}
	} else {
		zones, _ := a.svc.Store().ListChildren(id)
		for _, z := range zones {
			zd, _ := a.svc.Store().ListDrawings(z.ID)
			ch, _ := a.svc.ZoneChanges(&z)
			children = append(children, zoneCard{Project: z, Drawings: len(zd), PendingChanges: ch.Count()})
		}
	}

	writeJSON(w, 200, map[string]any{
		"project":     p,
		"parent":      parent,
		"children":    children,
		"integration": integration,
		"editing":     a.svc.Editing(id, false),
		"drawings":    drawings,
		"branches":    branches,
		"events":      events,
		"mainHead":    mainHead,
		"workspace":   a.svc.Config().Workspace,
		"usage": map[string]int64{
			"repo":  repoBytes,
			"clone": cloneBytes,
		},
	})
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (a *API) integrateProject(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Strategy string `json:"strategy"`
	}
	_ = readJSON(r, &body)
	res, err := a.svc.IntegrateZone(r.PathValue("id"), body.Strategy, a.svc.Config().Actor)
	if err != nil {
		return err
	}
	writeJSON(w, 200, res)
	return nil
}

func (a *API) syncProject(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if err := a.svc.Sync(id); err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"ok": true})
	return nil
}

func (a *API) importDrawings(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return fmt.Errorf("no pude leer el formulario: %w", err)
	}
	files := []app.IncomingFile{}
	for _, fh := range r.MultipartForm.File["file"] {
		f, err := fh.Open()
		if err != nil {
			return err
		}
		content, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return err
		}
		files = append(files, app.IncomingFile{Name: fh.Filename, Content: content})
	}
	if len(files) == 0 {
		return errors.New("no se recibió ningún archivo .dwg")
	}
	blobs, err := a.svc.Import(id, files, a.svc.Config().Actor)
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"ok": true, "blobs": blobs, "count": len(files)})
	return nil
}

func (a *API) listBranches(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	status := r.URL.Query().Get("status")
	branches, err := a.svc.Store().ListBranches(id, status)
	if err != nil {
		return err
	}
	type branchCard struct {
		store.Branch
		Files []store.BranchFile `json:"files"`
	}
	out := []branchCard{}
	for _, b := range branches {
		files, _ := a.svc.Store().ListBranchFiles(b.ID)
		out = append(out, branchCard{Branch: b, Files: files})
	}
	writeJSON(w, 200, out)
	return nil
}

func (a *API) createBranch(w http.ResponseWriter, r *http.Request) error {
	var in app.CreateBranchInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	in.ProjectID = r.PathValue("id")
	if in.Author == "" {
		in.Author = a.svc.Config().Actor
	}
	b, err := a.svc.CreateBranch(in)
	if err != nil {
		return err
	}
	writeJSON(w, 201, b)
	return nil
}

// ---------------------------------------------------------------- drawings

func (a *API) getDrawing(w http.ResponseWriter, r *http.Request) error {
	d, err := a.svc.Store().GetDrawing(r.PathValue("id"))
	if err != nil {
		return err
	}
	if d == nil {
		return errors.New("plano no encontrado")
	}
	revs, _ := a.svc.Store().ListRevisions(d.ProjectID, d.Path, 30)
	branches, _ := a.svc.Store().ListBranches(d.ProjectID, "active")
	related := []store.Branch{}
	for _, b := range branches {
		files, _ := a.svc.Store().ListBranchFiles(b.ID)
		for _, f := range files {
			if f.Path == d.Path {
				related = append(related, b)
				break
			}
		}
	}
	writeJSON(w, 200, map[string]any{
		"drawing":   d,
		"layouts":   json.RawMessage(d.LayoutsJSON),
		"meta":      json.RawMessage(d.MetaJSON),
		"revisions": revs,
		"branches":  related,
	})
	return nil
}

func (a *API) getDrawingFile(w http.ResponseWriter, r *http.Request) error {
	d, err := a.svc.Store().GetDrawing(r.PathValue("id"))
	if err != nil {
		return err
	}
	if d == nil {
		return errors.New("plano no encontrado")
	}
	ref := r.URL.Query().Get("ref")
	if ref == "" {
		ref = vcs.MainBranch
	}
	rd, size, err := a.svc.DrawingReader(d.ProjectID, d.Path, ref)
	if err != nil {
		return fmt.Errorf("no pude leer el plano en %s: %w", ref, err)
	}
	defer rd.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": path.Base(d.Path)}))
	w.Header().Set("Cache-Control", "no-store")
	if size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	_, err = io.Copy(w, rd)
	return err
}

func (a *API) listRevisions(w http.ResponseWriter, r *http.Request) error {
	d, err := a.svc.Store().GetDrawing(r.PathValue("id"))
	if err != nil {
		return err
	}
	if d == nil {
		return errors.New("plano no encontrado")
	}
	revs, err := a.svc.Store().ListRevisions(d.ProjectID, d.Path, 60)
	if err != nil {
		return err
	}
	writeJSON(w, 200, revs)
	return nil
}

func (a *API) restoreDrawing(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Commit string `json:"commit"`
	}
	if err := readJSON(r, &body); err != nil {
		return err
	}
	d, err := a.svc.RestoreDrawing(r.PathValue("id"), body.Commit, a.svc.Config().Actor)
	if err != nil {
		return err
	}
	writeJSON(w, 200, d)
	return nil
}

func (a *API) putLayouts(w http.ResponseWriter, r *http.Request) error {
	d, err := a.svc.Store().GetDrawing(r.PathValue("id"))
	if err != nil || d == nil {
		return errors.New("plano no encontrado")
	}
	var body struct {
		Layouts json.RawMessage `json:"layouts"`
		Meta    json.RawMessage `json:"meta"`
	}
	if err := readJSON(r, &body); err != nil {
		return err
	}
	layouts := "[]"
	if len(body.Layouts) > 0 {
		layouts = string(body.Layouts)
	}
	meta := "{}"
	if len(body.Meta) > 0 {
		meta = string(body.Meta)
	}
	if err := a.svc.Store().SetDrawingLayouts(d.ID, layouts, meta); err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"ok": true})
	return nil
}

// ---------------------------------------------------------------- branches

func (a *API) getBranch(w http.ResponseWriter, r *http.Request) error {
	view, err := a.svc.BranchView(r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, view)
	return nil
}

func (a *API) allBranches(w http.ResponseWriter, r *http.Request) error {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "active"
	}
	rows, err := a.svc.Store().ListAllBranches(status)
	if err != nil {
		return err
	}
	type card struct {
		store.Branch
		ProjectName string             `json:"projectName"`
		Files       []store.BranchFile `json:"files"`
	}
	out := []card{}
	for _, b := range rows {
		files, _ := a.svc.Store().ListBranchFiles(b.ID)
		out = append(out, card{Branch: b.Branch, ProjectName: b.ProjectName, Files: files})
	}
	writeJSON(w, 200, out)
	return nil
}

// maxUpload es el techo duro para una subida: planos grandes rondan los 150 MB.
const maxUpload = 2 << 30

func (a *API) upload(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	file, err := readUploadedFile(r)
	if err != nil {
		return err
	}
	actor := a.svc.Config().Actor
	if v := r.FormValue("actor"); strings.TrimSpace(v) != "" {
		actor = v
	}
	b, err := a.svc.Upload(r.PathValue("id"), file, r.FormValue("message"), actor)
	if err != nil {
		return err
	}
	writeJSON(w, 200, b)
	return nil
}

func (a *API) download(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	name, err := a.svc.BundleName(id)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Cache-Control", "no-store")
	// El ZIP se escribe directo al socket: sin Content-Length ni buffer en RAM.
	return a.svc.WriteBundle(w, id)
}

func (a *API) merge(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Strategy string `json:"strategy"`
	}
	_ = readJSON(r, &body)
	res, err := a.svc.Merge(r.PathValue("id"), body.Strategy, a.svc.Config().Actor)
	if err != nil {
		return err
	}
	writeJSON(w, 200, res)
	return nil
}

func (a *API) closeBranch(w http.ResponseWriter, r *http.Request) error {
	if err := a.svc.CloseBranch(r.PathValue("id"), a.svc.Config().Actor); err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"ok": true})
	return nil
}

func readUploadedFile(r *http.Request) (app.IncomingFile, error) {
	// 8 MB en memoria y el resto a un temporal: así un DWG de 150 MB no
	// duplica el archivo en RAM al parsear el formulario.
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return app.IncomingFile{}, fmt.Errorf("formulario inválido: %w", err)
	}
	for _, key := range []string{"file", "dwg"} {
		if fhs := r.MultipartForm.File[key]; len(fhs) > 0 {
			fh := fhs[0]
			f, err := fh.Open()
			if err != nil {
				return app.IncomingFile{}, err
			}
			defer f.Close()
			content, err := io.ReadAll(f)
			if err != nil {
				return app.IncomingFile{}, err
			}
			return app.IncomingFile{Name: fh.Filename, Content: content}, nil
		}
	}
	return app.IncomingFile{}, errors.New("falta el archivo DWG")
}

// ---------------------------------------------------------------- identidad y aprobaciones

func (a *API) me(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, 200, a.svc.Identity())
	return nil
}

func (a *API) getApprovers(w http.ResponseWriter, r *http.Request) error {
	v, err := a.svc.Approvers(r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, v)
	return nil
}

func (a *API) putApprovers(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Approvers []vcs.Approver `json:"approvers"`
	}
	if err := readJSON(r, &body); err != nil {
		return err
	}
	v, err := a.svc.SetApprovers(r.PathValue("id"), body.Approvers)
	if err != nil {
		return err
	}
	writeJSON(w, 200, v)
	return nil
}

// editing dice qué planos tiene alguien en edición. Consulta la carpeta
// compartida, así que la pantalla solo la pide al abrir el diálogo de tarea.
func (a *API) editing(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, 200, a.svc.Editing(r.PathValue("id"), true))
	return nil
}

func (a *API) approvalsPanel(w http.ResponseWriter, r *http.Request) error {
	panel, err := a.svc.ApprovalsPanel()
	if err != nil {
		return err
	}
	writeJSON(w, 200, panel)
	return nil
}

func (a *API) review(w http.ResponseWriter, r *http.Request) error {
	v, err := a.svc.Review(r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, v)
	return nil
}

func (a *API) decide(w http.ResponseWriter, r *http.Request, decision string) error {
	var body struct {
		Comment string `json:"comment"`
	}
	_ = readJSON(r, &body)
	v, err := a.svc.Decide(r.PathValue("id"), decision, body.Comment)
	if err != nil {
		return err
	}
	writeJSON(w, 200, v)
	return nil
}

func (a *API) approve(w http.ResponseWriter, r *http.Request) error {
	return a.decide(w, r, "approved")
}
func (a *API) reject(w http.ResponseWriter, r *http.Request) error { return a.decide(w, r, "rejected") }
