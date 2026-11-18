package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"lcs/internal/vcs"
)

// Un master recibe un plano de una zona y se abre una tarea sobre él: cada
// versión nueva que se sube tiene que contar como "cambios para aprobar".
func TestMasterUploadCountsAsChanges(t *testing.T) {
	svc := newTestService(t)
	master, _ := svc.CreateProject(CreateProjectInput{Name: "Planta"})
	zone, _ := svc.CreateProject(CreateProjectInput{Name: "Zona", ParentID: master.ID})
	if _, err := svc.Import(zone.ID, []IncomingFile{{Name: "a.dwg", Content: dwg("v1")}}, "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IntegrateZone(zone.ID, "", "t"); err != nil {
		t.Fatal(err)
	}
	ds, _ := svc.Store().ListDrawings(master.ID)
	b, err := svc.CreateBranch(CreateBranchInput{ProjectID: master.ID, Drawings: []string{ds[0].Path}, Title: "cambiar"})
	if err != nil {
		t.Fatal(err)
	}

	// Subir los mismos bytes: no hay nada que aprobar, y se dice por qué.
	if _, err := svc.Upload(b.ID, IncomingFile{Name: "a.dwg", Content: dwg("v1")}, "igual", "t"); err != nil {
		t.Fatal(err)
	}
	r, err := svc.Review(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.HasChanges || !r.Uploaded {
		t.Fatalf("idéntico: hasChanges=%v uploaded=%v", r.HasChanges, r.Uploaded)
	}

	for _, v := range []string{"v2", "v3"} {
		if _, err := svc.Upload(b.ID, IncomingFile{Name: "a.dwg", Content: dwg(v)}, "m", "t"); err != nil {
			t.Fatal(err)
		}
		r, err := svc.Review(b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !r.HasChanges {
			t.Fatalf("%s: la versión subida debería contar como cambio", v)
		}
	}

	// Aunque el índice local quede viejo, Review lo corrige desde git.
	svc.Store().SetBranchFiles(b.ID, nil)
	r, err = svc.Review(b.ID)
	if err != nil || !r.HasChanges {
		t.Fatalf("índice viejo: hasChanges=%v err=%v", r != nil && r.HasChanges, err)
	}
}

func TestDeleteProjectCascade(t *testing.T) {
	svc := newTestService(t)
	master, _ := svc.CreateProject(CreateProjectInput{Name: "Planta"})
	zone, _ := svc.CreateProject(CreateProjectInput{Name: "Zona", ParentID: master.ID})
	if _, err := svc.Import(zone.ID, []IncomingFile{{Name: "a.dwg", Content: dwg("v1")}}, "t"); err != nil {
		t.Fatal(err)
	}

	// Borrar solo la zona deja el master.
	res, err := svc.DeleteProject(zone.ID)
	if err != nil || len(res.Deleted) != 1 {
		t.Fatalf("borrar zona: %+v %v", res, err)
	}
	if p, _ := svc.Store().GetProject(zone.ID); p != nil {
		t.Fatal("la zona sigue en el índice")
	}
	// El índice se libera al instante; la carpeta del NAS se borra en segundo plano.
	gone := false
	for i := 0; i < 100 && !gone; i++ {
		_, err := os.Stat(filepath.Join(svc.Config().ProjectsDir(), zone.ID))
		gone = os.IsNotExist(err)
		if !gone {
			time.Sleep(30 * time.Millisecond)
		}
	}
	if !gone {
		t.Fatal("el repo de la zona sigue en la carpeta compartida")
	}
	if p, _ := svc.Store().GetProject(master.ID); p == nil {
		t.Fatal("el master no debía borrarse")
	}

	// Borrar el master arrastra las zonas.
	z2, _ := svc.CreateProject(CreateProjectInput{Name: "Zona 2", ParentID: master.ID})
	res, err = svc.DeleteProject(master.ID)
	if err != nil || len(res.Deleted) != 2 {
		t.Fatalf("borrar master: %+v %v", res, err)
	}
	for _, id := range []string{master.ID, z2.ID} {
		if p, _ := svc.Store().GetProject(id); p != nil {
			t.Fatalf("%s sigue en el índice", id)
		}
	}
}

func TestSnapshotsStayWithTask(t *testing.T) {
	svc := newTestService(t)
	p, _ := svc.CreateProject(CreateProjectInput{Name: "Planta"})
	if _, err := svc.Import(p.ID, []IncomingFile{{Name: "a.dwg", Content: dwg("v1")}}, "t"); err != nil {
		t.Fatal(err)
	}
	b, err := svc.CreateBranch(CreateBranchInput{ProjectID: p.ID, Drawings: []string{"drawings/a.dwg"}, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	treeBefore, _ := svc.Repo(p.ID).DrawingsTreeHash(b.Name)

	png := append([]byte{0x89}, []byte("PNG\r\n\x1a\n-recorte")...)
	sn, err := svc.AddSnapshot(b.ID, SnapshotInput{Caption: "zona de la prensa", PNG: png})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddSnapshot(b.ID, SnapshotInput{PNG: []byte("no es png")}); err == nil {
		t.Fatal("un archivo que no es PNG debería rechazarse")
	}
	list, err := svc.Snapshots(b.ID)
	if err != nil || len(list) != 1 || list[0].Caption != "zona de la prensa" || list[0].ID != sn.ID {
		t.Fatalf("recortes = %+v, %v", list, err)
	}
	rd, size, err := svc.SnapshotImage(b.ID, sn.ID)
	if err != nil || size != int64(len(png)) {
		t.Fatalf("imagen: size=%d err=%v", size, err)
	}
	rd.Close()
	if after, _ := svc.Repo(p.ID).DrawingsTreeHash(b.Name); after != treeBefore {
		t.Fatal("un recorte no debe cambiar drawings/ (invalidaría las aprobaciones)")
	}
}

func TestTasksTimelineAndSnapshotBounds(t *testing.T) {
	svc := newTestService(t)
	p, _ := svc.CreateProject(CreateProjectInput{Name: "Planta"})
	if _, err := svc.SetApprovers(p.ID, []vcs.Approver{{Email: "dev@lcs.local"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Import(p.ID, []IncomingFile{{Name: "a.dwg", Content: dwg("v1")}}, "t"); err != nil {
		t.Fatal(err)
	}
	b, err := svc.CreateBranch(CreateBranchInput{ProjectID: p.ID, Drawings: []string{"drawings/a.dwg"}, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := svc.TasksTimeline(nil, "all")
	if err != nil || len(rows) != 1 || rows[0].Stage != "open" {
		t.Fatalf("recién abierta: %+v %v", rows, err)
	}
	if _, err := svc.Upload(b.ID, IncomingFile{Name: "a.dwg", Content: dwg("v2")}, "m", "t"); err != nil {
		t.Fatal(err)
	}
	rows, _ = svc.TasksTimeline([]string{p.ID}, "all")
	if rows[0].Stage != "approving" || len(rows[0].Files) != 1 || rows[0].Files[0].Name != "a" {
		t.Fatalf("tras subir: stage=%s files=%+v", rows[0].Stage, rows[0].Files)
	}
	if other, _ := svc.TasksTimeline([]string{"no-existe"}, "all"); len(other) != 0 {
		t.Fatal("el filtro por proyecto no filtra")
	}
	// El panel de aprobaciones muestra los planos nuevos.
	panel, err := svc.ApprovalsPanel()
	if err != nil || len(panel.Pending) != 1 || len(panel.Pending[0].Changed) != 1 {
		t.Fatalf("panel: %+v %v", panel, err)
	}
	if _, err := svc.Decide(b.ID, "approved", ""); err != nil {
		t.Fatal(err)
	}
	if rows, _ = svc.TasksTimeline(nil, "all"); rows[0].Stage != "ready" {
		t.Fatalf("aprobada: stage=%s", rows[0].Stage)
	}

	png := append([]byte{0x89}, []byte("PNG\r\n\x1a\n-x")...)
	bounds := &Bounds{MinX: 10, MinY: 20, MaxX: 110, MaxY: 80}
	if _, err := svc.AddSnapshot(b.ID, SnapshotInput{PNG: png, Bounds: bounds}); err != nil {
		t.Fatal(err)
	}
	list, _ := svc.Snapshots(b.ID)
	if len(list) != 1 || list[0].Bounds == nil || list[0].Bounds.MaxX != 110 {
		t.Fatalf("bounds perdidos: %+v", list)
	}
}
