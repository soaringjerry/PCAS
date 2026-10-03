//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// The messages are UTF-8; a classic console window defaults to the system code page.
func prepareConsole() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	_, _, _ = kernel32.NewProc("SetConsoleOutputCP").Call(65001)
	_, _, _ = kernel32.NewProc("SetConsoleCP").Call(65001)
}

func openBrowser(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
