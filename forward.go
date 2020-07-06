package vforward

import (
	"net"
    "errors"
    "io"
    "strings"
    "time"
    "sync/atomic"
)
const DefaultReadBufSize int = 4096         // 默认交换数据缓冲大小

//Addr 地址包含本地，远程
type Addr struct {
    Network string
	Local, Remote   net.Addr        // 本地，远程
}

//响应完成设置
type atomicBool int32
func (T *atomicBool) isTrue() bool 	{ return atomic.LoadInt32((*int32)(T)) != 0 }
func (T *atomicBool) isFalse() bool	{ return atomic.LoadInt32((*int32)(T)) != 1 }
func (T *atomicBool) setTrue() bool	{ return !atomic.CompareAndSwapInt32((*int32)(T), 0, 1)}
func (T *atomicBool) setFalse() bool{ return !atomic.CompareAndSwapInt32((*int32)(T), 1, 0)}


func temporaryError(err error, wait time.Duration, maxDelay time.Duration)(time.Duration, bool) {
    if ne, ok := err.(net.Error); ok && ne.Temporary() {
    	return delay(wait, maxDelay), true
    }
    return wait, false
}

func delay(wait, maxDelay time.Duration) time.Duration {
	if wait == 0 {
		wait = (maxDelay/100)
	}else{
		wait *= 2
	}
	if wait >= maxDelay {
	    wait = maxDelay
	}
	time.Sleep(wait)
    return wait
}

func copyData(dst io.Writer, src io.ReadCloser, bufferSize int)(written int64, err error){
    defer src.Close()
    buf := make([]byte, bufferSize)
    return io.CopyBuffer(dst, src, buf)
}

func connectListen(addr *Addr) (interface{}, error) {
    switch addr.Network {
	case "tcp", "tcp4", "tcp6", "unix", "unixpacket":
        return net.Listen(addr.Network, addr.Local.String())
	case "udp", "udp4", "udp6", "ip", "ip4", "ip6", "unixgram":
        return net.ListenPacket(addr.Network, addr.Local.String())
    default:
        if strings.HasPrefix(addr.Network, "ip:") && len(addr.Network) > 3 {
            return net.ListenPacket(addr.Network, addr.Local.String())
        }
    }
    return nil, errors.New("vforward: 监听地址类型是未知的")
}

func connectUDP(addr *Addr) (net.Conn, error) {
	switch addr.Network {
	case "udp", "udp4", "udp6":
		return net.DialUDP(addr.Network, nil, addr.Remote.(*net.UDPAddr))
	case "ip", "ip4", "ip6":
		return net.DialIP(addr.Network, nil, addr.Remote.(*net.IPAddr))
	case "unix", "unixpacket", "unixgram":
		return net.DialUnix(addr.Network, nil, addr.Remote.(*net.UnixAddr))
    default:
        if strings.HasPrefix(addr.Network, "ip:") && len(addr.Network) > 3 {
	    	return net.DialIP(addr.Network, nil, addr.Remote.(*net.IPAddr))
        }
	}
    return nil, errors.New("vforward: 远程地址类型是未知的")
}
