//go:build !windows

package spglobal

import (
	"os"
	"syscall"
)

func stopProcess(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}
