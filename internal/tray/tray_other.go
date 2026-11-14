//go:build !windows

package tray

import (
	"fmt"
	"os/exec"
	"runtime"
)

func OpenBrowser(url string) {
	cmd := "xdg-open"
	if runtime.GOOS == "darwin" {
		cmd = "open"
	}
	_ = exec.Command(cmd, url).Start()
}

func CopyToClipboard(text string) { fmt.Println(text) }

func Run(openURL func(), onQuit func()) {
	openURL()
	if onQuit != nil {
		onQuit()
	}
}

func SetURLProvider(fn func() string) {}
