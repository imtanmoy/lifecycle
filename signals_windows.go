//go:build windows

package lifecycle

import "os"

func defaultSignals(opts *Options) []os.Signal {
	// Windows reliably supports os.Interrupt; SIGTERM may be delivered by some environments
	return []os.Signal{os.Interrupt}
}
