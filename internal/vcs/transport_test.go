package vcs

import (
	"path/filepath"
	"testing"
)

// Fetch/Push contra el bare local deben resolverse en proceso: sin git en el
// PATH no hay forma de lanzar git-upload-pack, así que si el test pasa, no se
// abre ninguna ventana de consola.
func TestLocalRemoteWithoutSpawningGit(t *testing.T) {
	t.Setenv("PATH", "")

	root := t.TempDir()
	bare := filepath.Join(root, "bare.git")
	local := filepath.Join(root, "local")

	if err := Init(bare, local, ProjectFile{ID: "p1", Name: "Proyecto"}, "tester"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := &Repo{Bare: bare, Local: local}

	if _, err := r.CreateBranch("work/x", &BranchMeta{Title: "x"}, "tester"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if _, _, err := r.WriteAndCommit("work/x", "drawings/a.dwg", []byte("dwg"), "add", "tester"); err != nil {
		t.Fatalf("WriteAndCommit: %v", err)
	}
	if err := r.Fetch(); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if err := r.Pull(MainBranch); err != nil {
		t.Fatalf("Pull: %v", err)
	}
}
