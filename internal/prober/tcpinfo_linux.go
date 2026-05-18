//go:build linux

package prober

import (
	"net"

	"golang.org/x/sys/unix"
)

// readRetrans reads kernel TCP_INFO for c and returns (total retransmitted
// segments, total data segments sent). Both counts are cumulative for the
// connection's lifetime, which is fine because DisableKeepAlives=true on
// the prober's transport means each probe gets a fresh conn.
//
// MUST be called while the fd is still open. The HTTP transport closes the
// underlying TCPConn the moment the response body is drained, so the prober
// reads this from inside an intercepted Close() on a wrapping conn rather
// than after the request returns.
func readRetrans(c *net.TCPConn) (totalRetrans, dataSegsOut uint32, ok bool) {
	rc, err := c.SyscallConn()
	if err != nil {
		return 0, 0, false
	}
	var (
		info    *unix.TCPInfo
		sockErr error
	)
	if err := rc.Control(func(fd uintptr) {
		info, sockErr = unix.GetsockoptTCPInfo(int(fd), unix.IPPROTO_TCP, unix.TCP_INFO)
	}); err != nil || sockErr != nil || info == nil {
		return 0, 0, false
	}
	return info.Total_retrans, info.Data_segs_out, true
}
