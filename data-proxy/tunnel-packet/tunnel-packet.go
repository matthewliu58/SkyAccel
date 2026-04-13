package tunnel_packet

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

const (
	BufferSize = 5120 // 固定 5K 缓冲区
	HeaderSize = 20   // 包头长度 20 字节 (1 + 4*4 + 2 + 1 padding)
	MaxHops    = 4    // 总跳数固定 4 跳
)

var ErrInvalidHeader = errors.New("invalid packet header: length too short")

// Packet 多级代理合并分包总包结构
type Packet struct {
	Buf []byte // 固定 5K 内存

	HopPos     byte      // 当前所在跳数 0~3
	HopIP      [4]uint32 // 4 跳节点 IPv4
	PayloadLen uint16    // 子报文总长度

	wp int // payload 内部写入游标
}

// SubPacket 单个用户子报文
type SubPacket struct {
	UserID uint32
	Data   []byte
}

// NewPacket 创建一个预分配 5K 的空包
func NewPacket() *Packet {
	return &Packet{
		Buf: make([]byte, BufferSize),
		wp:  HeaderSize,
	}
}

// SetHopIP 设置第 n 跳 IP（0=第一跳，1=第二跳，2=第三跳）
func (p *Packet) SetHopIP(hopIdx int, ip net.IP) {
	if hopIdx < 0 || hopIdx >= MaxHops {
		return
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return
	}
	p.HopIP[hopIdx] = binary.BigEndian.Uint32(ip4)
}

// SetHopPos 手动设置当前跳数（你要的函数）
func (p *Packet) SetHopPos(pos byte) {
	if pos >= 0 && pos < MaxHops {
		p.HopPos = pos
	}
}

// AppendUserPacket 追加一个用户子包
func (p *Packet) AppendUserPacket(userID uint32, data []byte) bool {
	subSize := 4 + 2 + len(data)
	if p.wp+subSize > BufferSize {
		return false
	}

	binary.BigEndian.PutUint32(p.Buf[p.wp:], userID)
	p.wp += 4

	binary.BigEndian.PutUint16(p.Buf[p.wp:], uint16(len(data)))
	p.wp += 2

	copy(p.Buf[p.wp:], data)
	p.wp += len(data)

	p.PayloadLen = uint16(p.wp - HeaderSize)
	return true
}

// SerializeHead 将包头结构写回 Buf 前 19 字节
func (p *Packet) SerializeHead() {
	b := p.Buf[:HeaderSize]

	b[0] = p.HopPos

	binary.BigEndian.PutUint32(b[1:5], p.HopIP[0])
	binary.BigEndian.PutUint32(b[5:9], p.HopIP[1])
	binary.BigEndian.PutUint32(b[9:13], p.HopIP[2])
	binary.BigEndian.PutUint32(b[13:17], p.HopIP[3])

	binary.BigEndian.PutUint16(b[17:19], p.PayloadLen)
}

// TotalBytes 实际使用总长度
func (p *Packet) TotalBytes() int {
	return HeaderSize + int(p.PayloadLen)
}

// GetHeader 返回裸包头 15 字节
//func (p *Packet) GetHeader() []byte {
//	if len(p.Buf) < HeaderSize {
//		return nil
//	}
//	return p.Buf[:HeaderSize]
//}

// ParseHeader 只解析包头（19字节），不解析payload
// 专门用于转发层：快速获取 HopPos / 下一跳IP / 总长
func ParseHeader(raw []byte) (*Packet, error) {
	// 包头都不够长，直接返回
	if len(raw) < HeaderSize {
		return nil, ErrInvalidHeader
	}

	p := &Packet{
		Buf: make([]byte, HeaderSize), // 只存头，不分配5K
	}
	// 只拷贝前19字节
	copy(p.Buf, raw[:HeaderSize])

	b := p.Buf[:HeaderSize]

	// 只解析包头字段
	p.HopPos = b[0]
	p.HopIP[0] = binary.BigEndian.Uint32(b[1:5])
	p.HopIP[1] = binary.BigEndian.Uint32(b[5:9])
	p.HopIP[2] = binary.BigEndian.Uint32(b[9:13])
	p.HopIP[3] = binary.BigEndian.Uint32(b[13:17])
	p.PayloadLen = binary.BigEndian.Uint16(b[17:19])

	// 不解析 payload
	return p, nil
}

// Parse 从收到的字节流解析整包和所有子包
func Parse(raw []byte) (*Packet, []SubPacket, error) {
	if len(raw) < HeaderSize {
		return nil, nil, fmt.Errorf("raw data length %d < header size %d", len(raw), HeaderSize)
	}

	p := NewPacket()
	copy(p.Buf, raw)

	b := p.Buf[:HeaderSize]

	p.HopPos = b[0]
	p.HopIP[0] = binary.BigEndian.Uint32(b[1:5])
	p.HopIP[1] = binary.BigEndian.Uint32(b[5:9])
	p.HopIP[2] = binary.BigEndian.Uint32(b[9:13])
	p.HopIP[3] = binary.BigEndian.Uint32(b[13:17])
	p.PayloadLen = binary.BigEndian.Uint16(b[17:19])

	payloadEnd := HeaderSize + int(p.PayloadLen)
	if payloadEnd > len(raw) || payloadEnd > BufferSize {
		return nil, nil, fmt.Errorf("invalid payload length: %d (raw: %d, buffer: %d)", p.PayloadLen, len(raw), BufferSize)
	}
	payload := p.Buf[HeaderSize:payloadEnd]

	var subs []SubPacket
	r := bytes.NewReader(payload)

	for {
		var userID uint32
		if err := binary.Read(r, binary.BigEndian, &userID); err != nil {
			break
		}

		var subLen uint16
		if err := binary.Read(r, binary.BigEndian, &subLen); err != nil {
			break
		}

		data := make([]byte, subLen)
		if _, err := r.Read(data); err != nil {
			break
		}

		subs = append(subs, SubPacket{
			UserID: userID,
			Data:   data,
		})
	}

	return p, subs, nil
}

// HopIPToNet 将 uint32 格式 IP 转为 net.IP
func HopIPToNet(ip uint32) net.IP {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, ip)
	return b
}
