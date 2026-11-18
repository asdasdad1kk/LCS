package app

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"lcs/internal/identity"
	"lcs/internal/store"
	"lcs/internal/vcs"
)

// Discover registra los proyectos que existen en la carpeta compartida pero no
// en este equipo: los creó otra persona. Sin esto un aprobador nunca vería
// los proyectos donde le toca aprobar.
//
// Lee .lcs/project.json directo del repositorio compartido; el clon local se
// crea en la primera sincronización. Devuelve cuántos proyectos nuevos halló.
func (s *Service) Discover(background bool) int {
	entries, err := os.ReadDir(s.cfg.ProjectsDir())
	if err != nil {
		return 0
	}
	s.pruneVanished()
	found := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if existing, _ := s.st.GetProject(id); existing != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.cfg.ProjectsDir(), id, tombstone)); err == nil {
			s.cleanupDir(id) // borrado en otro equipo y a medias: se termina de limpiar
			continue
		}
		bare := filepath.Join(s.cfg.ProjectsDir(), id, "repo.git")
		if _, err := os.Stat(filepath.Join(bare, "HEAD")); err != nil {
			continue
		}
		pf, err := vcs.ReadBareProject(bare)
		if err != nil {
			slog.Debug("proyecto sin project.json", "project", id, "err", err)
			continue
		}
		name := pf.Name
		if name == "" {
			name = id
		}
		p := &store.Project{
			ID: id, Name: name, Description: pf.Description, ParentID: pf.Parent,
			Owner: identity.Normalize(pf.Owner),
		}
		if err := s.st.CreateProject(p); err != nil {
			continue
		}
		_ = s.st.AddEvent(id, "", "project.discovered", s.cfg.Email, map[string]any{"name": name})
		found++
		if !background {
			continue
		}
		go func() {
			if err := s.Sync(id); err != nil {
				slog.Warn("sincronizando proyecto descubierto", "project", id, "err", err)
			}
		}()
	}
	return found
}

// MaybeDiscover llama a Discover a lo sumo una vez por ttl.
func (s *Service) MaybeDiscover(ttl time.Duration) int {
	s.refreshMu.Lock()
	if time.Since(s.discoveredAt) < ttl {
		s.refreshMu.Unlock()
		return 0
	}
	s.discoveredAt = time.Now()
	s.refreshMu.Unlock()
	return s.Discover(true)
}
