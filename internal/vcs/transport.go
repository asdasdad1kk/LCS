package vcs

import (
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
)

// El remoto "origin" es una ruta local (el repositorio bare compartido). Por
// defecto go-git lo atiende lanzando `git --exec-path`, `git-upload-pack` y
// `git-receive-pack` como procesos hijos; como LCS.exe se compila con
// -H windowsgui (sin consola), Windows abre una ventana de terminal por cada
// uno de ellos en cada fetch/push.
//
// Registramos el servidor en proceso de go-git para el esquema "file": hace lo
// mismo leyendo el repositorio directamente, sin lanzar ningún proceso ni
// depender de que git esté instalado.
func init() {
	client.InstallProtocol("file", server.NewClient(server.DefaultLoader))
}
