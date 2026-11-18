package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lcs/internal/vcs"
)

// Sobre un NAS cada operación sobre un archivo cuesta una espera de red, así
// que lo que se mide es CUÁNTAS se hacen. La prueba simula 30 ms por operación
// y fija un tope de operaciones para subir, aprobar e integrar en un proyecto
// con muchas ramas.
func TestNASOperationBudget(t *testing.T)       { nasBudget(t, false) }
func TestNASOperationBudgetLegacy(t *testing.T) { nasBudget(t, true) }

// legacy: el repositorio compartido no tiene lcs-state (proyectos creados con
// versiones anteriores). Subir, aprobar e integrar no deben recorrer las ramas.
func nasBudget(t *testing.T, legacy bool) {
	svc := newTestService(t)
	p, err := svc.CreateProject(CreateProjectInput{Name: "Planta"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetApprovers(p.ID, []vcs.Approver{{Email: "dev@lcs.local"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Import(p.ID, []IncomingFile{{Name: "a.dwg", Content: dwg("v1")}, {Name: "b.dwg", Content: dwg("b1")}}, "t"); err != nil {
		t.Fatal(err)
	}
	// Muchas tareas viejas: recorrer una a una las referencias era el costo.
	for i := 0; i < 25; i++ {
		if _, err := svc.CreateBranch(CreateBranchInput{ProjectID: p.ID, Drawings: []string{"drawings/b.dwg"}, Title: fmt.Sprintf("vieja %d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	b, err := svc.CreateBranch(CreateBranchInput{ProjectID: p.ID, Drawings: []string{"drawings/a.dwg"}, Title: "actual"})
	if err != nil {
		t.Fatal(err)
	}

	if legacy {
		if err := os.Remove(filepath.Join(svc.Config().ProjectsDir(), p.ID, "repo.git", "lcs-state")); err != nil {
			t.Fatal(err)
		}
	}
	ops, restore := vcs.InstrumentNAS(30 * time.Millisecond)
	defer restore()
	step := func(name string, budget int64, fn func() error) {
		t.Helper()
		f0, o0 := ops()
		start := time.Now()
		if err := fn(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		f1, o1 := ops()
		n := f1 - f0
		t.Logf("%-10s %3d operaciones en el NAS, %d aperturas de repo, %v", name, n, o1-o0, time.Since(start).Round(time.Millisecond))
		if n > budget {
			t.Errorf("%s: %d operaciones contra el NAS, el tope es %d", name, n, budget)
		}
		// Abrir el repositorio del NAS con go-git cuesta decenas de operaciones que
		// el contador de archivos no ve: ninguna acción normal debe hacerlo.
		if o1-o0 != 0 {
			t.Errorf("%s: abrió el repositorio compartido %d veces con go-git (debe ser 0)", name, o1-o0)
		}
	}

	step("subir", 40, func() error {
		_, err := svc.Upload(b.ID, IncomingFile{Name: "a.dwg", Content: dwg("v2")}, "m", "t")
		return err
	})
	step("aprobar", 30, func() error {
		_, err := svc.Decide(b.ID, "approved", "")
		return err
	})
	step("integrar", 40, func() error {
		_, err := svc.Merge(b.ID, "", "t")
		return err
	})
	step("revisar", 6, func() error {
		_, err := svc.Review(b.ID)
		return err
	})
}
