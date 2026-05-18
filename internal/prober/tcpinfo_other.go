//go:build !linux

package prober

import "net"

func readRetrans(_ *net.TCPConn) (totalRetrans, dataSegsOut uint32, ok bool) {
	return 0, 0, false
}
