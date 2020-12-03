package vforward

import (
	"net"
    "errors"
    "fmt"
    "io"
    "time"
    "log"
    "github.com/456vv/vmap/v2"
    "context"
    "sync/atomic"
)

//L2DSwap 数据交换
type L2DSwap struct {
    ld              *L2D                    // 引用父结构体 L2D
    dialer			net.Dialer				// 连接拨号
    raddr           *Addr                   // 远程地址
    currUseConn     int32                   // 当前使用连接数量
    conns           vmap.Map                // 连接存储，方便关闭已经连接的连接

    used            atomicBool              // 正在使用
    exit            chan bool
}

//当前连接数量
func (T *L2DSwap) currUseConns() int {
    var i int = int(atomic.LoadInt32(&T.currUseConn))
    if i%2 != 0 {
        return (i/2)+1
    }else{
        return (i/2)
    }
}

//ConnNum 当前正在转发的连接数量
//	int     实时连接数量
func (T *L2DSwap) ConnNum() int {
    return T.currUseConns()
}

func (T *L2DSwap) connRemoteTCP(lconn net.Conn) {
    var (
    	ctx		= T.ld.Context
    	cancel 	context.CancelFunc
    )
    if ctx == nil {
    	ctx = context.Background()
    }
    if T.ld.Timeout != 0 {
        ctx, cancel = context.WithTimeout(ctx, T.ld.Timeout)
        defer cancel()
    }

    rconn, err := T.dialer.DialContext(ctx, T.raddr.Network, T.raddr.Remote.String())
    if err != nil {
        //远程连接不通，关闭请求连接
        atomic.AddInt32(&T.currUseConn, -2)
        lconn.Close()
        T.ld.logf("L2DSwap.connRemoteTCP", "本地 %s 向远程 %s 发起请求失败: %v", T.raddr.Local.String(), T.raddr.Remote.String(), err)
        return
    }

    //设置缓冲区大小
    bufSize := T.ld.ReadBufSize
    if bufSize == 0 {
        bufSize = DefaultReadBufSize
    }
    
    //记录连接
    T.conns.Set(lconn, rconn)
	
    //开始交换数据
    go func(T *L2DSwap, rconn, lconn net.Conn, bufSize int){
        copyData(rconn, lconn, bufSize)
        rconn.Close()
        atomic.AddInt32(&T.currUseConn, -1)
    }(T, rconn, lconn, bufSize)
    go func(T *L2DSwap, rconn, lconn net.Conn, bufSize int){
        copyData(lconn, rconn, bufSize)
        lconn.Close()
        atomic.AddInt32(&T.currUseConn, -1)

        //删除连接
        if T.used.isTrue() {
            T.conns.Del(lconn)
        }
    }(T, rconn, lconn, bufSize)
}

type readWriteReply struct{
    lconn       net.PacketConn //upd连接
    laddr       net.Addr

    rconn       net.Conn		//远程连接可能是tcp 或 udp

    closed      atomicBool
}

func (rw *readWriteReply) Close() error {
    if rw.closed.setTrue() {
        return nil
    }
    return rw.rconn.Close()
}

func (T *L2DSwap) connReadReply(rw *readWriteReply){
    //设置缓冲区大小
    var (
    	bufSize 	= T.ld.ReadBufSize
		waitTime	= T.ld.Timeout
    )
    if bufSize == 0 {
        bufSize = DefaultReadBufSize
    }
    if waitTime == 0 {
    	waitTime = time.Second
    }
    

    var tempDelay time.Duration
    var ok bool
    for {
        //设置UDP读取超时，由于UDP是无连接，无法知道对方状态
        if T.ld.Timeout != 0 {
            err := rw.rconn.SetReadDeadline(time.Now().Add(waitTime))
            if err != nil {
                break
            }
        }

        //读取数据
        b := make([]byte, bufSize)
        n, err := rw.rconn.Read(b)
        if err != nil {
            if ne, ok := err.(net.Error); ok && ne.Timeout() {
                break
            }
            //读取出现错误
		    if tempDelay, ok = temporaryError(err, tempDelay, waitTime); ok {
    			continue
    		}
            break
        }
        tempDelay = 0
        
        go rw.lconn.WriteTo(b[:n], rw.laddr)
    }

    T.conns.Del(rw.laddr.String())
    rw.Close()
    
    //退出时减去计数
    atomic.AddInt32(&T.currUseConn, -2)
}

func (T *L2DSwap) keepAvailable() error {
    if l, ok := T.ld.listen.(net.Listener); ok {
        //这里是TCP连接
        
    	var tempDelay time.Duration
        for {
        	rw, err := l.Accept()
    		if err != nil {
                //交级关闭了，子级也关闭
                if T.ld.closed.isTrue() {
                    return T.Close()
                }

    			if tempDelay, ok = temporaryError(err, tempDelay, time.Second); ok {
    				continue
    			}
                T.ld.logf("L2DSwap.keepAvailable", "监听地址 %s， 并等待连接过程中失败: %v", l.Addr(), err)
    			return err
    		}
            tempDelay = 0

            //1,连接数量超过最大限制
            //2,交换已经关闭
            //3,交换不在使用状态
            if (T.ld.MaxConn != 0 && T.currUseConns() >= T.ld.MaxConn) || T.used.isFalse() {
                rw.Close()
                continue
            }

            //计数连接数
            atomic.AddInt32(&T.currUseConn, 2)
            
            go T.connRemoteTCP(rw)
        }
    }else if lconn, ok := T.ld.listen.(net.PacketConn); ok {
        //这里是UDP连接
        bufSize := T.ld.ReadBufSize
        if bufSize == 0 {
            bufSize = DefaultReadBufSize
        }
    	var tempDelay time.Duration
        for  {
            b := make([]byte, bufSize)
            n, laddr, err := lconn.ReadFrom(b)
            if err != nil {
                //交级关闭了，子级也关闭
                if T.ld.closed.isTrue() {
                    return T.Close()
                }

                //读取出现错误
    			if tempDelay, ok = temporaryError(err, tempDelay, time.Second); ok {
    				continue
    			}
                T.ld.logf("L2DSwap.keepAvailable", "监听地址 %s， 并等待连接过程中失败: %v", lconn.LocalAddr(), err)
                return err
            }
            tempDelay = 0

            //如果已经建立连接
            if inf, ok := T.conns.GetHas(laddr.String()); ok {
                rw := inf.(*readWriteReply)
            	go rw.rconn.Write(b[:n])
                continue
            }

            //1,连接数量超过最大限制
            //2,交换已经关闭
            //3,交换不在使用状态
            if (T.ld.MaxConn != 0 && T.currUseConns() >= T.ld.MaxConn) || T.used.isFalse() {
                continue
            }

            //计数连接数
            atomic.AddInt32(&T.currUseConn, 2)
            

            //开始建立连接
            rconn, err := connectUDP(T.raddr)
            if err != nil {
                T.ld.logf("L2DSwap.keepAvailable", "本地UDP向远程 %v 发起请求失败: %v", T.raddr.Remote.String(), err)
            	atomic.AddInt32(&T.currUseConn, -2)
                continue
            }
            rw := &readWriteReply{
                lconn: lconn,
                laddr: laddr,
                rconn: rconn,
            }
            T.conns.Set(laddr.String(), rw)

            go T.connReadReply(rw)
            go rw.rconn.Write(b[:n])
        }
    }
    return nil
}

//Swap 开始数据交换，当有TCP/UDP请求发来的时候，将会转发连接。
//如果你关闭了交换，只是临时关闭的。还可以再次调用Swap。
//永远关闭需要调用 L2D.Close() 的关闭。
//	error       错误
func (T *L2DSwap) Swap() error {
    if T.used.setTrue() {
        return errors.New("vforward: 交换数据已经开启不需重复调用")
    }
    <- T.exit
    return nil
}

//Close 关闭交换
//	error   错误
func (T *L2DSwap) Close() error {
    if T.used.setFalse() {
        return nil
    }
    T.conns.Range(func(k, v interface{}) bool {
        if c, ok := k.(io.Closer); ok {
            c.Close()
        }
        if c, ok := v.(io.Closer); ok {
            c.Close()
        }
        return true
    })
    T.conns.Reset()
    T.exit <- true
    return nil
}

//L2D 是在内网或公网都可以使用，配合D2D或L2L使用功能更自由。L2D功能主要是转发连接（端口转发）。
//	-------------------------------------
//  |     |  1→  |       |  2→  |     |（1，B收到A发来数据）
//  |A端口|  ←4  |L2D转发|  ←3  |B端口|（2，然后向A回应数据）
//  |     |  5→  |       |  6→  |     |（3，B然后再收到A数据）
//	-------------------------------------
type L2D struct {
    MaxConn         int                     // 限制连接最大的数量
    ReadBufSize     int                     // 交换数据缓冲大小
    Timeout         time.Duration           // 发起连接超时
    ErrorLog        *log.Logger             // 日志
    Context			context.Context

    listen          interface{}             // 监听

    closed          atomicBool              // 关闭
    used            atomicBool              // 正在使用
}

//Transport 支持协议类型："tcp", "tcp4","tcp6", "unix", "unixpacket", "udp", "udp4", "udp6", "ip", "ip4", "ip6", "unixgram"
//	raddr, laddr *Addr  转发IP，监听IP地址
//	*L2DSwap    交换数据
//	error       错误
func (T *L2D) Transport(raddr, laddr *Addr) (*L2DSwap, error) {
    if T.used.setTrue() {
        return nil, errors.New("vforward: 不能重复调用 L2D.Transport")
    }

    var err error
    T.listen, err = connectListen(laddr)
    if err != nil {
        return nil, err
    }
    
    lds := &L2DSwap{ld: T, raddr: raddr,  dialer: net.Dialer{LocalAddr: raddr.Local}, exit: make(chan bool)}
    //保持连接处于监听状态
    go lds.keepAvailable()
    return lds, nil
}

//Close 关闭L2D
//	error   错误
func (T *L2D) Close() error {
    T.closed.setTrue()
    if T.listen != nil {
        return T.listen.(io.Closer).Close()
    }
    return nil
}

func (T *L2D) logf(funcName string, format string, v ...interface{}){
    if T.ErrorLog != nil{
        T.ErrorLog.Printf(fmt.Sprint(funcName, "->", format), v...)
    }
}

