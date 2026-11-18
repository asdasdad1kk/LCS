package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"lcs/internal/identity"
	"lcs/internal/store"
	"lcs/internal/vcs"
)

// Análisis de cambios: el visor lee las propiedades de dos versiones de un DWG
// (entidades por handle, capas, textos, cajetín) y calcula qué cambió y dónde.
// Aquí solo se guarda: el resumen (para listas, aprobaciones y la línea de
// tiempo) y el análisis completo, dentro de la rama de la tarea para que todos
// lo vean. Vive en .lcs/analysis/, fuera de drawings/: no invalida aprobaciones
// ni viaja a main.

const (
	analysisDir      = vcs.MetaDir + "/analysis"
	maxAnalysisBytes = 4 << 20
)

type analysisEnvelope struct {
	Path    string          `json:"path"`
	Commit  string          `json:"commit"`
	Summary json.RawMessage `json:"summary"`
}

// AddAnalysis guarda el análisis que calculó el visor para una versión subida.
func (s *Service) AddAnalysis(branchID string, doc []byte) (*store.Analysis, error) {
	b, err := s.st.GetBranch(branchID)
	if err != nil || b == nil {
		return nil, errors.New("rama no encontrada")
	}
	if b.Status == "merged" || b.Status == "closed" {
		return nil, errors.New("la tarea ya está cerrada")
	}
	if len(doc) == 0 || len(doc) > maxAnalysisBytes {
		return nil, errors.New("el análisis está vacío o es demasiado grande")
	}
	var env analysisEnvelope
	if err := json.Unmarshal(doc, &env); err != nil || len(env.Summary) == 0 || env.Path == "" {
		return nil, errors.New("el análisis no tiene el formato esperado")
	}
	me := identity.Normalize(s.cfg.Email)
	now := time.Now().UnixMilli()
	id := fmt.Sprintf("%013d-%s", now, randomHex(2))
	a := store.Analysis{
		ID: id, BranchID: b.ID, Path: env.Path, Commit: env.Commit, Author: identity.NameOf(me), At: now, Summary: env.Summary,
	}
	sumFile, _ := json.Marshal(a)

	repo := s.repo(b.ProjectID)
	err = repo.WithLock(func() error {
		commit, err := repo.CommitFilesOnBranch(b.Name, map[string][]byte{
			analysisDir + "/" + id + ".json":     doc,
			analysisDir + "/" + id + ".sum.json": sumFile,
		}, "LCS: cambios detectados en "+b.Title, s.cfg.Actor)
		if err != nil {
			return err
		}
		b.HeadCommit = commit
		return s.st.UpdateBranch(b)
	})
	if err != nil {
		return nil, err
	}
	if err := s.st.AddAnalysis(a); err != nil {
		return nil, err
	}
	_ = s.st.AddEvent(b.ProjectID, b.ID, "branch.analyzed", s.cfg.Actor, map[string]any{"branch": b.Title, "path": env.Path})
	return &a, nil
}

// Analyses lista los resúmenes de una tarea (del índice local).
func (s *Service) Analyses(branchID string) ([]store.Analysis, error) {
	return s.st.ListAnalyses(branchID)
}

// AnalysisDoc abre el análisis completo.
func (s *Service) AnalysisDoc(branchID, id string) (io.ReadCloser, int64, error) {
	b, err := s.st.GetBranch(branchID)
	if err != nil || b == nil {
		return nil, 0, errors.New("rama no encontrada")
	}
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return nil, 0, errors.New("análisis inválido")
	}
	return s.repo(b.ProjectID).BlobReader(b.Name, analysisDir+"/"+id+".json")
}

// refreshAnalyses trae al índice los resúmenes que escribió cualquier equipo.
func (s *Service) refreshAnalyses(repo *vcs.Repo, b *store.Branch) {
	files, err := repo.ListFilesWhere(b.Name, analysisDir, func(n string) bool { return strings.HasSuffix(n, ".sum.json") })
	if err != nil {
		return
	}
	list := make([]store.Analysis, 0, len(files))
	for _, f := range files {
		var a store.Analysis
		if json.Unmarshal(f.Content, &a) == nil && a.ID != "" {
			a.BranchID = b.ID
			list = append(list, a)
		}
	}
	if len(list) == 0 {
		return
	}
	_ = s.st.ReplaceAnalyses(b.ID, list)
}

// latestAnalysis devuelve el análisis más reciente de cada plano de la tarea.
func latestPerPath(list []store.Analysis) []store.Analysis {
	byPath := map[string]store.Analysis{}
	for _, a := range list {
		if cur, ok := byPath[a.Path]; !ok || a.At >= cur.At {
			byPath[a.Path] = a
		}
	}
	out := make([]store.Analysis, 0, len(byPath))
	for _, a := range byPath {
		out = append(out, a)
	}
	return out
}

// Changes es el resumen de cambios de toda la tarea para listas y paneles: suma
// el último análisis de cada plano.
type Changes struct {
	Added    int      `json:"added"`
	Removed  int      `json:"removed"`
	Modified int      `json:"modified"`
	Total    int      `json:"total"`
	Texts    int      `json:"texts"`
	Layers   []string `json:"layers"`
	Files    int      `json:"files"`
	At       int64    `json:"at"`
}

func (s *Service) changesOf(b *store.Branch) *Changes {
	list, err := s.st.ListAnalyses(b.ID)
	if err != nil || len(list) == 0 {
		return nil
	}
	c := &Changes{}
	seen := map[string]bool{}
	for _, a := range latestPerPath(list) {
		var sum struct {
			Added, Removed, Modified, Total, Texts int
			Layers                                 []string
		}
		raw := struct {
			Added    int      `json:"added"`
			Removed  int      `json:"removed"`
			Modified int      `json:"modified"`
			Total    int      `json:"total"`
			Texts    int      `json:"texts"`
			Layers   []string `json:"layers"`
		}{}
		if json.Unmarshal(a.Summary, &raw) != nil {
			continue
		}
		sum.Added, sum.Removed, sum.Modified, sum.Total, sum.Texts, sum.Layers = raw.Added, raw.Removed, raw.Modified, raw.Total, raw.Texts, raw.Layers
		c.Added += sum.Added
		c.Removed += sum.Removed
		c.Modified += sum.Modified
		c.Total += sum.Total
		c.Texts += sum.Texts
		for _, l := range sum.Layers {
			if !seen[l] && len(c.Layers) < 5 {
				seen[l] = true
				c.Layers = append(c.Layers, l)
			}
		}
		c.Files++
		if a.At > c.At {
			c.At = a.At
		}
	}
	if c.Files == 0 {
		return nil
	}
	return c
}
