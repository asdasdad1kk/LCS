package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"lcs/internal/config"
	"lcs/internal/server"
	"lcs/internal/store"
	"lcs/internal/tray"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx     context.Context
	dataDir string
	mode    string // fondo con el que arrancó la ventana: off | acrylic
}

// uiPrefs vive en <DataDir>/ui.json. Wails fija el fondo al crear la ventana,
// por eso el cambio se aplica en el siguiente arranque.
type uiPrefs struct {
	Backdrop    string `json:"backdrop"`
	Translucent bool   `json:"translucent"` // formato anterior
}

func readMode(dir string) string {
	var p uiPrefs
	if b, err := os.ReadFile(filepath.Join(dir, "ui.json")); err == nil {
		_ = json.Unmarshal(b, &p)
	}
	switch {
	case p.Backdrop == "acrylic" || p.Backdrop == "off":
		return p.Backdrop
	case p.Translucent:
		return "acrylic"
	}
	return "off"
}

// WindowBackdrop: active = fondo con el que arrancó la ventana; pref = el elegido para el próximo arranque.
func (a *App) WindowBackdrop() map[string]string {
	return map[string]string{"active": a.mode, "pref": readMode(a.dataDir)}
}

// SetWindowBackdrop guarda "off" (opaca) o "acrylic" (desenfoque del sistema).
func (a *App) SetWindowBackdrop(mode string) error {
	if mode != "off" && mode != "acrylic" {
		return fmt.Errorf("modo de fondo desconocido: %q", mode)
	}
	b, _ := json.Marshal(uiPrefs{Backdrop: mode})
	return os.WriteFile(filepath.Join(a.dataDir, "ui.json"), b, 0o644)
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }
func (a *App) Minimise()                   { runtime.WindowMinimise(a.ctx) }
func (a *App) ToggleMaximise()             { runtime.WindowToggleMaximise(a.ctx) }

// Restart lanza otra instancia (que espera a que esta libere el puerto) y cierra esta.
func (a *App) Restart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = append(os.Environ(), "LCS_RESTART=1")
	if err := cmd.Start(); err != nil {
		return err
	}
	runtime.Quit(a.ctx)
	return nil
}

func (a *App) Quit() { runtime.Quit(a.ctx) }

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
	// Tras "Reiniciar", la instancia anterior tarda un instante en soltar el puerto.
	if alreadyRunning && os.Getenv("LCS_RESTART") == "1" {
		for i := 0; i < 40 && alreadyRunning && err == nil; i++ {
			time.Sleep(250 * time.Millisecond)
			ln, alreadyRunning, err = srv.Listen()
		}
		if err != nil {
			fatal(err)
		}
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
		app := &App{dataDir: cfg.DataDir, mode: readMode(cfg.DataDir)}
		bg, backdrop := options.NewRGB(34, 33, 31), windows.Auto
		switch app.mode {
		case "acrylic":
			bg, backdrop = options.NewRGBA(0, 0, 0, 0), windows.Acrylic
		}
		transparent := app.mode != "off"
		if err := wails.Run(&options.App{
			Title: "LCS — Layout Control System",
			Width: 1440, Height: 920, MinWidth: 980, MinHeight: 640,
			Frameless:        true,
			BackgroundColour: bg,
			Windows: &windows.Options{
				WebviewIsTransparent: transparent,
				WindowIsTranslucent:  app.mode == "acrylic",
				BackdropType:         backdrop,
			},
			AssetServer: &assetserver.Options{Handler: devHandler(srv.Handler())},
			OnStartup:   app.startup,
			Bind:        []interface{}{app},
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
	// En paralelo: cada proyecto espera al NAS por separado, en serie se sumaban.
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, p := range projects {
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := svc.Sync(id); err != nil {
				slog.Warn("sync inicial", "project", id, "err", err)
				return
			}
			slog.Debug("proyecto sincronizado", "project", id)
		}(p.ID)
	}
	wg.Wait()
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
