//go:build !linux

package feed

import "syscall"

func browserProcAttr(cred *syscall.Credential) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Credential: cred}
}
