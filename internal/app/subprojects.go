package app

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"lcs/internal/store"
	"lcs/internal/vcs"
)

// Un proyecto master (el layout general) tiene subproyectos: una zona o área
// con su propio repositorio. Mientras la zona avanza, el master queda atrás.
// "Integrar" copia los DWG vigentes de la zona dentro del master, en
// drawings/<id-de-la-zona>/, y deja un commit en el master con el historial.
//
// No es un merge de git: un DWG es binario, así que el contenido se copia
// entero. Lo que sí se controla es el conflicto: si alguien tocó esos mismos
// archivos dentro del master desde la última integración, hay que decidir.

// ZoneChanges lists what the zone has on main that the master does not.
type ZoneChanges struct {
	Head    string   `json:"head"`    // main de la zona
	Changed []string `json:"changed"` // DWG nuevos o modificados (rutas de la zona)
	Removed []string `json:"removed"` // DWG que ya no existen en la zona
}

func (c ZoneChanges) Count() int { return len(c.Changed) + len(c.Removed) }

// MasterPath maps a zone path to where it lives inside the master.
func MasterPath(zoneID, zonePath string) string {
	return vcs.DrawingsDir + "/" + zoneID + "/" + strings.TrimPrefix(zonePath, vcs.DrawingsDir+"/")
}

// ZoneChanges compares the zone's main against the last integrated commit.
func (s *Service) ZoneChanges(sub *store.Project) (ZoneChanges, error) {
	var out ZoneChanges
	if sub.ParentID == "" {
		return out, nil
	}
	repo := s.repo(sub.ID)
	head, err := repo.HeadOf(vcs.MainBranch)
	if err != nil {
		return out, err
	}
	out.Head = head
	if head == sub.IntegratedCommit {
		return out, nil
	}
	entries, err := repo.ListDrawings(vcs.MainBranch)
	if err != nil {
		return out, err
	}
	present := map[string]bool{}
	for _, e := range entries {
		present[e.Path] = true
	}

	if sub.IntegratedCommit == "" {
		for _, e := range entries {
			out.Changed = append(out.Changed, e.Path)
		}
		sort.Strings(out.Changed)
		return out, nil
	}
	paths, err := repo.ChangedSince(sub.IntegratedCommit, head)
	if err != nil {
		return out, err
	}
	for _, p := range paths {
		if !strings.HasPrefix(p, vcs.DrawingsDir+"/") || !strings.EqualFold(path.Ext(p), ".dwg") {
			continue
		}
		if present[p] {
			out.Changed = append(out.Changed, p)
		} else {
			out.Removed = append(out.Removed, p)
		}
	}
	return out, nil
}

// IntegrateZone copies the zone's current drawings into the master.
// strategy "take-branch" imposes the zone's version when the master has edited
// the same files since the last integration.
func (s *Service) IntegrateZone(zoneID, strategy, actor string) (*vcs.MergeResult, error) {
	sub, err := s.st.GetProject(zoneID)
	if err != nil {
		return nil, err
	}
	if sub == nil || sub.ParentID == "" {
		return nil, errors.New("este proyecto no es un subproyecto")
	}
	master, err := s.st.GetProject(sub.ParentID)
	if err != nil || master == nil {
		return nil, errors.New("el proyecto master no existe")
	}

	masterRepo, subRepo := s.repo(master.ID), s.repo(sub.ID)
	var res *vcs.MergeResult

	// Siempre master primero y zona después: dos integraciones cruzadas no
	// pueden esperarse entre sí.
	err = masterRepo.WithLock(func() error {
		return subRepo.WithLock(func() error {
			if err := subRepo.Pull(vcs.MainBranch); err != nil {
				return err
			}
			if err := masterRepo.Pull(vcs.MainBranch); err != nil {
				return err
			}
			// El estado leído antes del lock puede haber quedado viejo.
			sub, err = s.st.GetProject(zoneID)
			if err != nil || sub == nil {
				return errors.New("subproyecto no encontrado")
			}
			changes, err := s.ZoneChanges(sub)
			if err != nil {
				return err
			}
			if changes.Count() == 0 {
				return errors.New("la zona no tiene cambios para integrar")
			}

			targets := map[string]string{} // ruta en la zona → ruta en el master
			for _, p := range append(append([]string{}, changes.Changed...), changes.Removed...) {
				targets[p] = MasterPath(sub.ID, p)
			}

			if sub.IntegratedMasterCommit != "" && strategy != "take-branch" {
				masterHead, err := masterRepo.HeadOf(vcs.MainBranch)
				if err != nil {
					return err
				}
				touched, err := masterRepo.ChangedSince(sub.IntegratedMasterCommit, masterHead)
				if err != nil {
					return err
				}
				touchedSet := map[string]bool{}
				for _, p := range touched {
					touchedSet[p] = true
				}
				conflicts := []string{}
				for _, mp := range targets {
					if touchedSet[mp] {
						conflicts = append(conflicts, mp)
					}
				}
				if len(conflicts) > 0 {
					sort.Strings(conflicts)
					return &vcs.ErrConflict{Paths: conflicts}
				}
			}

			payload := map[string][]byte{}
			for _, p := range changes.Changed {
				b, err := subRepo.FileAt(vcs.MainBranch, p)
				if err != nil {
					return fmt.Errorf("leyendo %s de la zona: %w", path.Base(p), err)
				}
				payload[targets[p]] = b
			}
			removes := make([]string, 0, len(changes.Removed))
			for _, p := range changes.Removed {
				removes = append(removes, targets[p])
			}
			sort.Strings(removes)

			msg := fmt.Sprintf("LCS: integrar zona %s (%d plano(s))", sub.Name, changes.Count())
			_, commit, err := masterRepo.WriteFilesAndCommit(vcs.MainBranch, payload, removes, msg, actor)
			if err != nil {
				return err
			}
			if err := s.reconcileLocked(master.ID, masterRepo); err != nil {
				return err
			}
			if err := s.st.SetIntegration(sub.ID, changes.Head, commit); err != nil {
				return err
			}

			paths := make([]string, 0, len(targets))
			for _, mp := range targets {
				paths = append(paths, mp)
			}
			sort.Strings(paths)
			res = &vcs.MergeResult{Commit: commit, Paths: paths}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	payload := map[string]any{"zone": sub.Name, "zoneId": sub.ID, "master": master.Name, "paths": res.Paths}
	_ = s.st.AddEvent(master.ID, "", "subproject.integrated", actor, payload)
	_ = s.st.AddEvent(sub.ID, "", "subproject.integrated", actor, payload)
	return res, nil
}
