//go:build unix

package link

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// shareAddress lets other programs on this machine bind the discovery
// port too, as every Link peer does. Without it, the second of
// crossroads and Ableton Live to start cannot listen at all.
func shareAddress(_, _ string, c syscall.RawConn) error {
	var err error
	if cerr := c.Control(func(fd uintptr) {
		err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
		if err == nil {
			err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		}
	}); cerr != nil {
		return cerr
	}
	return err
}
