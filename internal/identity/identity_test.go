package identity

import "testing"

func TestIsEmail(t *testing.T) {
	ok := []string{"ana@empresa.com", "ana.perez@sub.empresa.com.mx", "a+b@x.io"}
	bad := []string{"", "ana", "ana@", "@empresa.com", "ana@empresa", "ana@empresa.", "Ana <ana@empresa.com>",
		"ana@empresa.com, luis@empresa.com", "ana perez@empresa.com", "DOMINIO\\ana"}
	for _, s := range ok {
		if !IsEmail(s) {
			t.Errorf("IsEmail(%q) = false", s)
		}
	}
	for _, s := range bad {
		if IsEmail(s) {
			t.Errorf("IsEmail(%q) = true", s)
		}
	}
}

func TestResolveOverrideWins(t *testing.T) {
	id := Resolve("  Dev@Lcs.Local ")
	if id.Email != "dev@lcs.local" || id.Name != "dev" || !id.Verified || id.Source != "config" {
		t.Fatalf("override = %+v", id)
	}
}

func TestResolveAlwaysYieldsAnEmail(t *testing.T) {
	// Sin override y fuera de un dominio, igual debe haber una identidad usable.
	id := Resolve("no-es-un-correo")
	if !IsEmail(id.Email) || id.Name == "" || id.Source == "" {
		t.Fatalf("identidad inválida: %+v", id)
	}
}
