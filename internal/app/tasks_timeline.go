package app

import (
	"sort"
	"strings"
	"time"

	"lcs/internal/store"
)

// ChangedFile es un plano que la tarea modificó respecto de main.
type ChangedFile struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	DrawingID string `json:"drawingId"`
	Size      int64  `json:"size"`
}

func (s *Service) changedFiles(b *store.Branch) []ChangedFile {
	files, _ := s.st.ListBranchFiles(b.ID)
	out := []ChangedFile{}
	for _, f := range files {
		if !f.Changed {
			continue
		}
		out = append(out, ChangedFile{
			Path: f.Path, Name: displayName(f.Path), DrawingID: drawingID(b.ProjectID, f.Path), Size: f.SizeBytes,
		})
	}
	return out
}

// TaskRow es una tarea con su estado y su línea de tiempo, para la tabla de
// seguimiento: cualquiera ve en qué paso va cada tarea de cualquier proyecto.
type TaskRow struct {
	Branch   store.BranchRow `json:"branch"`
	Nodes    []Node          `json:"nodes"`
	Files    []ChangedFile   `json:"files"`
	Timeline []TimelineItem  `json:"timeline"`
	// Stage: open | review | approving | ready | merged | closed
	Stage string `json:"stage"`
}

func stageOf(b *store.Branch, st ReviewState) string {
	switch b.Status {
	case "merged", "closed":
		return b.Status
	}
	switch {
	case !st.HasChanges:
		return "open"
	case st.Required && !st.Complete:
		return "approving"
	case st.Required && st.Complete:
		return "ready"
	}
	return "review"
}

// TasksTimeline lista las tareas de los proyectos pedidos (todos si ids está
// vacío), las más recientes primero. Lee solo del índice local; el refresco
// contra el NAS se hace a lo sumo una vez cada 10 s.
func (s *Service) TasksTimeline(ids []string, status string) ([]TaskRow, error) {
	s.RefreshAllReviews(10 * time.Second)
	rows, err := s.st.ListAllBranches(status)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			want[id] = true
		}
	}
	out := []TaskRow{}
	for _, row := range rows {
		if len(want) > 0 && !want[row.ProjectID] {
			continue
		}
		b := row.Branch
		st := s.reviewState(&b)
		out = append(out, TaskRow{
			Branch: row, Nodes: st.Nodes, Files: s.changedFiles(&b),
			Timeline: s.timeline(&b, st), Stage: stageOf(&b, st),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Branch.UpdatedAt > out[j].Branch.UpdatedAt })
	return out, nil
}
