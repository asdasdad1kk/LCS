package app

import (
	"log/slog"
	"time"
)

// trace registra en el log las operaciones que tardan más de 300 ms, para ver
// dónde se va el tiempo (casi siempre es la red hacia el NAS):
//
//	defer trace("Upload")()
func trace(op string) func() {
	start := time.Now()
	return func() {
		if d := time.Since(start); d > 300*time.Millisecond {
			slog.Info("lento", "op", op, "dur", d.Round(time.Millisecond).String())
		}
	}
}
