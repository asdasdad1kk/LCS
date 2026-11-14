//go:build windows

package tray

import (
	"fmt"
	"os/exec"

	"github.com/energye/systray"
)

// OpenBrowser sends the URL to the default handler without flashing a console.
func OpenBrowser(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// CopyToClipboard uses the PowerShell clipboard cmdlet (no cgo, no Win32 glue).
func CopyToClipboard(text string) {
	_ = exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden",
		"-Command", fmt.Sprintf("Set-Clipboard -Value %q", text)).Start()
}

// Run starts the notification-area icon and blocks until the user quits.
// openURL is called for the menu entries that open the browser.
func Run(openURL func(), onQuit func()) {
	systray.Run(func() {
		systray.SetIcon(icoBytes())
		systray.SetTitle("LCS")
		systray.SetTooltip("LCS — Layout Control System")
		systray.SetOnClick(func(systray.IMenu) { openURL() })
		systray.SetOnDClick(func(systray.IMenu) { openURL() })

		mOpen := systray.AddMenuItem("Abrir LCS", "Abrir el panel en el navegador")
		mCopy := systray.AddMenuItem("Copiar dirección", "Copiar la URL local")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Salir", "Cerrar LCS")

		mOpen.Click(func() { openURL() })
		mCopy.Click(func() {
			if url := openURLURL(); url != "" {
				CopyToClipboard(url)
			}
		})
		mQuit.Click(func() { systray.Quit() })
	}, func() {
		if onQuit != nil {
			onQuit()
		}
	})
}

var currentURL func() string

// SetURLProvider lets the menu copy the address that is actually in use.
func SetURLProvider(fn func() string) { currentURL = fn }

func openURLURL() string {
	if currentURL != nil {
		return currentURL()
	}
	return ""
}
