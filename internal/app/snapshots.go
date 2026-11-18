package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"lcs/internal/identity"
	"lcs/internal/vcs"
)

// Recortes: una imagen de la zona del layout que se va a cambiar, guardada con
// la tarea (dentro de su rama, en .lcs/snapshots/) para que cualquiera la vea
// desde su equipo. No forma parte de drawings/, así que no invalida las
// aprobaciones ni viaja a main al integrar.

const (
	snapshotsDir     = vcs.MetaDir + "/snapshots"
	maxSnapshotBytes = 12 << 20
)

type Snapshot struct {
	ID      string `json:"id"`
	Caption string `json:"caption"`
	Author  string `json:"author"`
	Email   string `json:"email"`
	Drawing string `json:"drawing,omitempty"`
	Layout  string `json:"layout,omitempty"`
	// Bounds: la zona en coordenadas del plano (no de la pantalla), para volver a
	// enfocarla en cualquier versión.
	Bounds *Bounds `json:"bounds,omitempty"`
	At     int64   `json:"at"`
	Size   int64   `json:"size"`
}

type Bounds struct {
	MinX float64 `json:"minX"`
	MinY float64 `json:"minY"`
	MaxX float64 `json:"maxX"`
	MaxY float64 `json:"maxY"`
}

type SnapshotInput struct {
	Caption string
	Drawing string
	Layout  string
	Bounds  *Bounds
	PNG     []byte
}

func isPNG(b []byte) bool {
	return len(b) > 8 && string(b[1:4]) == "PNG"
}

// AddSnapshot guarda un recorte en la rama de la tarea.
func (s *Service) AddSnapshot(branchID string, in SnapshotInput) (*Snapshot, error) {
	b, err := s.st.GetBranch(branchID)
	if err != nil || b == nil {
		return nil, errors.New("rama no encontrada")
	}
	if b.Status == "merged" || b.Status == "closed" {
		return nil, errors.New("la tarea ya está cerrada")
	}
	if !isPNG(in.PNG) {
		return nil, errors.New("el recorte debe ser una imagen PNG")
	}
	if len(in.PNG) > maxSnapshotBytes {
		return nil, errors.New("el recorte es demasiado grande")
	}
	me := identity.Normalize(s.cfg.Email)
	now := time.Now().UnixMilli()
	id := fmt.Sprintf("%013d-%s", now, randomHex(2))
	meta := Snapshot{
		ID: id, Caption: strings.TrimSpace(in.Caption), Author: identity.NameOf(me), Email: me,
		Drawing: in.Drawing, Layout: in.Layout, Bounds: in.Bounds, At: now, Size: int64(len(in.PNG)),
	}
	raw, _ := json.MarshalIndent(meta, "", "  ")

	repo := s.repo(b.ProjectID)
	err = repo.WithLock(func() error {
		commit, err := repo.CommitFilesOnBranch(b.Name, map[string][]byte{
			snapshotsDir + "/" + id + ".png":  in.PNG,
			snapshotsDir + "/" + id + ".json": append(raw, '\n'),
		}, "LCS: recorte de "+b.Title, s.cfg.Actor)
		if err != nil {
			return err
		}
		b.HeadCommit = commit
		return s.st.UpdateBranch(b)
	})
	if err != nil {
		return nil, err
	}
	_ = s.st.AddEvent(b.ProjectID, b.ID, "branch.snapshot", s.cfg.Actor, map[string]any{"branch": b.Title, "caption": meta.Caption})
	return &meta, nil
}

// Snapshots lista los recortes de una tarea, del más viejo al más nuevo.
func (s *Service) Snapshots(branchID string) ([]Snapshot, error) {
	b, err := s.st.GetBranch(branchID)
	if err != nil || b == nil {
		return nil, errors.New("rama no encontrada")
	}
	repo := s.repo(b.ProjectID)
	files, err := repo.ListFilesWhere(b.Name, snapshotsDir, func(n string) bool { return strings.HasSuffix(n, ".json") })
	if err != nil {
		return nil, err
	}
	out := []Snapshot{}
	for _, f := range files {
		var sn Snapshot
		if json.Unmarshal(f.Content, &sn) == nil && sn.ID != "" {
			out = append(out, sn)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out, nil
}

// SnapshotImage abre la imagen de un recorte.
func (s *Service) SnapshotImage(branchID, snapshotID string) (io.ReadCloser, int64, error) {
	b, err := s.st.GetBranch(branchID)
	if err != nil || b == nil {
		return nil, 0, errors.New("rama no encontrada")
	}
	if strings.ContainsAny(snapshotID, `/\.`) {
		return nil, 0, errors.New("recorte inválido")
	}
	return s.repo(b.ProjectID).BlobReader(b.Name, snapshotsDir+"/"+snapshotID+".png")
}
