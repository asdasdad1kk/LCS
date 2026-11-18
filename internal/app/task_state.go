package app

import "errors"

var errorsNew = errors.New

// TaskState es todo lo que la pantalla de una tarea necesita, leído del índice
// local (sin red). Las acciones (subir, aprobar, integrar) lo devuelven al
// terminar: la pantalla se actualiza con la respuesta y no con otra ronda de
// consultas.
type TaskState struct {
	Branch *BranchView `json:"branch"`
	Review *ReviewView `json:"review"`
}

func (s *Service) TaskState(branchID string) (*TaskState, error) {
	bv, err := s.BranchView(branchID)
	if err != nil {
		return nil, err
	}
	return &TaskState{Branch: bv, Review: s.reviewView(&bv.Branch)}, nil
}

// FreshTaskState refresca el proyecto (barato si nada cambió) y lee el estado.
func (s *Service) FreshTaskState(branchID string) (*TaskState, error) {
	b, err := s.st.GetBranch(branchID)
	if err != nil || b == nil {
		return nil, errorsNew("rama no encontrada")
	}
	s.RefreshReviews(b.ProjectID)
	if b2, err := s.st.GetBranch(branchID); err == nil && b2 != nil && (b2.Status == "open" || b2.Status == "review") {
		s.refreshBranchFiles(s.repo(b2.ProjectID), b2)
	}
	return s.TaskState(branchID)
}
