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
    conns           *vmap.Map               // 连接存储，方便关闭已经连接的连接

    used            atomicBool              // 正在使用
    exit            chan bool
}

//当前连接数量
func (lds *L2DSwap) currUseConns() int {
    var i int = int(atomic.LoadInt32(&lds.currUseConn))
    if i%2 != 0 {
        return (i/2)+1
    }else{
        return (i/2)
    }
}

//ConnNum 当前正在转发的连接数量
//	int     实时连接数量
func (lds *L2DSwap) ConnNum() int {
    return lds.currUseConns()
}

func (lds *L2DSwap) connTCP(lconn net.Conn) {
    var (
    	ctx		= context.Background()
    	cancel 	context.CancelFunc
    )
    if lds.ld.Timeout != 0 {
        ctx, cancel = context.WithTimeout(ctx, lds.ld.Timeout)
        defer cancel()
    }

    rconn, err := lds.dialer.DialContext(ctx, lds.raddr.Network, lds.raddr.Remote.String())
    if err != nil {
        //远程连接不通，关闭请求连接
        atomic.AddInt32(&lds.currUseConn, -2)
        lconn.Close()
        lds.ld.logf("L2DSwap.connTCP", "本地 %s 向远程 %s 发起请求失败: %v", lds.raddr.Local.String(), lds.raddr.Remote.String(), err)
        return
    }

    //设置缓冲区大小
    bufSize := lds.ld.ReadBufSize
    if lds.ld.ReadBufSize == 0 {
        bufSize = DefaultReadBufSize
    }


    //记录连接
    lds.conns.Set(lconn, rconn)

    //开始交换数据
    go func(lds *L2DSwap, rconn, lconn net.Conn, bufSize int){
        copyData(rconn, lconn, bufSize)
        rconn.Close()
        atomic.AddInt32(&lds.currUseConn, -1)
    }(lds, rconn, lconn, bufSize)
    go func(lds *L2DSwap, rconn, lconn net.Conn, bufSize int){
        copyData(lconn, rconn, bufSize)
        lconn.Close()
        atomic.AddInt32(&lds.currUseConn, -1)

        //删除连接
        if lds.used.isTrue() {
            lds.conns.Del(lconn)
        }
    }(lds, rconn, lconn, bufSize)
}

type readWriteUDP struct{
    lconn       net.PacketConn //upd连接
    laddr       net.Addr

    rconn       net.Conn		//远程连接可能是tcp 或 udp

    closed      atomicBool
}

func (rw *readWriteUDP) Close() error {
    if rw.closed.setTrue() {
        return nil
    }
    return rw.rconn.Close()
}

func (lds *L2DSwap) connReadUDP(rw *readWriteUDP){
    //设置缓冲区大小
    bufSize := lds.ld.ReadBufSize
    if lds.ld.ReadBufSize == 0 {
        bufSize = DefaultReadBufSize
    }

    var tempDelay time.Duration
    var ok bool
    for {
        //设置UDP读取超时，由于UDP是无连接，无法知道对方状态
        if lds.ld.Timeout != 0 {
            err := rw.rconn.SetReadDeadline(time.Now().Add(lds.ld.Timeout))
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
		    if tempDelay, ok = temporaryError(err, tempDelay, time.Second); ok {
    			continue
    		}
            break
        }//if
        tempDelay = 0
        
        go rw.lconn.WriteTo(b[:n], rw.laddr)
        //rw.toLocal<-b[:n]
    }

    lds.conns.Del(rw.laddr)
    rw.Close()
    
    //退出时减去计数
    atomic.AddInt32(&lds.currUseConn, -2)
}


func (lds *L2DSwap) keepAvailable() error {
    if l, ok := lds.ld.listen.(net.Listener); ok {
        //这里是TCP连接
        
    	var tempDelay time.Duration
        for {
        	rw, err := l.Accept()
    		if err != nil {
                //交级关闭了，子级也关闭
                if lds.ld.closed.isTrue() {
                    return lds.Close()
                }

    			if tempDelay, ok = temporaryError(err, tempDelay, time.Second); ok {
    				continue
    			}
                lds.ld.logf("L2DSwap.keepAvailable", "监听地址 %s， 并等待连接过程中失败: %v", l.Addr(), err)
    			return err
    		}//if
            tempDelay = 0

            //1,连接数量超过最大限制
            //2,交换已经关闭
            //3,交换不在使用状态
            if lds.currUseConns() >= lds.ld.MaxConn || lds.used.isFalse() {
                rw.Close()
                continue
            }

            //计数连接数
            atomic.AddInt32(&lds.currUseConn, 2)
            
            go lds.connTCP(rw)
        }
    }else if lconn, ok := lds.ld.listen.(net.PacketConn); ok {
        //这里是UDP连接
        bufSize := lds.ld.ReadBufSize
        if lds.ld.ReadBufSize == 0 {
            bufSize = DefaultReadBufSize
        }
    	var tempDelay time.Duration
        for  {
            b := make([]byte, bufSize)
            n, laddr, err := lconn.ReadFrom(b)
            if err != nil {
                //交级关闭了，子级也关闭
                if lds.ld.closed.isTrue() {
                    return lds.Close()
                }

                //读取出现错误
    			if tempDelay, ok = temporaryError(err, tempDelay, time.Second); ok {
    				continue
    			}
                lds.ld.logf("L2DSwap.keepAvailable", "监听地址 %s， 并等待连接过程中失败: %v", lconn.LocalAddr(), err)
                return err
            }//if
            tempDelay = 0


            //如果已经建立连接
            if inf, ok := lds.conns.GetHas(laddr); ok {
                rw := inf.(*readWriteUDP)
            	go rw.rconn.Write(b[:n])
                continue
            }

            //1,连接数量超过最大限制
            //2,交换已经关闭
            //3,交换不在使用状态
            if lds.currUseConns() >= lds.ld.MaxConn || lds.used.isFalse() {
                continue
            }

            //计数连接数
            atomic.AddInt32(&lds.currUseConn, 2)
            

            //开始建立连接
            rconn, err := connectUDP(lds.raddr)
            if err != nil {
                lds.ld.logf("L2DSwap.keepAvailable", "本地UDP向远程 %v 发起请求失败: %v", lds.raddr.Remote.String(), err)
            	atomic.AddInt32(&lds.currUseConn, -2)
                continue
            }
            rw := &readWriteUDP{
                lconn: lconn,
                laddr: laddr,
                rconn: rconn,
            }
            lds.conns.Set(laddr, rw)

            go lds.connReadUDP(rw)
            go rw.rconn.Write(b[:n])
        }
    }
    return nil
}

//Swap 开始数据交换，当有TCP/UDP请求发来的时候，将会转发连接。
//如果你关闭了交换，只是临时关闭的。还可以再次调用Swap。
//永远关闭需要调用 L2D.Close() 的关闭。
//	error       错误
func (lds *L2DSwap) Swap() error {
    if lds.used.setTrue() {
        return errors.New("vforward: 交换数据已经开启不需重复调用")
    }
    <- lds.exit
    return nil
}

//Close 关闭交换
//	error   错误
func (lds *L2DSwap) Close() error {
    if lds.used.setFalse() {
        return nil
    }
    lds.conns.Range(func(k, v interface{}) bool {
        if c, ok := k.(io.Closer); ok {
            c.Close()
        }
        if c, ok := v.(io.Closer); ok {
            c.Close()
        }
        return true
    })
    lds.conns.Reset()
    lds.exit <- true
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

    listen          interface{}             // 监听

    closed          atomicBool              // 关闭
    used            atomicBool              // 正在使用
}

//Transport 支持协议类型："tcp", "tcp4","tcp6", "unix", "unixpacket", "udp", "udp4", "udp6", "ip", "ip4", "ip6", "unixgram"
//  参：
//      raddr, laddr *Addr  转发IP，监听IP地址
//  返：
//      *L2DSwap    交换数据
//      error       错误
func (ld *L2D) Transport(raddr, laddr *Addr) (*L2DSwap, error) {
    if ld.used.setTrue() {
        return nil, errors.New("vforward: 不能重复调用 L2D.Transport")
    }

    var err error
    ld.listen, err = connectListen(laddr)
    if err != nil {
        return nil, err
    }
    lds := &L2DSwap{ld: ld, raddr: raddr, conns: vmap.NewMap(), dialer: net.Dialer{LocalAddr: raddr.Local}, exit: make(chan bool)}
    //保持连接处于监听状态
    go lds.keepAvailable()
    return lds, nil
}

//Close 关闭L2D
//  返：
//      error   错误
func (ld *L2D) Close() error {
    ld.closed.setTrue()
    if ld.listen != nil {
        return ld.listen.(io.Closer).Close()
    }
    return nil
}

func (ld *L2D) logf(funcName string, format string, v ...interface{}){
    if ld.ErrorLog != nil{
        ld.ErrorLog.Printf(fmt.Sprint(funcName, "->", format), v...)
    }
}

