package spglobal

import "os"

func stopProcess(p *os.Process) error {
	return p.Kill()
}
