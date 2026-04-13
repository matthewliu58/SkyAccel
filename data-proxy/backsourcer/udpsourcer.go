package backsourcer

import (
	"net"
	"time"
)

// UDPProtocol UDP 协议实现（和 TCP 结构完全一致）
type UDPProtocol struct {
	dialTimeout time.Duration
	ioTimeout   time.Duration
}

// NewUDPProtocol 创建 UDP 协议实例
func NewUDPProtocol(dialTimeout, ioTimeout time.Duration) *UDPProtocol {
	return &UDPProtocol{
		dialTimeout: dialTimeout,
		ioTimeout:   ioTimeout,
	}
}

// DoRequest 实现 OriginProtocol 接口（和 TCP 方法签名一样）
func (u *UDPProtocol) DoRequest(addr string, reqData []byte) ([]byte, error) {
	// 建立 UDP 连接（Dial 依然可用，UDP 会模拟“连接”状态）
	conn, err := net.DialTimeout("udp", addr, u.dialTimeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// 设置超时
	_ = conn.SetDeadline(time.Now().Add(u.ioTimeout))

	// 发送数据（和 TCP 一模一样）
	_, err = conn.Write(reqData)
	if err != nil {
		return nil, err
	}

	// 读取回包（UDP 最大包 65535）
	buf := make([]byte, 65535)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}

	return buf[:n], nil
}
