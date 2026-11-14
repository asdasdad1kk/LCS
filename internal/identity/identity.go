// Package identity resuelve quién es el usuario de esta máquina, sin pedirle
// que inicie sesión: LCS es una app de escritorio y la sesión de Windows ya lo
// identificó.
//
// Orden de resolución:
//
//  1. Override explícito (LCS_EMAIL o "email" en config.json). Sirve para
//     equipos fuera del dominio corporativo y para las pruebas.
//  2. UPN de la sesión de Windows (lo mismo que imprime `whoami /upn`), leído
//     con la API de Windows, sin lanzar ningún proceso.
//  3. `whoami /upn` con la ventana oculta, por si la API no lo entrega.
//  4. usuario@dominio-DNS, si la máquina está en un dominio.
//  5. usuario@equipo.local. No está verificado: no coincide con ningún
//     aprobador real hasta que se configure un correo.
//
// Esto identifica, no autentica criptográficamente: quien pueda editar la
// carpeta compartida podría escribir registros con otro nombre. La barrera
// real son los permisos de esa carpeta.
package identity

import (
	"net/mail"
	"os"
	"os/user"
	"strings"
)

type Identity struct {
	Email string `json:"email"`
	// Name es la parte local del correo, para mostrar.
	Name string `json:"name"`
	// Verified es true cuando el correo vino del sistema operativo o de una
	// configuración explícita, y false cuando se dedujo.
	Verified bool `json:"verified"`
	// Source dice de dónde salió: config, upn, whoami, dominio o local.
	Source string `json:"source"`
}

// Normalize deja un correo comparable: minúsculas y sin espacios.
func Normalize(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// IsEmail acepta solo direcciones simples (usuario@dominio.tld).
func IsEmail(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t<>,;") {
		return false
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s {
		return false
	}
	at := strings.LastIndex(s, "@")
	return at > 0 && strings.Contains(s[at+1:], ".") && !strings.HasSuffix(s, ".")
}

// NameOf devuelve la parte local de un correo.
func NameOf(email string) string {
	if i := strings.Index(email, "@"); i > 0 {
		return email[:i]
	}
	return email
}

// Resolve determina la identidad de esta sesión. override es el correo
// configurado explícitamente ("" si no hay).
func Resolve(override string) Identity {
	if e := Normalize(override); e != "" {
		if IsEmail(e) {
			return Identity{Email: e, Name: NameOf(e), Verified: true, Source: "config"}
		}
	}
	if e := Normalize(osUPN()); IsEmail(e) {
		return Identity{Email: e, Name: NameOf(e), Verified: true, Source: "upn"}
	}
	if e := Normalize(whoamiUPN()); IsEmail(e) {
		return Identity{Email: e, Name: NameOf(e), Verified: true, Source: "whoami"}
	}

	login := loginName()
	if domain := strings.ToLower(strings.TrimSpace(os.Getenv("USERDNSDOMAIN"))); domain != "" && login != "" {
		if e := Normalize(login + "@" + domain); IsEmail(e) {
			return Identity{Email: e, Name: login, Verified: false, Source: "dominio"}
		}
	}

	host, _ := os.Hostname()
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		host = "equipo"
	}
	if login == "" {
		login = "operador"
	}
	e := Normalize(login + "@" + host + ".local")
	return Identity{Email: e, Name: login, Verified: false, Source: "local"}
}

// loginName es el usuario de Windows sin el prefijo DOMINIO\.
func loginName() string {
	u, err := user.Current()
	if err != nil || u.Username == "" {
		return strings.ToLower(strings.TrimSpace(os.Getenv("USERNAME")))
	}
	name := u.Username
	if i := strings.LastIndexAny(name, `\/`); i >= 0 {
		name = name[i+1:]
	}
	return strings.ToLower(strings.TrimSpace(name))
}
