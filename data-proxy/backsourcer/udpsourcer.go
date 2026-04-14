package backsourcer

import (
	"net"
	"time"
)

type UDPProtocol struct {
	dialTimeout time.Duration
	ioTimeout   time.Duration
}

func NewUDPProtocol(dialTimeout, ioTimeout time.Duration) *UDPProtocol {
	return &UDPProtocol{
		dialTimeout: dialTimeout,
		ioTimeout:   ioTimeout,
	}
}

func (u *UDPProtocol) DoRequest(addr string, reqData []byte) ([]byte, error) {

	conn, err := net.DialTimeout("udp", addr, u.dialTimeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(u.ioTimeout))

	_, err = conn.Write(reqData)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, 65535)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}

	return buf[:n], nil
}
