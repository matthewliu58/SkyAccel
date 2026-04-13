package util

import (
	"encoding/binary"
	"net"
	"sync/atomic"
	"time"
)

// 全局原子自增序号
var seq uint64

// GenShortReqID 返回 uint32 格式的唯一ID（你要的！）
// 结构：IP后2字节 + 时间低8位 + 自增序号低6位
func GenShortReqID(clientIP string) uint32 {
	// 1. 原子自增序号
	no := atomic.AddUint64(&seq, 1)

	// 2. 时间戳取低 8 位
	now := time.Now().Unix()
	timeByte := byte(now & 0xFF)

	// 3. 解析IP，取后 2 字节
	ip := net.ParseIP(clientIP).To4()
	ip16 := binary.BigEndian.Uint16(ip[2:4])

	// 4. 组合成 32 位唯一ID
	// 高16位：IP后两位
	// 中8位：时间戳低8位
	// 低8位：序号低8位
	id := (uint32(ip16) << 16) | (uint32(timeByte) << 8) | (uint32(no) & 0xFF)

	return id
}
