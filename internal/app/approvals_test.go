package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"lcs/internal/vcs"
)

// Tres equipos distintos sobre la misma carpeta compartida: ana crea el
// proyecto y trabaja, beto y carla aprueban en ese orden desde sus equipos.
func TestApprovalChainAcrossMachines(t *testing.T) {
	t.Setenv("PATH", "") // sin git: nada puede lanzar procesos

	root := t.TempDir()
	nas := filepath.Join(root, "nas")
	ana := newMachine(t, nas, filepath.Join(root, "ana"), "ana@empresa.com")
	beto := newMachine(t, nas, filepath.Join(root, "beto"), "beto@empresa.com")
	carla := newMachine(t, nas, filepath.Join(root, "carla"), "carla@empresa.com")

	// --- ana: proyecto, un plano, una tarea con cambios
	p, err := ana.CreateProject(CreateProjectInput{Name: "Nave A", Description: "Producción"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ana.Import(p.ID, []IncomingFile{{Name: "prensa.dwg", Content: dwg("v0")}}, "ana"); err != nil {
		t.Fatal(err)
	}
	plan := vcs.DrawingsDir + "/prensa.dwg"
	task, err := ana.CreateBranch(CreateBranchInput{ProjectID: p.ID, Drawings: []string{plan}, Title: "Mover prensa"})
	if err != nil {
		t.Fatal(err)
	}
	if task.AuthorEmail != "ana@empresa.com" {
		t.Fatalf("authorEmail = %q", task.AuthorEmail)
	}
	upload := func(content string) {
		t.Helper()
		if _, err := ana.Upload(task.ID, IncomingFile{Name: "prensa.dwg", Content: dwg(content)}, "versión "+content, "ana"); err != nil {
			t.Fatalf("upload %s: %v", content, err)
		}
	}
	upload("v1")

	// Sin aprobadores definidos no hay compuerta (comportamiento de siempre).
	if v, err := ana.Review(task.ID); err != nil || v.Required || !v.Complete {
		t.Fatalf("sin cadena: %+v %v", v, err)
	}

	// --- ana define el orden: beto primero, carla después
	chain := []vcs.Approver{{Email: "Beto@Empresa.com", Label: "Producción"}, {Email: "carla@empresa.com", Label: "Seguridad"}}
	view, err := ana.SetApprovers(p.ID, chain)
	if err != nil {
		t.Fatalf("SetApprovers: %v", err)
	}
	if len(view.Approvers) != 2 || view.Approvers[0].Email != "beto@empresa.com" || view.Owner != "ana@empresa.com" {
		t.Fatalf("cadena = %+v owner=%q", view.Approvers, view.Owner)
	}

	// Integrar sin aprobaciones falla, con la lista de lo que falta.
	_, err = ana.Merge(task.ID, "", "ana")
	var need *ErrNeedsApproval
	if !errors.As(err, &need) || !strings.Contains(err.Error(), "beto@empresa.com") {
		t.Fatalf("merge sin aprobar: %v", err)
	}

	// --- los otros equipos descubren el proyecto en la carpeta compartida
	if n := beto.Discover(false); n != 1 {
		t.Fatalf("beto descubrió %d proyectos", n)
	}
	if n := carla.Discover(false); n != 1 {
		t.Fatalf("carla descubrió %d proyectos", n)
	}
	for _, m := range []*Service{beto, carla} {
		if err := m.Sync(p.ID); err != nil {
			t.Fatalf("sync: %v", err)
		}
	}
	bv, err := beto.Review(task.ID)
	if err != nil {
		t.Fatalf("beto ve la tarea: %v", err)
	}
	if !bv.Required || bv.MyIndex != 0 || !bv.CanAct || len(bv.Nodes) != 2 {
		t.Fatalf("vista de beto: %+v", bv)
	}

	// carla no puede saltarse a beto.
	if _, err := carla.Decide(task.ID, "approved", ""); err == nil || !strings.Contains(err.Error(), "no es tu turno") {
		t.Fatalf("carla fuera de orden: %v", err)
	}
	// quien no está en la cadena tampoco decide.
	ajeno := newMachine(t, nas, filepath.Join(root, "ajeno"), "ajeno@empresa.com")
	ajeno.Discover(false)
	_ = ajeno.Sync(p.ID)
	if _, err := ajeno.Decide(task.ID, "approved", ""); err == nil || !strings.Contains(err.Error(), "no está en la cadena") {
		t.Fatalf("ajeno: %v", err)
	}

	revsBefore, _ := ana.Store().ListRevisions(p.ID, "", 400)

	// beto aprueba; carla ya puede.
	if _, err := beto.Decide(task.ID, "approved", "ok producción"); err != nil {
		t.Fatalf("beto aprueba: %v", err)
	}
	if err := carla.Sync(p.ID); err != nil {
		t.Fatal(err)
	}
	cv, err := carla.Review(task.ID)
	if err != nil || !cv.CanAct || cv.Nodes[0].Status != NodeApproved {
		t.Fatalf("vista de carla tras beto: %+v %v", cv, err)
	}

	// carla rechaza (con motivo obligatorio): bloquea la integración.
	if _, err := carla.Decide(task.ID, "rejected", ""); err == nil {
		t.Fatal("rechazar sin motivo debería fallar")
	}
	if _, err := carla.Decide(task.ID, "rejected", "falta la salida de emergencia"); err != nil {
		t.Fatalf("carla rechaza: %v", err)
	}
	if _, err := ana.Merge(task.ID, "", "ana"); !errors.As(err, &need) {
		t.Fatalf("merge con rechazo: %v", err)
	}

	// ana corrige y sube otra versión: TODAS las decisiones quedan obsoletas.
	upload("v2")
	av, err := ana.Review(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if av.Complete || av.Nodes[0].Status != NodePending || !av.Nodes[0].Stale || av.Next != 0 {
		t.Fatalf("tras subir v2: %v next=%d", statuses(av.ReviewState), av.Next)
	}

	// Resubir EXACTAMENTE los mismos bytes no reinicia nada: el contenido es el mismo.
	if _, err := beto.Decide(task.ID, "approved", "v2 bien"); err != nil {
		t.Fatalf("beto aprueba v2: %v", err)
	}
	upload("v2")
	if av, _ = ana.Review(task.ID); av.Nodes[0].Status != NodeApproved {
		t.Fatalf("resubir lo mismo no debería invalidar: %v", statuses(av.ReviewState))
	}

	if _, err := carla.Decide(task.ID, "approved", "ahora sí"); err != nil {
		t.Fatalf("carla aprueba: %v", err)
	}

	// Las decisiones son commits en la rama, pero no son revisiones de planos.
	revsAfter, _ := ana.Store().ListRevisions(p.ID, "", 400)
	uploads := 0
	for _, r := range revsAfter {
		if r.BranchID == task.ID {
			uploads++
		}
	}
	_ = revsBefore
	if uploads != 3 { // v1, v2, v2 repetido
		t.Fatalf("revisiones de la tarea = %d, esperaba 3 (las aprobaciones no cuentan)", uploads)
	}

	// --- ana integra: ahora sí. Decide con lo que hay en la carpeta compartida,
	// sin haber sincronizado antes.
	if _, err := ana.Merge(task.ID, "", "ana"); err != nil {
		t.Fatalf("merge aprobado: %v", err)
	}
	got, err := ana.DrawingBytes(p.ID, plan, "")
	if err != nil || string(got) != string(dwg("v2")) {
		t.Fatalf("main quedó con %q, %v", got, err)
	}
	// Las aprobaciones nunca llegan a main.
	if files, _ := ana.Repo(p.ID).ListFilesAt(vcs.MainBranch, vcs.ApprovalsDir); len(files) != 0 {
		t.Fatalf("main no debe llevar aprobaciones: %d archivos", len(files))
	}

	// --- los demás equipos se enteran de que se integró (sin que nadie se lo diga).
	if err := beto.Sync(p.ID); err != nil {
		t.Fatal(err)
	}
	if b, _ := beto.Store().GetBranch(task.ID); b == nil || b.Status != "merged" {
		t.Fatalf("beto debería ver la tarea integrada: %+v", b)
	}
	if v, _ := beto.Review(task.ID); v == nil || v.CanAct {
		t.Fatal("una tarea integrada no admite más decisiones")
	}
	if _, err := beto.Decide(task.ID, "approved", ""); err == nil {
		t.Fatal("decidir sobre una tarea cerrada debería fallar")
	}
	tl := beto.mustTimeline(t, task.ID)
	kinds := []string{}
	for _, it := range tl {
		kinds = append(kinds, it.Kind)
	}
	// "Abrió la tarea" va primero aunque beto la descubriera mucho después.
	if len(tl) == 0 || tl[0].Kind != "opened" {
		t.Fatalf("la línea de tiempo debe empezar con la apertura: %v", kinds)
	}
	if b, _ := beto.Store().GetBranch(task.ID); b == nil || b.CreatedAt != task.CreatedAt {
		t.Fatalf("beto debería conservar la fecha real de apertura: %v vs %v", b, task.CreatedAt)
	}
	for _, want := range []string{"opened", "upload", "approved", "rejected", "merged"} {
		found := false
		for _, k := range kinds {
			found = found || k == want
		}
		if !found {
			t.Fatalf("la línea de tiempo no tiene %q: %v", want, kinds)
		}
	}
}

func (s *Service) mustTimeline(t *testing.T, branchID string) []TimelineItem {
	t.Helper()
	v, err := s.Review(branchID)
	if err != nil {
		t.Fatal(err)
	}
	return v.Timeline
}

func TestSetApproversPermissionsAndValidation(t *testing.T) {
	t.Setenv("PATH", "")
	root := t.TempDir()
	nas := filepath.Join(root, "nas")
	ana := newMachine(t, nas, filepath.Join(root, "ana"), "ana@empresa.com")
	luis := newMachine(t, nas, filepath.Join(root, "luis"), "luis@empresa.com")

	p, err := ana.CreateProject(CreateProjectInput{Name: "Permisos"})
	if err != nil {
		t.Fatal(err)
	}
	bad := [][]vcs.Approver{
		{{Email: "no-es-correo"}},
		{{Email: "a@x.com"}, {Email: "A@x.com"}},
	}
	for _, c := range bad {
		if _, err := ana.SetApprovers(p.ID, c); err == nil {
			t.Fatalf("debería rechazar %+v", c)
		}
	}
	if _, err := ana.SetApprovers(p.ID, []vcs.Approver{{Email: "beto@empresa.com"}}); err != nil {
		t.Fatal(err)
	}

	// luis no es dueño ni aprobador: no puede cambiar la cadena.
	luis.Discover(false)
	if err := luis.Sync(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := luis.SetApprovers(p.ID, []vcs.Approver{{Email: "luis@empresa.com"}}); err == nil {
		t.Fatal("luis no debería poder redefinir la cadena")
	}
	if v, _ := luis.Approvers(p.ID); v == nil || v.CanEdit || len(v.Approvers) != 1 {
		t.Fatalf("vista de luis: %+v", v)
	}
	// El dueño sí puede, y el cambio llega a los demás equipos.
	if _, err := ana.SetApprovers(p.ID, []vcs.Approver{{Email: "beto@empresa.com"}, {Email: "luis@empresa.com"}}); err != nil {
		t.Fatal(err)
	}
	luis.RefreshReviews(p.ID)
	if v, _ := luis.Approvers(p.ID); v == nil || !v.CanEdit || len(v.Approvers) != 2 {
		t.Fatalf("luis ya es aprobador: %+v", v)
	}
}

// Dos personas abren tareas sobre el mismo plano desde equipos distintos: cada
// una debe enterarse de la otra.
func TestConcurrentEditWarning(t *testing.T) {
	t.Setenv("PATH", "")
	root := t.TempDir()
	nas := filepath.Join(root, "nas")
	ana := newMachine(t, nas, filepath.Join(root, "ana"), "ana@empresa.com")
	beto := newMachine(t, nas, filepath.Join(root, "beto"), "beto@empresa.com")

	p, _ := ana.CreateProject(CreateProjectInput{Name: "Concurrencia"})
	if _, err := ana.Import(p.ID, []IncomingFile{{Name: "a.dwg", Content: dwg("a")}, {Name: "b.dwg", Content: dwg("b")}}, "ana"); err != nil {
		t.Fatal(err)
	}
	pa, pb := vcs.DrawingsDir+"/a.dwg", vcs.DrawingsDir+"/b.dwg"

	t1, err := ana.CreateBranch(CreateBranchInput{ProjectID: p.ID, Drawings: []string{pa}, Title: "Tarea de ana"})
	if err != nil {
		t.Fatal(err)
	}
	beto.Discover(false)
	if err := beto.Sync(p.ID); err != nil {
		t.Fatal(err)
	}

	// beto, antes de abrir su tarea, consulta quién edita qué (datos frescos).
	ed := beto.Editing(p.ID, true)
	if len(ed[pa]) != 1 || ed[pa][0].AuthorEmail != "ana@empresa.com" || len(ed[pb]) != 0 {
		t.Fatalf("editing de beto = %+v", ed)
	}

	t2, err := beto.CreateBranch(CreateBranchInput{ProjectID: p.ID, Drawings: []string{pa, pb}, Title: "Tarea de beto"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := ana.Review(t1.ID) // ana se entera sin sincronizar a mano
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Overlaps) != 1 || v.Overlaps[0].BranchID != t2.ID || v.Overlaps[0].AuthorEmail != "beto@empresa.com" ||
		len(v.Overlaps[0].Paths) != 1 || v.Overlaps[0].Paths[0] != pa {
		t.Fatalf("overlaps de ana = %+v", v.Overlaps)
	}

	// Descartar la tarea de beto la saca de la lista en el equipo de ana.
	if err := beto.CloseBranch(t2.ID, "beto"); err != nil {
		t.Fatal(err)
	}
	if v, _ = ana.Review(t1.ID); len(v.Overlaps) != 0 {
		t.Fatalf("la tarea descartada sigue figurando: %+v", v.Overlaps)
	}
}
