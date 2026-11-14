//go:build windows

package identity

import (
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// osUPN lee el User Principal Name de la sesión con la API de Windows. Es lo
// mismo que devuelve `whoami /upn`, pero sin lanzar un proceso: LCS.exe no
// tiene consola y cada proceso hijo abriría una ventana.
func osUPN() string {
	n := uint32(256)
	for i := 0; i < 3; i++ {
		buf := make([]uint16, n)
		err := windows.GetUserNameEx(windows.NameUserPrincipal, &buf[0], &n)
		if err == nil {
			return windows.UTF16ToString(buf)
		}
		if err != windows.ERROR_MORE_DATA {
			return ""
		}
	}
	return ""
}

// whoamiUPN es el respaldo: `whoami /upn` con la ventana oculta.
func whoamiUPN() string {
	cmd := exec.Command("whoami", "/upn")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}

	done := make(chan []byte, 1)
	go func() {
		out, _ := cmd.Output()
		done <- out
	}()
	select {
	case out := <-done:
		return strings.TrimSpace(string(out))
	case <-time.After(5 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return ""
	}
}
