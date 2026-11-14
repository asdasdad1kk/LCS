package main

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

// devHandler: con LCS_DEV_URL definido (p. ej. http://localhost:5173) la ventana
// carga el front desde Vite (hot reload) y solo la API sale del binario Go.
// Sin la variable no hace nada: producción sigue sirviendo el SPA embebido.
func devHandler(prod http.Handler) http.Handler {
	raw := os.Getenv("LCS_DEV_URL")
	if raw == "" {
		return prod
	}
	target, err := url.Parse(raw)
	if err != nil {
		slog.Error("LCS_DEV_URL inválida", "err", err)
		return prod
	}
	slog.Info("modo dev: front servido por Vite", "url", raw)
	vite := httputil.NewSingleHostReverseProxy(target)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/lcs-api/") {
			prod.ServeHTTP(w, r)
			return
		}
		vite.ServeHTTP(w, r)
	})
}
