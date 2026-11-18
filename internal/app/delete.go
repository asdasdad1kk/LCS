package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"lcs/internal/identity"
)

// DeleteResult dice qué se borró (un master arrastra a sus zonas).
type DeleteResult struct {
	Deleted []string `json:"deleted"`
}

// DeleteProject elimina un proyecto o subproyecto: el repositorio compartido, el
// clon de esta máquina y el índice. Borrar un master borra también sus zonas.
// Solo puede hacerlo el dueño (o cualquiera si el proyecto no tiene dueño).
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
		if err := s.removeProject(pid); err != nil {
			return nil, fmt.Errorf("borrando %s: %w", pid, err)
		}
	}
	_ = s.st.AddEvent("", "", "project.deleted", s.cfg.Actor, map[string]any{"name": p.Name, "ids": ids})
	return &DeleteResult{Deleted: ids}, nil
}

func (s *Service) removeProject(id string) error {
	// Primero la carpeta compartida: si falla, el proyecto sigue en pie.
	if err := os.RemoveAll(filepath.Join(s.cfg.ProjectsDir(), id)); err != nil {
		return err
	}
	return s.forgetProject(id)
}

// forgetProject quita de esta máquina el clon y el índice, sin tocar el NAS.
func (s *Service) forgetProject(id string) error {
	repo := s.repo(id)
	return repo.WithLock(func() error {
		if err := os.RemoveAll(repo.Local); err != nil {
			return err
		}
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
		return s.st.DeleteProject(id)
	})
}

// pruneVanished quita del índice los proyectos cuya carpeta ya no existe en el
// recurso compartido: otro equipo los borró.
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
		if present[p.ID] || time.Since(time.UnixMilli(p.CreatedAt)) < 5*time.Minute {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.cfg.ProjectsDir(), p.ID)); os.IsNotExist(err) {
			_ = s.forgetProject(p.ID)
		}
	}
}
