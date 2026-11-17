//go:build windows

// Command installer es el instalador de LCS: un solo .exe que lleva LCS.exe
// dentro (comprimido), no pide administrador y no descarga nada.
//
//	LCS-Setup.exe                  asistente gráfico
//	LCS-Setup.exe --silent         instala sin ventana (--dir, --workspace, --no-desktop)
//	uninstall.exe --uninstall      desinstala (también con --silent)
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	syswin "golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	appName    = "LCS"
	appVersion = "0.1.0"
	uninstKey  = `Software\Microsoft\Windows\CurrentVersion\Uninstall\LCS`
)

//go:embed ui
var uiFS embed.FS
var buildWorkspaceBase64 string

//go:embed all:payload
var payloadFS embed.FS

func main() {
	var (
		silent    = flag.Bool("silent", false, "instalar/desinstalar sin ventana")
		uninstall = flag.Bool("uninstall", false, "desinstalar")
		dir       = flag.String("dir", "", "carpeta de instalación")
		workspace = flag.String("workspace", "", "carpeta de proyectos")
		noDesktop = flag.Bool("no-desktop", false, "no crear acceso directo en el escritorio")
	)
	flag.Parse()

	in := &Installer{}
	if *silent {
		var err error
		if *uninstall {
			err = in.Uninstall()
		} else {
			d, w := *dir, *workspace
			if d == "" {
				d = defaultDir()
			}
			if w == "" {
				w = defaultWorkspace()
			}
			err = in.Install(d, w, !*noDesktop)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	in.uninstallMode = *uninstall
	err := wails.Run(&options.App{
		Title:            "Instalador de LCS",
		Width:            600,
		Height:           440,
		DisableResize:    true,
		Frameless:        true,
		BackgroundColour: options.NewRGB(26, 23, 20),
		AssetServer:      &assetserver.Options{Assets: uiFS},
		OnStartup:        func(ctx context.Context) { in.ctx = ctx },
		Bind:             []interface{}{in},
		Windows:          &windows.Options{Theme: windows.Dark},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------- rutas

func localAppData() string {
	if v := os.Getenv("LOCALAPPDATA"); v != "" {
		return v
	}
	return filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
}

func defaultDir() string { return filepath.Join(localAppData(), "Programs", appName) }

func configPath() string { return filepath.Join(localAppData(), appName, "config.json") }

func defaultWorkspace() string {
	if buildWorkspaceBase64 != "" {
		if b, err := base64.StdEncoding.DecodeString(buildWorkspaceBase64); err == nil {
			if workspace := strings.TrimSpace(string(b)); workspace != "" {
				return workspace
			}
		}
	}

	return filepath.Join(os.Getenv("USERPROFILE"), "Documents", appName)
}

func readConfig() map[string]any {
	cfg := map[string]any{}
	if b, err := os.ReadFile(configPath()); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	return cfg
}

// knownDir resuelve carpetas especiales con la API de Windows: el escritorio suele
// estar redirigido a OneDrive y %USERPROFILE%\Desktop no existe.
func knownDir(id *syswin.KNOWNFOLDERID, fallback string) string {
	if p, err := syswin.KnownFolderPath(id, 0); err == nil && p != "" {
		return p
	}
	return fallback
}

func startMenuDir() string {
	return knownDir(syswin.FOLDERID_Programs, filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs"))
}

func desktopDir() string {
	return knownDir(syswin.FOLDERID_Desktop, filepath.Join(os.Getenv("USERPROFILE"), "Desktop"))
}

// installedDir devuelve la carpeta donde LCS ya está instalado ("" si no).
func installedDir() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, uninstKey, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("InstallLocation")
	if err != nil {
		return ""
	}
	return v
}

func hidden(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return c
}

// ---------------------------------------------------------------- servicio enlazado a la ventana

type Installer struct {
	ctx           context.Context
	uninstallMode bool
}

type Info struct {
	Version     string `json:"version"`
	Dir         string `json:"dir"`
	Workspace   string `json:"workspace"`
	Installed   bool   `json:"installed"`
	Uninstall   bool   `json:"uninstall"`
	PayloadSize int    `json:"payloadSize"`
}

func (i *Installer) Info() Info {
	dir := installedDir()
	d := dir
	if d == "" {
		d = defaultDir()
	}
	payload, _ := payloadFS.ReadFile("payload/lcs.exe.gz")
	return Info{Version: appVersion, Dir: d, Workspace: defaultWorkspace(), Installed: dir != "", Uninstall: i.uninstallMode, PayloadSize: len(payload)}
}

func (i *Installer) PickFolder(title, start string) string {
	p, err := runtime.OpenDirectoryDialog(i.ctx, runtime.OpenDialogOptions{Title: title, DefaultDirectory: start, CanCreateDirectories: true})
	if err != nil {
		return ""
	}
	return p
}

func (i *Installer) progress(pct int, text string) {
	if i.ctx != nil {
		runtime.EventsEmit(i.ctx, "progress", map[string]any{"pct": pct, "text": text})
	}
}

func (i *Installer) Minimise() { runtime.WindowMinimise(i.ctx) }
func (i *Installer) Close()    { runtime.Quit(i.ctx) }

// Launch abre LCS ya instalado y cierra el instalador.
func (i *Installer) Launch() {
	if d := installedDir(); d != "" {
		_ = exec.Command(filepath.Join(d, "LCS.exe")).Start()
	}
	runtime.Quit(i.ctx)
}

// ---------------------------------------------------------------- instalar

func (i *Installer) Install(dir, workspace string, desktop bool) error {
	dir = filepath.Clean(strings.TrimSpace(dir))
	workspace = strings.TrimSpace(workspace)
	if dir == "." || dir == "" {
		return errors.New("carpeta de instalación vacía")
	}

	i.progress(5, "Cerrando LCS si está abierto…")
	_ = hidden("taskkill", "/F", "/IM", "LCS.exe").Run()

	i.progress(15, "Copiando archivos…")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	gz, err := payloadFS.ReadFile("payload/lcs.exe.gz")
	if err != nil || len(gz) == 0 {
		return errors.New("este instalador no incluye LCS.exe (compílalo con installer.ps1)")
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return err
	}
	exe := filepath.Join(dir, "LCS.exe")
	if err := writeAtomic(exe, zr); err != nil {
		return fmt.Errorf("no pude escribir LCS.exe: %w", err)
	}

	i.progress(55, "Preparando el desinstalador…")
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if self != filepath.Join(dir, "uninstall.exe") {
		f, err := os.Open(self)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := writeAtomic(filepath.Join(dir, "uninstall.exe"), f); err != nil {
			return err
		}
	}

	i.progress(70, "Configurando la carpeta de proyectos…")
	if workspace != "" {
		if err := os.MkdirAll(workspace, 0o755); err != nil {
			return fmt.Errorf("no pude crear %s: %w", workspace, err)
		}
		cfg := readConfig()
		cfg["workspace"] = workspace
		if _, ok := cfg["port"]; !ok {
			cfg["port"] = 7809
		}
		b, _ := json.MarshalIndent(cfg, "", "  ")
		if err := os.MkdirAll(filepath.Dir(configPath()), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(configPath(), b, 0o644); err != nil {
			return err
		}
	}

	i.progress(82, "Creando accesos directos…")
	links := []string{filepath.Join(startMenuDir(), appName+".lnk")}
	if desktop {
		links = append(links, filepath.Join(desktopDir(), appName+".lnk"))
	}
	for n, l := range links {
		_ = os.MkdirAll(filepath.Dir(l), 0o755)
		// El menú Inicio es obligatorio; el escritorio es un extra y no frena la instalación.
		if err := makeShortcuts(exe, dir, []string{l}); err != nil && n == 0 {
			return fmt.Errorf("accesos directos: %w", err)
		}
	}

	i.progress(94, "Registrando en Windows…")
	if err := registerUninstall(dir, exe); err != nil {
		return err
	}
	i.progress(100, "Listo")
	return nil
}

func writeAtomic(dst string, r io.Reader) error {
	tmp := dst + ".new"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = os.Remove(dst)
	return os.Rename(tmp, dst)
}

func makeShortcuts(target, workDir string, links []string) error {
	var sb strings.Builder
	sb.WriteString("$s=New-Object -ComObject WScript.Shell;")
	for _, l := range links {
		fmt.Fprintf(&sb, "$l=$s.CreateShortcut('%s');$l.TargetPath='%s';$l.WorkingDirectory='%s';$l.IconLocation='%s,0';$l.Description='LCS - Layout Control System';$l.Save();",
			psq(l), psq(target), psq(workDir), psq(target))
	}
	out, err := hidden("powershell", "-NoProfile", "-NonInteractive", "-Command", sb.String()).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func psq(s string) string { return strings.ReplaceAll(s, "'", "''") }

func registerUninstall(dir, exe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	for name, v := range map[string]string{
		"DisplayName":     "LCS - Layout Control System",
		"DisplayVersion":  appVersion,
		"Publisher":       "LCS",
		"InstallLocation": dir,
		"DisplayIcon":     exe,
		"UninstallString": fmt.Sprintf(`"%s" --uninstall`, filepath.Join(dir, "uninstall.exe")),
	} {
		if err := k.SetStringValue(name, v); err != nil {
			return err
		}
	}
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	return nil
}

// ---------------------------------------------------------------- desinstalar

// Uninstall quita programa, accesos y registro. No toca proyectos ni config.json.
func (i *Installer) Uninstall() error {
	dir := installedDir()
	if dir == "" {
		dir = defaultDir()
	}
	i.progress(10, "Cerrando LCS…")
	_ = hidden("taskkill", "/F", "/IM", "LCS.exe").Run()
	i.progress(40, "Quitando accesos directos…")
	_ = os.Remove(filepath.Join(startMenuDir(), appName+".lnk"))
	_ = os.Remove(filepath.Join(desktopDir(), appName+".lnk"))
	i.progress(65, "Quitando el registro…")
	_ = registry.DeleteKey(registry.CURRENT_USER, uninstKey)
	i.progress(85, "Borrando archivos…")
	_ = os.Remove(filepath.Join(dir, "LCS.exe"))
	// uninstall.exe está en uso: se reintenta borrar la carpeta hasta que este proceso termine.
	cleanup := hidden("cmd")
	// CmdLine en crudo: Go escaparía las comillas de forma que cmd.exe no entiende.
	cleanup.SysProcAttr.CmdLine = fmt.Sprintf(`cmd /C for /l %%i in (1,1,60) do (rmdir /s /q "%[1]s" 2>nul & if not exist "%[1]s" exit & ping -n 2 127.0.0.1 >nul)`, dir)
	_ = cleanup.Start()
	i.progress(100, "Listo")
	return nil
}
