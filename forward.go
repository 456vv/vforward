package vforward

import (
	"errors"
	"io"
	"log"
	"net"
	"sync"
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

// errLog 统一的错误日志输出函数
func errLog(l *log.Logger, format string, v ...interface{}) {
	if l != nil {
		l.Printf(format, v...)
	} else {
		log.Printf(format, v...) // 如果未设置 Logger，则使用默认 log 包
	}
}

// isClosedError 用于检查连接是否由于非异常原因关闭（例如正常关闭或EOF）
func isClosedError(err error) bool {
	if err == nil {
		return false
	}
	// 检查是否是网络连接已关闭或EOF错误
	return errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF)
}
