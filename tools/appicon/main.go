// Command appicon genera los recursos de Windows de LCS a partir de
// build/appicon.png:
//
//   - cmd/lcs/rsrc_windows_amd64.syso  icono del .exe (Explorador, barra de
//     tareas, ventana), manifiesto (DPI por monitor) e información de versión.
//   - internal/tray/icon.ico           el mismo icono para la bandeja.
//
// Ejecutar desde la raíz del repositorio:  go run ./tools/appicon
//
// build/appicon.png es el render a 1024 px de build/appicon.svg.
package main

import (
	"bytes"
	"fmt"
	"image/png"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

var appVersion = [4]uint16{0, 1, 0, 0}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "appicon:", err)
		os.Exit(1)
	}
}

func run() error {
	src, err := os.Open("build/appicon.png")
	if err != nil {
		return err
	}
	defer src.Close()
	img, err := png.Decode(src)
	if err != nil {
		return err
	}
	icon, err := winres.NewIconFromResizedImage(img, []int{256, 128, 64, 48, 40, 32, 24, 20, 16})
	if err != nil {
		return err
	}

	var ico bytes.Buffer
	if err := icon.SaveICO(&ico); err != nil {
		return err
	}
	if err := os.WriteFile("internal/tray/icon.ico", ico.Bytes(), 0o644); err != nil {
		return err
	}

	rs := winres.ResourceSet{}
	// Wails busca el icono de la ventana en el recurso con ID 3 (RT_ICON).
	if err := rs.SetIcon(winres.RT_ICON, icon); err != nil {
		return err
	}
	rs.SetManifest(winres.AppManifest{
		Identity:            winres.AssemblyIdentity{Name: "lcs.LayoutControlSystem", Version: appVersion},
		Description:         "LCS — Layout Control System",
		Compatibility:       winres.Win10AndAbove,
		DPIAwareness:        winres.DPIPerMonitorV2,
		LongPathAware:       true,
		UseCommonControlsV6: true,
	})

	vi := version.Info{FileVersion: appVersion, ProductVersion: appVersion}
	for key, value := range map[string]string{
		version.ProductName:      "LCS — Layout Control System",
		version.FileDescription:  "LCS — Layout Control System",
		version.ProductVersion:   "0.1.0",
		version.FileVersion:      "0.1.0",
		version.InternalName:     "LCS",
		version.OriginalFilename: "LCS.exe",
	} {
		if err := vi.Set(version.LangDefault, key, value); err != nil {
			return err
		}
	}
	rs.SetVersionInfo(vi)

	out, err := os.Create("cmd/lcs/rsrc_windows_amd64.syso")
	if err != nil {
		return err
	}
	defer out.Close()
	return rs.WriteObject(out, winres.ArchAMD64)
}
