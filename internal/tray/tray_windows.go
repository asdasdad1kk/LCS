//go:build windows

package tray

import (
	"syscall"
	"unsafe"

	"github.com/energye/systray"
)

var (
	shell32          = syscall.NewLazyDLL("shell32.dll")
	user32           = syscall.NewLazyDLL("user32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	shellExecuteW    = shell32.NewProc("ShellExecuteW")
	openClipboard    = user32.NewProc("OpenClipboard")
	closeClipboard   = user32.NewProc("CloseClipboard")
	emptyClipboard   = user32.NewProc("EmptyClipboard")
	setClipboardData = user32.NewProc("SetClipboardData")
	globalAlloc      = kernel32.NewProc("GlobalAlloc")
	globalLock       = kernel32.NewProc("GlobalLock")
	globalUnlock     = kernel32.NewProc("GlobalUnlock")
	globalFree       = kernel32.NewProc("GlobalFree")
)

// OpenBrowser sends the URL to the default handler without flashing a console.
func OpenBrowser(url string) {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return
	}
	target, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return
	}
	shellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, 1)
}

// CopyToClipboard writes Unicode text directly to the Windows clipboard.
func CopyToClipboard(text string) {
	value, err := syscall.UTF16FromString(text)
	if err != nil {
		return
	}
	if opened, _, _ := openClipboard.Call(0); opened == 0 {
		return
	}
	defer closeClipboard.Call()
	emptyClipboard.Call()

	handle, _, _ := globalAlloc.Call(0x0002, uintptr(len(value)*2)) // GMEM_MOVEABLE
	if handle == 0 {
		return
	}
	locked, _, _ := globalLock.Call(handle)
	if locked == 0 {
		globalFree.Call(handle)
		return
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(locked)), len(value)), value)
	globalUnlock.Call(handle)
	if result, _, _ := setClipboardData.Call(13, handle); result == 0 { // CF_UNICODETEXT
		globalFree.Call(handle)
	}
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
