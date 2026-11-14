package store

// SetBranchTree guarda el hash de drawings/ en la punta de la rama sin tocar
// updated_at (que ordena las listas por actividad real).
func (s *Store) SetBranchTree(id, tree string) error {
	_, err := s.db.Exec(`UPDATE branches SET tree_hash=? WHERE id=?`, tree, id)
	return err
}
