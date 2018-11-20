package vforward

import (
	"net"
    "errors"
    "fmt"
    "time"
    "log"
    "io"
    "github.com/456vv/vconn"
    "github.com/456vv/vconnpool"
    "github.com/456vv/vmap/v2"
    "sync/atomic"
    "context"
)

//D2DSwap 数据交换
type D2DSwap struct {
    dd      *D2D                // 引用父结构体 D2D
    conns   *vmap.Map           // 连接存储，方便关闭已经连接的连接

    closed  atomicBool          // 关闭
    used    atomicBool          // 正在使用中
}

//ConnNum 当前正在转发的连接数量
//	int     实时连接数量
func (dds *D2DSwap) ConnNum() int {
    return dds.dd.currUseConns()
}

//Swap 开始数据交换，它是从连接池中读出空闲连接，进行双方交换数据。
//如果你关闭了交换，只是临时关闭的。还可以再次调用Swap。
//永远关闭需要调用 D2D.Close() 的关闭。
//	error       错误
func (dds *D2DSwap) Swap() error {
    if dds.used.setTrue() {
        return errors.New("vforward: 交换数据已经开启不需重复调用")
    }
    dds.closed.setFalse()
    
    var (
     	wait 	time.Duration
     	maxWait	= dds.dd.TryConnTime
     ) 
    for {
        //程序退出
        if dds.closed.isTrue() {
            return nil
        }
        //如果父级被关闭，则子级也执行关闭
        if dds.dd.closed.isTrue() {
            return dds.Close()
        }
        
        //等待
        if dds.dd.acp.ConnNum() <= 0 || dds.dd.bcp.ConnNum() <= 0 || dds.dd.backPooling.isTrue() {
        	wait = delay(wait, maxWait)
        	continue
        }
        wait = 0
        
        atomic.AddInt32(&dds.dd.currUseConn, 1)
        conna, err := dds.dd.aGetConn()
        if err != nil {
            atomic.AddInt32(&dds.dd.currUseConn, -1)
            continue
        }
        atomic.AddInt32(&dds.dd.currUseConn, 1)
        connb, err := dds.dd.bGetConn()
        if err != nil {
            atomic.AddInt32(&dds.dd.currUseConn, -2)
            conna.Close()
            continue
        }

        //记录当前连接
        dds.conns.Set(conna, connb)

        bufSize := dds.dd.ReadBufSize
        if dds.dd.ReadBufSize == 0 {
            bufSize = DefaultReadBufSize
        }
        go func(conna, connb net.Conn, dd *D2D, dds *D2DSwap, bufSize int){
            copyData(conna, connb, bufSize)
            conna.Close()
            atomic.AddInt32(&dd.currUseConn, -1)
        }(conna, connb, dds.dd, dds, bufSize)
        go func(conna, connb net.Conn, dd *D2D, dds *D2DSwap, bufSie int){
            copyData(connb, conna, bufSize)
            connb.Close()

            //删除记录的连接，如果是以 .Close() 关闭的。不再重复删除。
            if !dds.closed.isTrue() {
                dds.conns.Del(conna)
            }

            atomic.AddInt32(&dd.currUseConn, -1)
        }(conna, connb, dds.dd, dds, bufSize)
    }
    return nil
}

//Close 关闭数据交换 .Swap()，你还可以再次使用 .Swap() 启动。
//  返：
//      error       错误
func (dds *D2DSwap) Close() error {
    dds.closed.setTrue()
    dds.conns.Range(func(k, v interface{}) bool {
        if c, ok := k.(io.Closer); ok {
            c.Close()
        }
        if c, ok := v.(io.Closer); ok {
            c.Close()
        }
        return true
    })
    dds.conns.Reset()
    dds.used.setFalse()
    return nil
}

//D2D 内网开放端口，外网无法访问的情况下。内网使用D2D主动连接外网端口。以便外网发来数据转发到内网端口中去。
//	-------------------------------------
//  |     |  ←1  |         |  2→  |     |（1，B收到[A内网-D2D]发来连接）
//  |A内网|  ←4  |A内网-D2D|  ←3  |B外网|（2，B然后向[A内网-D2D]回应数据，数据将转发到A内网。）
//  |     |  5→  |         |  6→  |     |（3，A内网收到数据再发出数据，由[A内网-D2D]转发到B外网。）
//	-------------------------------------
type D2D struct {
    TryConnTime     time.Duration           // 尝试或发起连接时间，可能一方不在线，会一直尝试连接对方。
    MaxConn         int                     // 限制连接最大的数量
    KeptIdeConn     int                     // 保持一方连接数量，以备快速互相连接。
    keptIdeConn     int                     // 保持一方连接数量，以备快速互相连接。
    ReadBufSize     int                     // 交换数据缓冲大小
    Timeout         time.Duration           // 发起连接超时
    ErrorLog        *log.Logger             // 日志

    acp             vconnpool.ConnPool      // A方连接池
    aticker         *time.Ticker            // A方心跳时间
    aaddr           *Addr                   // A方连接地址

    bcp             vconnpool.ConnPool      // B方连接池
    bticker         *time.Ticker            // B方心跳时间
    baddr           *Addr                   // B方连接地址
	
	backPooling		atomicBool				// 确保连接回到池中
	
    currUseConn     int32                   // 当前使用连接数量
    closed          atomicBool              // 关闭
    used            atomicBool              // 正在使用
}

//初始化
func (dd *D2D) init(){
	//保证有一条空闲数量
	dd.keptIdeConn = dd.KeptIdeConn
    if dd.keptIdeConn == 0 {
    	dd.keptIdeConn = 1
    }
    dd.acp.Dialer	= new(net.Dialer)
    dd.acp.IdeConn  = dd.keptIdeConn
    dd.acp.MaxConn	= dd.MaxConn
    
    dd.bcp.Dialer	= new(net.Dialer)
    dd.bcp.IdeConn  = dd.keptIdeConn
    dd.bcp.MaxConn	= dd.MaxConn
}

//Transport 建立连接，支持协议类型："tcp", "tcp4","tcp6", "unix", "unixpacket"。其它还没测试支持："udp", "udp4", "udp6", "ip", "ip4", "ip6", "unixgram"
//	a, b *Addr  A，B方地址
//	*D2DSwap    数据交换
//	error       错误
func (dd *D2D) Transport(a, b *Addr) (*D2DSwap, error) {
    if dd.used.setTrue() {
        return nil, errors.New("vforward: 不能重复调用 D2D.Transport")
    }
    dd.init()
    //A连接
    dd.aaddr 		= a
    dd.aticker 		= time.NewTicker(dd.TryConnTime)
   	dd.acp.LocalAddr= a.Local
    go dd.bufConn(dd.aticker, &dd.acp, a)

    //B连接
    dd.baddr 		= b
    dd.bticker 		= time.NewTicker(dd.TryConnTime)
   	dd.bcp.LocalAddr= b.Local
    go dd.bufConn(dd.bticker, &dd.bcp, b)
    return &D2DSwap{dd: dd, conns: vmap.NewMap()}, nil
}

//Close 关闭D2D
//  返：
//      error   错误
func (dd *D2D) Close() error {
    dd.closed.setTrue()
    dd.acp.Close()
    dd.bcp.Close()
    return nil
}

//当前连接数量
func (dd *D2D) currUseConns() int {
    var i int =  int(atomic.LoadInt32(&dd.currUseConn))
    if i%2 != 0 {
        return (i/2)+1
    }else{
        return (i/2)
    }
}

//快速取得连接
func (dd *D2D) aGetConn() (net.Conn, error) {
    return dd.acp.Get(dd.aaddr.Remote)
}
func (dd *D2D) bGetConn() (net.Conn, error) {
    return dd.bcp.Get(dd.baddr.Remote)
}

//缓冲连接，保持可用的连接数量
func (dd *D2D) bufConn(tick *time.Ticker, cp *vconnpool.ConnPool, addr *Addr){
    for {
        //程序退出
        if dd.closed.isTrue() {
            tick.Stop()
            return
        }
        select {
    	case <- tick.C:
            //1，连接最大限制
            //2，空闲连接限制
            if dd.currUseConns()+cp.ConnNum() >= dd.MaxConn || cp.ConnNumIde(addr.Remote.Network(), addr.Remote.String()) >= dd.keptIdeConn {
            	//检测连接有没有被远程关闭
           		conn, err := cp.Get(addr.Remote)
            	if err != nil {
            		continue
            	}
            	if notifiy, ok := conn.(vconn.CloseNotifier); ok {
            		select {
            		case <-notifiy.CloseNotify():
            		default:
            			cp.Add(addr.Remote, conn)
            		}
            	}
                continue
            }
            
		    ctx := context.Background()
		    if dd.Timeout != 0 {
		        ctx, _ = context.WithTimeout(ctx, dd.Timeout)
		    }
		    ctx = context.WithValue(ctx, "priority", true)
		    dd.backPooling.setTrue()
		    conn, err := cp.DialContext(ctx, addr.Network, addr.Remote.String())
		    if err != nil {
		    	dd.backPooling.setFalse()
                continue
		    }
		    conn.Close()//回到池中
		   	dd.backPooling.setFalse()
		    //cp.Add(conn.RemoteAddr(), conn)
		    
        }
    }
}
func (dd *D2D) logf(funcName string, format string, v ...interface{}){
    if dd.ErrorLog != nil{
        dd.ErrorLog.Printf(fmt.Sprint(funcName, "->", format), v...)
    }
}