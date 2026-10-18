package link

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// shareAddress lets other programs on this machine bind the discovery
// port too, as every Link peer does. Without it, the second of
// crossroads and Ableton Live to start cannot listen at all.
func shareAddress(_, _ string, c syscall.RawConn) error {
	var err error
	if cerr := c.Control(func(fd uintptr) {
		err = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
	}); cerr != nil {
		return cerr
	}
	return err
}
