//go:build unix

package lifecycle

import (
	"os"
	"syscall"
)

func defaultSignals(opts *Options) []os.Signal {
	var sigs []os.Signal
	if opts.EnableSIGHUP {
		sigs = append(sigs, syscall.SIGHUP)
	}
	if opts.EnableSIGINT {
		sigs = append(sigs, syscall.SIGINT)
	}
	if opts.EnableSIGTERM {
		sigs = append(sigs, syscall.SIGTERM)
	}
	if opts.EnableSIGQUIT {
		sigs = append(sigs, syscall.SIGQUIT)
	}
	return sigs
}
