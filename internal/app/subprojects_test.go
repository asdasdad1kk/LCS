package app

import (
	"errors"
	"path/filepath"
	"testing"

	"lcs/internal/config"
	"lcs/internal/store"
	"lcs/internal/vcs"
)

func dwg(tag string) []byte { return []byte("AC1032" + tag) }

func newTestService(t *testing.T) *Service {
	t.Helper()
	// Sin git en el PATH: si algo intenta lanzar un proceso, el test falla.
	t.Setenv("PATH", "")
	root := t.TempDir()
	return newMachine(t, filepath.Join(root, "nas"), filepath.Join(root, "data"), "dev@lcs.local")
}

// newMachine simula un equipo: base de datos y clones propios (dataDir) sobre
// la misma carpeta compartida (workspace). Dos máquinas con el mismo workspace
// y distinto dataDir se comportan como dos personas en el mismo NAS.
func newMachine(t *testing.T, workspace, dataDir, email string) *Service {
	t.Helper()
	cfg := &config.Config{
		Workspace:     workspace,
		DataDir:       dataDir,
		Actor:         "tester",
		Email:         email,
		EmailVerified: true,
		EmailSource:   "config",
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(cfg, st)
}

// Los IDs de planos planos (sin carpeta) no pueden cambiar: la migración a
// rutas anidadas se apoya en eso para no romper URLs ni filas existentes.
func TestFlatDrawingIDsUnchanged(t *testing.T) {
	svc := newTestService(t)
	p, err := svc.CreateProject(CreateProjectInput{Name: "Plano"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Import(p.ID, []IncomingFile{{Name: "Foo Bar.dwg", Content: dwg("x")}}, "tester"); err != nil {
		t.Fatal(err)
	}
	ds, err := svc.Store().ListDrawings(p.ID)
	if err != nil || len(ds) != 1 {
		t.Fatalf("planos = %d, %v", len(ds), err)
	}
	if want := p.ID + "--foo-bar"; ds[0].ID != want || ds[0].Name != "Foo_Bar" {
		t.Fatalf("id=%q name=%q, quería %q / Foo_Bar", ds[0].ID, ds[0].Name, want)
	}
}

func TestZoneIntegration(t *testing.T) {
	svc := newTestService(t)

	master, err := svc.CreateProject(CreateProjectInput{Name: "Planta Master"})
	if err != nil {
		t.Fatal(err)
	}
	zone, err := svc.CreateProject(CreateProjectInput{Name: "Zona Norte", ParentID: master.ID})
	if err != nil {
		t.Fatal(err)
	}
	if zone.ParentID != master.ID {
		t.Fatalf("parentId = %q", zone.ParentID)
	}
	if _, err := svc.CreateProject(CreateProjectInput{Name: "Sub de sub", ParentID: zone.ID}); err == nil {
		t.Fatal("un subproyecto no debería aceptar hijos")
	}
	if _, err := svc.CreateProject(CreateProjectInput{Name: "Huérfano", ParentID: "no-existe"}); err == nil {
		t.Fatal("el master inexistente debería rechazarse")
	}

	zoneFile := vcs.DrawingsDir + "/norte.dwg"
	masterFile := MasterPath(zone.ID, zoneFile)

	reload := func() *store.Project {
		p, err := svc.Store().GetProject(zone.ID)
		if err != nil || p == nil {
			t.Fatalf("GetProject: %v", err)
		}
		return p
	}
	pending := func() ZoneChanges {
		c, err := svc.ZoneChanges(reload())
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	// Zona recién creada, sin planos: nada que integrar.
	if n := pending().Count(); n != 0 {
		t.Fatalf("zona vacía con %d pendientes", n)
	}
	if _, err := svc.IntegrateZone(zone.ID, "", "tester"); err == nil {
		t.Fatal("integrar sin cambios debería fallar")
	}

	// La zona recibe un plano → hay que actualizar el master.
	if _, err := svc.Import(zone.ID, []IncomingFile{{Name: "norte.dwg", Content: dwg("v1")}}, "tester"); err != nil {
		t.Fatal(err)
	}
	if c := pending(); len(c.Changed) != 1 || c.Changed[0] != zoneFile {
		t.Fatalf("pendientes tras importar = %+v", c)
	}

	// Integrar: el master recibe el DWG dentro de la carpeta de la zona.
	res, err := svc.IntegrateZone(zone.ID, "", "tester")
	if err != nil {
		t.Fatalf("integrar: %v", err)
	}
	if len(res.Paths) != 1 || res.Paths[0] != masterFile {
		t.Fatalf("paths = %v, quería %s", res.Paths, masterFile)
	}
	got, err := svc.DrawingBytes(master.ID, masterFile, "")
	if err != nil || string(got) != string(dwg("v1")) {
		t.Fatalf("contenido en el master = %q, %v", got, err)
	}
	if n := pending().Count(); n != 0 {
		t.Fatalf("tras integrar quedan %d pendientes", n)
	}
	md, err := svc.Store().ListDrawings(master.ID)
	if err != nil || len(md) != 1 {
		t.Fatalf("planos del master = %d, %v", len(md), err)
	}

	// La zona cambia de nuevo → vuelve a quedar pendiente.
	if _, err := svc.Import(zone.ID, []IncomingFile{{Name: "norte.dwg", Content: dwg("v2")}}, "tester"); err != nil {
		t.Fatal(err)
	}
	if n := pending().Count(); n != 1 {
		t.Fatalf("tras editar la zona hay %d pendientes", n)
	}

	// Alguien edita ese mismo plano dentro del master → conflicto.
	mrepo := svc.Repo(master.ID)
	if err := mrepo.WithLock(func() error {
		_, _, err := mrepo.WriteFilesAndCommit(vcs.MainBranch,
			map[string][]byte{masterFile: dwg("master-edit")}, nil, "edit en master", "otro")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.IntegrateZone(zone.ID, "", "tester")
	var conflict *vcs.ErrConflict
	if !errors.As(err, &conflict) || len(conflict.Paths) != 1 || conflict.Paths[0] != masterFile {
		t.Fatalf("esperaba conflicto en %s, obtuve %v", masterFile, err)
	}
	if got, _ := svc.DrawingBytes(master.ID, masterFile, ""); string(got) != string(dwg("master-edit")) {
		t.Fatal("el conflicto no debe tocar el master")
	}
	if n := pending().Count(); n != 1 {
		t.Fatal("un conflicto no debe marcar la zona como integrada")
	}

	// Imponer la versión de la zona resuelve el conflicto.
	if _, err := svc.IntegrateZone(zone.ID, "take-branch", "tester"); err != nil {
		t.Fatalf("imponer: %v", err)
	}
	if got, _ := svc.DrawingBytes(master.ID, masterFile, ""); string(got) != string(dwg("v2")) {
		t.Fatalf("el master quedó con %q", got)
	}
	if n := pending().Count(); n != 0 {
		t.Fatalf("quedan %d pendientes", n)
	}

	// Un mismo nombre en dos zonas no debe chocar dentro del master.
	other, err := svc.CreateProject(CreateProjectInput{Name: "Zona Sur", ParentID: master.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Import(other.ID, []IncomingFile{{Name: "norte.dwg", Content: dwg("sur")}}, "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IntegrateZone(other.ID, "", "tester"); err != nil {
		t.Fatalf("integrar zona sur: %v", err)
	}
	md, _ = svc.Store().ListDrawings(master.ID)
	if len(md) != 2 {
		t.Fatalf("el master debería tener 2 planos, tiene %d", len(md))
	}

	// Un plano que sale de la zona sale también del master (y del índice).
	zrepo := svc.Repo(zone.ID)
	if err := zrepo.WithLock(func() error {
		_, _, err := zrepo.WriteFilesAndCommit(vcs.MainBranch, nil, []string{zoneFile}, "borrar", "tester")
		if err != nil {
			return err
		}
		return svc.reconcileLocked(zone.ID, zrepo)
	}); err != nil {
		t.Fatal(err)
	}
	if c := pending(); len(c.Removed) != 1 {
		t.Fatalf("esperaba 1 baja pendiente, %+v", c)
	}
	if _, err := svc.IntegrateZone(zone.ID, "", "tester"); err != nil {
		t.Fatalf("integrar baja: %v", err)
	}
	if _, err := svc.DrawingBytes(master.ID, masterFile, ""); err == nil {
		t.Fatal("el plano debería haber salido del master")
	}
	md, _ = svc.Store().ListDrawings(master.ID)
	if len(md) != 1 {
		t.Fatalf("el índice del master debería tener 1 plano, tiene %d", len(md))
	}
}
