package vforward

import (
	"errors"
	"io"
	"net"
	"strings"
	"sync"

	"github.com/libp2p/go-reuseport"
)

const DefaultReadBufSize = 64 << 10 // 增加到64KB，提升大吞吐量性能

// Option 封装地址信息
type Option struct {
	Local  net.Addr
	Remote net.Addr
	Verify func(net.Conn) bool
}

// 字节池，减少GC开销
var bufPool = sync.Pool{
	New: func() any {
		return make([]byte, DefaultReadBufSize)
	},
}

// copyData 优化的数据交换
func copyData(dst io.Writer, src io.ReadCloser, bufSize int) (int64, error) {
	if bufSize == 0 {
		bufSize = DefaultReadBufSize
	}
	// 如果 BufSize 小于等于默认池大小，则从对象池获取缓冲区
	if bufSize <= DefaultReadBufSize {
		buf := bufPool.Get().([]byte)
		defer bufPool.Put(buf)
		// 注意：如果传入的 bufSize 小于 DefaultReadBufSize，这里会使用切片
		return io.CopyBuffer(dst, src, buf[:bufSize])
	}
	// 否则分配临时大缓冲区
	return io.CopyBuffer(dst, src, make([]byte, bufSize))
}

func connectListen(addr net.Addr) (any, error) {
	switch addr.Network() {
	case "tcp", "tcp4", "tcp6", "unix", "unixpacket":
		return reuseport.Listen(addr.Network(), addr.String())
	case "udp", "udp4", "udp6", "ip", "ip4", "ip6", "unixgram":
		return reuseport.ListenPacket(addr.Network(), addr.String())
	default:
		if strings.HasPrefix(addr.Network(), "ip") && len(addr.Network()) > 3 {
			return reuseport.ListenPacket(addr.Network(), addr.String())
		}
	}
	return nil, errors.New("vforward: 监听地址类型是未知的")
}
