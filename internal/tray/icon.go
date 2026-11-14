package tray

import _ "embed"

// icon.ico lo genera `go run ./tools/appicon` a partir de build/appicon.png;
// es el mismo icono que lleva el ejecutable.
//
//go:embed icon.ico
var ico []byte

func icoBytes() []byte { return ico }
