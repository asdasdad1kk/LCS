package app

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"lcs/internal/identity"
)

const tombstone = ".lcs-deleted"

// DeleteResult dice qué se borró (un master arrastra a sus zonas).
type DeleteResult struct {
	Deleted []string `json:"deleted"`
}

// DeleteProject elimina un proyecto o subproyecto. La pantalla se libera al
// instante: se marca la carpeta del NAS como borrada (así nadie la vuelve a
// descubrir), se retira del índice y del clon de esta máquina, y la carpeta se
// borra en segundo plano con reintentos (en un NAS puede tardar minutos o
// toparse con archivos abiertos por otro equipo).
// Borrar un master borra también sus zonas. Solo puede hacerlo el dueño.
func (s *Service) DeleteProject(id string) (*DeleteResult, error) {
	p, err := s.st.GetProject(id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errors.New("proyecto no encontrado")
	}
	me := identity.Normalize(s.cfg.Email)
	if owner := identity.Normalize(p.Owner); owner != "" && owner != me {
		return nil, fmt.Errorf("solo el dueño del proyecto (%s) puede borrarlo", owner)
	}

	ids := []string{}
	if p.ParentID == "" {
		zones, _ := s.st.ListChildren(id)
		for _, z := range zones {
			ids = append(ids, z.ID)
		}
	}
	ids = append(ids, id) // zonas primero, master al final

	for _, pid := range ids {
		dir := filepath.Join(s.cfg.ProjectsDir(), pid)
		// Primero la marca: sin ella, el siguiente descubrimiento volvería a
		// registrar el proyecto mientras la carpeta todavía existe.
		if _, err := os.Stat(dir); err == nil {
			if err := os.WriteFile(filepath.Join(dir, tombstone), []byte(time.Now().Format(time.RFC3339)), 0o644); err != nil {
				return nil, fmt.Errorf("marcando %s como borrado: %w", pid, err)
			}
		}
		if err := s.forgetProject(pid); err != nil {
			return nil, fmt.Errorf("borrando %s: %w", pid, err)
		}
		s.cleanupDir(pid)
	}
	_ = s.st.AddEvent("", "", "project.deleted", s.cfg.Actor, map[string]any{"name": p.Name, "ids": ids})
	return &DeleteResult{Deleted: ids}, nil
}

// cleanupDir borra la carpeta del proyecto en el NAS en segundo plano.
func (s *Service) cleanupDir(id string) {
	if _, running := s.cleaning.LoadOrStore(id, true); running {
		return
	}
	go func() {
		defer s.cleaning.Delete(id)
		dir := filepath.Join(s.cfg.ProjectsDir(), id)
		wait := 2 * time.Second
		for i := 0; i < 10; i++ {
			err := os.RemoveAll(dir)
			if err == nil {
				return
			}
			slog.Warn("no pude borrar la carpeta del proyecto, reintento", "project", id, "err", err.Error())
			time.Sleep(wait)
			if wait < time.Minute {
				wait *= 2
			}
		}
	}()
}

// forgetProject quita de esta máquina el clon y el índice, sin tocar el NAS.
func (s *Service) forgetProject(id string) error {
	repo := s.repo(id)
	return repo.WithLock(func() error {
		s.seenMu.Lock()
		delete(s.seen, id)
		s.seenMu.Unlock()
		s.reconMu.Lock()
		delete(s.reconSig, id)
		s.reconMu.Unlock()
		s.usageMu.Lock()
		delete(s.usage, id)
		s.usageMu.Unlock()
		s.indexedMu.Lock()
		delete(s.indexedMain, id)
		s.indexedMu.Unlock()
		if err := os.RemoveAll(repo.Local); err != nil {
			slog.Warn("no pude borrar el clon local", "project", id, "err", err.Error())
		}
		return s.st.DeleteProject(id)
	})
}

// pruneVanished quita del índice los proyectos cuya carpeta ya no existe en el
// recurso compartido (otro equipo los borró).
func (s *Service) pruneVanished() {
	entries, err := os.ReadDir(s.cfg.ProjectsDir())
	if err != nil {
		return // NAS desconectado: no se concluye nada
	}
	if len(entries) == 0 {
		return // un listado vacío puede ser un fallo del recurso: no se concluye nada
	}
	present := map[string]bool{}
	for _, e := range entries {
		present[e.Name()] = true
	}
	projects, err := s.st.ListProjects()
	if err != nil {
		return
	}
	for _, p := range projects {
		if time.Since(time.UnixMilli(p.CreatedAt)) < 5*time.Minute {
			continue
		}
		gone := !present[p.ID]
		if !gone {
			// La carpeta existe pero otro equipo la marcó como borrada.
			if _, err := os.Stat(filepath.Join(s.cfg.ProjectsDir(), p.ID, tombstone)); err == nil {
				gone = true
			}
		}
		if gone {
			_ = s.forgetProject(p.ID)
		}
	}
}
