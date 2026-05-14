package util

import (
	"net"
	"sync/atomic"
)

var reqSeq uint64

func GenShortReqID(clientIP string) uint32 {

	seq := atomic.AddUint64(&reqSeq, 1)

	ip := net.ParseIP(clientIP).To4()
	ipSegment := uint32(0)
	if ip != nil {
		ipSegment = uint32(ip[3])
	}

	id := (ipSegment << 24) | (uint32(seq) & 0xFFFFFF)
	return id
}
