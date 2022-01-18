package vforward

import (
	"net"
    "errors"
    "fmt"
    "time"
    "log"
    "io"
    "github.com/456vv/vconnpool/v2"
    "github.com/456vv/vmap/v2"
    "sync/atomic"
    "context"
)

//D2DSwap 数据交换
type D2DSwap struct {
	Verify	func(a, b net.Conn) (net.Conn, net.Conn, error)	// 数据交换前对双方连接操作，可以现实验证之类
    dd      *D2D                // 引用父结构体 D2D
    conns	vmap.Map			// 连接存储，方便关闭已经连接的连接
    closed  atomicBool          // 关闭
    used    atomicBool          // 正在使用中
}

//ConnNum 当前正在转发的连接数量
//	int     实时连接数量
func (T *D2DSwap) ConnNum() int {
    return T.dd.currUseConns()
}

//Swap 开始数据交换，它是从连接池中读出空闲连接，进行双方交换数据。
//如果你关闭了交换，只是临时关闭的。还可以再次调用Swap。
//永远关闭需要调用 D2D.Close() 的关闭。
//	error       错误
func (T *D2DSwap) Swap() error {
    if T.used.setTrue() {
        return errors.New("vforward: 交换数据已经开启不需重复调用")
    }
    T.closed.setFalse()
    
    var (
     	wait 	time.Duration
     	maxWait	= T.dd.TryConnTime
     ) 
    for {
        //程序退出
        if T.closed.isTrue() {
            return nil
        }
        
        //如果父级被关闭，则子级也执行关闭
        if T.dd.closed.isTrue() {
            return T.Close()
        }
        
        //等待
        if T.dd.acp.ConnNum() <= 0 || T.dd.bcp.ConnNum() <= 0 || T.dd.backPooling.isTrue() {
        	wait = delay(wait, maxWait)
        	continue
        }
        wait = 0
        
        atomic.AddInt32(&T.dd.currUseConn, 1)
        conna, err := T.dd.aGetConn()
        if err != nil {
            atomic.AddInt32(&T.dd.currUseConn, -1)
            continue
        }
        atomic.AddInt32(&T.dd.currUseConn, 1)
        connb, err := T.dd.bGetConn()
        if err != nil {
            atomic.AddInt32(&T.dd.currUseConn, -2)
            conna.Close()
            continue
        }
        
        bufSize := T.dd.ReadBufSize
        if bufSize == 0 {
            bufSize = DefaultReadBufSize
        }
        
        go func(conna, connb net.Conn, dd *D2D, T *D2DSwap, bufSize int){
        	
            defer atomic.AddInt32(&dd.currUseConn, -2)
            
            if T.closed.isTrue() {
            	conna.Close()
            	connb.Close()
            	return
            }
            
        	//记录当前连接
        	T.conns.Set(conna, connb)
            defer T.conns.Del(conna)
            
	        //----------------------------
        	var err error
        	if T.Verify != nil {
        		conna, connb, err = T.Verify(conna, connb)
        		if err != nil {
            		T.dd.logf(err.Error())
        			return
        		}
        	}
        	
	        //----------------------------
	        go func(conna, connb net.Conn, dd *D2D, T *D2DSwap, bufSize int){
	            copyData(conna, connb, bufSize)
	            conna.Close()
	        }(conna, connb, dd, T, bufSize)
	        
	        //----------------------------
            copyData(connb, conna, bufSize)
            connb.Close()
	        
        }(conna, connb, T.dd, T, bufSize)
    }
}

//Close 关闭数据交换 .Swap()，你还可以再次使用 .Swap() 启动。
//	error       错误
func (T *D2DSwap) Close() error {
    T.closed.setTrue()
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
    T.used.setFalse()
    return nil
}

//D2D 内网开放端口，外网无法访问的情况下。内网使用D2D主动连接外网端口。以便外网发来数据转发到内网端口中去。
//	-------------------------------------
//  |     |  ←1  |   |  2→  |     |（1，A和B收到[D2D]发来连接）
//  |A内网|  ←4  |D2D|  ←3  |B外网|（2，B然后向[D2D]回应数据，数据将转发到A内网。）
//  |     |  5→  |   |  6→  |     |（3，A内网收到数据再发出数据，由[D2D]转发到B外网。）
//	-------------------------------------
type D2D struct {
    TryConnTime     time.Duration           // 尝试或发起连接时间，可能一方不在线，会一直尝试连接对方。
    MaxConn         int                     // 限制连接最大的数量
    KeptIdeConn     int                     // 保持一方连接数量，以备快速互相连接。
    IdeTimeout      time.Duration           // 空闲连接超时
    ReadBufSize     int                     // 交换数据缓冲大小
    Timeout         time.Duration           // 发起连接超时
    ErrorLog        *log.Logger             // 日志
    Context			context.Context			// 上下文

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
func (T *D2D) init(){
    //保持一个连接在池中
	if T.KeptIdeConn == 0 {
		T.KeptIdeConn = 1
	}
    T.acp.Dialer	= new(net.Dialer)
    T.acp.MaxConn	= T.MaxConn
    T.acp.IdeTimeout= T.IdeTimeout
    T.acp.IdeConn   = T.KeptIdeConn
    
    T.bcp.Dialer	= new(net.Dialer)
    T.bcp.MaxConn	= T.MaxConn
    T.bcp.IdeTimeout= T.IdeTimeout
    T.bcp.IdeConn  	= T.KeptIdeConn
}

//Transport 建立连接，支持协议类型："tcp", "tcp4","tcp6", "unix", "unixpacket"。其它还没测试支持："udp", "udp4", "udp6", "ip", "ip4", "ip6", "unixgram"
//	a, b *Addr  A，B方地址
//	*D2DSwap    数据交换
//	error       错误
func (T *D2D) Transport(a, b *Addr) (*D2DSwap, error) {
    if T.used.setTrue() {
        return nil, errors.New("vforward: 不能重复调用 D2D.Transport")
    }
    T.init()
    //A连接
    T.aaddr 		= a
    T.aticker 		= time.NewTicker(T.TryConnTime)
   	T.acp.LocalAddr= a.Local
    go T.bufConn(T.aticker, &T.acp, a)//定时处理连接池

    //B连接
    T.baddr 		= b
    T.bticker 		= time.NewTicker(T.TryConnTime)
   	T.bcp.LocalAddr= b.Local
    go T.bufConn(T.bticker, &T.bcp, b)
    
    return &D2DSwap{dd: T}, nil
}

//Close 关闭D2D
//	error   错误
func (T *D2D) Close() error {
    T.closed.setTrue()
    
    T.aticker.Stop()
    T.bticker.Stop()
    
    T.acp.Close()
    T.bcp.Close()
    return nil
}

//当前连接数量
func (T *D2D) currUseConns() int {
    var i int =  int(atomic.LoadInt32(&T.currUseConn))
    if i%2 != 0 {
        return (i/2)+1
    }else{
        return (i/2)
    }
}

//快速取得连接
func (T *D2D) aGetConn() (net.Conn, error) {
    return T.acp.Get(T.aaddr.Remote)
}
func (T *D2D) bGetConn() (net.Conn, error) {
    return T.bcp.Get(T.baddr.Remote)
}

//缓冲连接，保持可用的连接数量
func (T *D2D) bufConn(tick *time.Ticker, cp *vconnpool.ConnPool, addr *Addr){
    for {
        //程序退出
        if T.closed.isTrue() {
            tick.Stop()
            return
        }
        select {
    	case _, ok := <- tick.C:
    		if !ok {
    			//已经被关闭
    			return
    		}
    		
            //实时变更
            if cp.IdeTimeout != T.IdeTimeout {
            	cp.IdeTimeout = T.IdeTimeout
            }
			if cp.IdeConn != T.KeptIdeConn {
				cp.IdeConn = T.KeptIdeConn
			}
			if cp.MaxConn != T.MaxConn {
				cp.MaxConn = T.MaxConn
			}
			
            //1，连接最大限制
            //2，空闲连接限制
            if (T.MaxConn != 0 && T.currUseConns()+cp.ConnNum() >= T.MaxConn) || (T.KeptIdeConn != 0 && cp.ConnNumIde(addr.Remote.Network(), addr.Remote.String()) >= T.KeptIdeConn)  {
                continue
            }
			
			var (
				ctx = T.Context
				cancel context.CancelFunc
			)
            if ctx == nil {
		    	ctx = context.Background()
            }
		    if T.Timeout != 0 {
		        ctx, cancel = context.WithTimeout(ctx, T.Timeout)
		        defer cancel()
		    }
		    T.backPooling.setTrue()
		    ctx = context.WithValue(ctx, "priority", true)
		    conn, err := cp.DialContext(ctx, addr.Network, addr.Remote.String())
		    if err != nil {
		    	T.backPooling.setFalse()
        		T.logf("向远程 %s 发起请求失败: %v", addr.Remote.String(), err)
                continue
		    }
		    conn.Close()//回到池中
		   	T.backPooling.setFalse()
        }
    }
}
func (T *D2D) logf(format string, v ...interface{}){
	if T.ErrorLog != nil {
		T.ErrorLog.Output(2, fmt.Sprintf(format+"\n", v...))
	}
}