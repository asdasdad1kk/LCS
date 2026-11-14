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

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct{ ctx context.Context }

func (a *App) startup(ctx context.Context) { a.ctx = ctx }
func (a *App) Minimise()                   { runtime.WindowMinimise(a.ctx) }
func (a *App) ToggleMaximise()             { runtime.WindowToggleMaximise(a.ctx) }
func (a *App) Quit()                       { runtime.Quit(a.ctx) }

func main() {
	var (
		noTray   = flag.Bool("no-tray", false, "no mostrar el icono en la bandeja del sistema")
		open     = flag.Bool("open", false, "abrir el navegador al arrancar")
		verbose  = flag.Bool("v", false, "log detallado")
		headless = flag.Bool("headless", false, "solo servidor, sin bandeja ni navegador")
	)
	flag.Parse()
	if err := config.LoadDotEnv(); err != nil {
		fatal(err)
	}

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
		// Do not launch another browser process for accidental duplicate starts.
		if *open && !*headless {
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
		app := &App{}
		if err := wails.Run(&options.App{
			Title: "LCS — Layout Control System",
			Width: 1440, Height: 920, MinWidth: 980, MinHeight: 640,
			Frameless:        true,
			BackgroundColour: options.NewRGB(34, 33, 31),
			AssetServer:      &assetserver.Options{Handler: srv.Handler()},
			OnStartup:        app.startup,
			Bind:             []interface{}{app},
		}); err != nil {
			slog.Error("ventana LCS", "err", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func warm(srv *server.Server) {
	svc := srv.Service()
	// Proyectos creados por otras personas en la carpeta compartida.
	if n := svc.Discover(false); n > 0 {
		slog.Info("proyectos descubiertos en la carpeta compartida", "n", n)
	}
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
