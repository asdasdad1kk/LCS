package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"lcs/internal/config"
	"lcs/internal/server"
	"lcs/internal/store"
	"lcs/internal/tray"
)

func main() {
	var (
		noTray   = flag.Bool("no-tray", false, "no mostrar el icono en la bandeja del sistema")
		open     = flag.Bool("open", false, "abrir el navegador al arrancar")
		verbose  = flag.Bool("v", false, "log detallado")
		headless = flag.Bool("headless", false, "solo servidor, sin bandeja ni navegador")
	)
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}
	if err := cfg.EnsureDirs(); err != nil {
		fatal(err)
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logFile := openLog(cfg)
	defer logFile.Close()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(logFile, os.Stderr), &slog.HandlerOptions{Level: level})))

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		fatal(err)
	}
	defer st.Close()

	srv, err := server.New(cfg, st)
	if err != nil {
		fatal(err)
	}

	ln, alreadyRunning, err := srv.Listen()
	if err != nil {
		fatal(err)
	}
	if alreadyRunning {
		slog.Info("LCS ya está en ejecución en este puerto", "url", cfg.LocalURL())
		if !*headless {
			tray.OpenBrowser(cfg.LocalURL())
		}
		return
	}

	go func() {
		slog.Info("LCS escuchando", "url", cfg.LocalURL(), "workspace", cfg.Workspace, "data", cfg.DataDir)
		if err := srv.Serve(ln); err != nil {
			slog.Error("servidor", "err", err)
		}
	}()

	// Warm up the index of every project on startup.
	go warm(srv)

	tray.SetURLProvider(func() string { return cfg.LocalURL() })

	if *open && !*headless {
		go func() { time.Sleep(400 * time.Millisecond); tray.OpenBrowser(cfg.LocalURL()) }()
	}

	if *noTray || *headless {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
	} else {
		tray.Run(func() { tray.OpenBrowser(cfg.LocalURL()) }, func() {})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func warm(srv *server.Server) {
	svc := srv.Service()
	projects, err := svc.Store().ListProjects()
	if err != nil {
		return
	}
	for _, p := range projects {
		if err := svc.Sync(p.ID); err != nil {
			slog.Warn("sync inicial", "project", p.ID, "err", err)
			continue
		}
		slog.Debug("proyecto sincronizado", "project", p.ID)
	}
}

func openLog(cfg *config.Config) *os.File {
	dir := filepath.Join(cfg.DataDir, "logs")
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(filepath.Join(dir, "lcs.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return os.Stderr
	}
	return f
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "LCS:", err)
	os.Exit(1)
}
