//go:build linux

package feed

import "syscall"

// browserProcAttr kills the browser with the bot, as chromedp does on Linux when it starts the browser itself.
func browserProcAttr(cred *syscall.Credential) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Credential: cred, Pdeathsig: syscall.SIGKILL}
}
