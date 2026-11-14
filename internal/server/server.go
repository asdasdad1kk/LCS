package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"lcs/internal/api"
	"lcs/internal/app"
	"lcs/internal/config"
	"lcs/internal/store"
	"lcs/internal/webui"
)

type Server struct {
	cfg   *config.Config
	svc   *app.Service
	http  *http.Server
	mux   *http.ServeMux
	ready chan struct{}
}

func New(cfg *config.Config, st *store.Store) (*Server, error) {
	svc := app.New(cfg, st)
	a := api.New(svc)

	mux := http.NewServeMux()
	spa, err := webui.SPA()
	if err != nil {
		return nil, err
	}

	apiMux := a.Routes()
	mux.Handle("/api/", apiMux)
	mux.Handle("/lcs-api/", http.StripPrefix("/lcs-api", apiMux))
	mux.Handle("/", spa)

	s := &Server{
		cfg:   cfg,
		svc:   svc,
		mux:   mux,
		ready: make(chan struct{}),
	}
	s.http = &http.Server{
		Handler:           s.logged(mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return s, nil
}

// Listen binds the loopback address and reports whether another instance
// already owns the port.
func (s *Server) Listen() (net.Listener, bool, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", s.cfg.Port))
	if err == nil {
		return ln, false, nil
	}
	probe, perr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", s.cfg.Port), 400*time.Millisecond)
	if perr == nil {
		probe.Close()
		return nil, true, nil
	}
	return nil, false, err
}

func (s *Server) Serve(ln net.Listener) error {
	err := s.http.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error { return s.http.Shutdown(ctx) }

func (s *Server) Service() *app.Service { return s.svc }

func (s *Server) logged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path == "/api/bootstrap" || r.URL.Path == "/api/stats" {
			return
		}
		slog.Debug("http", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start).String())
	})
}
