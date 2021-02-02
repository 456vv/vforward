package vforward

import (
	"net"
    "io"
    "errors"
    "log"
    "time"
    "github.com/456vv/vconnpool/v2"
    "github.com/456vv/vmap/v2"
    "fmt"
    "sync/atomic"
)

type L2LSwap struct {
	Verify	func(a, b net.Conn) (net.Conn, net.Conn, error)	// 数据交换前对双方连接操作，可以现实验证之类
    ll     	*L2L                    		// 引用父结构体 L2L
    conns  	vmap.Map               			// 连接存储，方便关闭已经连接的连接
    closed 	atomicBool                    	// 关闭
    used   	atomicBool                    	// 正在使用
}
//ConnNum 当前正在转发的连接数量
//	int     实时连接数量
func (T *L2LSwap) ConnNum() int {
    return T.ll.currUseConns()
}

func (T *L2LSwap) Swap() error {
    if T.used.setTrue() {
        return errors.New("vforward: 交换数据已经开启不需重复调用")
    }

    T.closed.setFalse()
    
    bufSize := T.ll.ReadBufSize
    if bufSize == 0 {
        bufSize = DefaultReadBufSize
    }

    var wait, maxDelay time.Duration = 0, time.Second

    for {
        //延时
        wait = delay(wait, maxDelay)

        //程序退出
        if T.closed.isTrue() {
            return nil
        }
        
        //如果父级被关闭，则子级也执行关闭
        if T.ll.closed.isTrue() {
            return T.Close()
        }
        
        if T.ll.acp.ConnNum() > 0 && T.ll.bcp.ConnNum() > 0 {
            atomic.AddInt32(&T.ll.currUseConn, 1)
            conna, err := T.ll.aGetConn()
            if err != nil {
            	atomic.AddInt32(&T.ll.currUseConn, -1)
                continue
            }
            
            atomic.AddInt32(&T.ll.currUseConn, 1)
            connb, err := T.ll.bGetConn()
            if err != nil {
            	atomic.AddInt32(&T.ll.currUseConn, -2)
                conna.Close()
                continue
            }
            
            wait = 0
            
            go func(conna, connb net.Conn, ll *L2L, T *L2LSwap, bufSize int){
        		
	            defer atomic.AddInt32(&ll.currUseConn, -2)
	            
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
	        			return
	        		}
	        	}
	        	
		        //----------------------------
           		go func(conna, connb net.Conn, ll *L2L, T *L2LSwap, bufSize int){
                	copyData(conna, connb, bufSize)
                	conna.Close()
                }(conna, connb, ll, T, bufSize)
                
		        //----------------------------
                copyData(connb, conna, bufSize)
                connb.Close()
                
            }(conna, connb, T.ll, T, bufSize)
        }
    }
}
func (T *L2LSwap) Close() error {
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

//L2L 是在公网主机上面监听两个TCP端口，由两个内网客户端连接。 L2L使这两个连接进行交换数据，达成内网到内网通道。
//注意：1）双方必须主动连接公网L2L。2）不支持UDP协议。
//	------------------------------------
//  |     |  →  |   |  ←  |     |（1，A和B同时连接[D2D]，由[D2D]互相桥接A和B这两个连接）
//  |A内网|  ←  |D2D|  ←  |B内网|（2，B 往 A 发送数据）
//  |     |  →  |   |  →  |     |（3，A 往 B 发送数据）
//	------------------------------------
type L2L struct {
    MaxConn         int                     // 限制连接最大的数量
    KeptIdeConn     int                     // 保持一方连接数量，以备快速互相连接。
    IdeTimeout      time.Duration           // 空闲连接超时
    ReadBufSize     int                     // 交换数据缓冲大小
    ErrorLog        *log.Logger             // 日志

    alisten         net.Listener            // A监听
    acp             vconnpool.ConnPool      // A方连接池

    blisten         net.Listener            // B监听
    bcp             vconnpool.ConnPool      // B方连接池

    currUseConn     int32                   // 当前使用连接数量

    closed          atomicBool              // 关闭
    used            atomicBool              // 正在使用
}
func (T *L2L) init(){
	
    T.acp.IdeConn=T.KeptIdeConn
    T.bcp.IdeConn=T.KeptIdeConn

    T.acp.IdeTimeout=T.IdeTimeout
    T.bcp.IdeTimeout=T.IdeTimeout
}

//当前连接数量
func (T *L2L) currUseConns() int {
    var i int = int(atomic.LoadInt32(&T.currUseConn))
    if i%2 != 0 {
        return (i/2)+1
    }else{
        return (i/2)
    }
}

//快速取得连接
func (T *L2L) aGetConn() (net.Conn, error) {
    return T.acp.Get(T.alisten.Addr())
}
func (T *L2L) bGetConn() (net.Conn, error) {
    return T.bcp.Get(T.blisten.Addr())
}

func (T *L2L) bufConn(l net.Listener, cp *vconnpool.ConnPool) error {
    var tempDelay time.Duration
    var ok bool
    for  {
    	conn, err := l.Accept()
        if err != nil {
            if tempDelay, ok = temporaryError(err, tempDelay, time.Second); ok {
                continue
            }
            T.logf("监听地址 %s， 并等待连接过程中失败: %v", l.Addr(), err)
            return err
        }
        tempDelay = 0

        //实时变更
        if cp.IdeTimeout != T.IdeTimeout {
        	cp.IdeTimeout = T.IdeTimeout
        }
        if cp.IdeConn != T.KeptIdeConn {
        	cp.IdeConn = T.KeptIdeConn
        }
        
        //1，连接最大限制
        //2，空闲连接限制
        if (T.MaxConn != 0 && T.currUseConns()+cp.ConnNum() >= T.MaxConn) ||  (T.KeptIdeConn != 0 && cp.ConnNum() >= T.KeptIdeConn) {
            conn.Close()
            continue
        }
        cp.Put(conn, l.Addr())
    }
}

//Transport 支持协议类型："tcp", "tcp4","tcp6", "unix" 或 "unixpacket".
//	aaddr, baddr *Addr  A&B监听地址
//	*L2LSwap    交换数据
//	error       错误
func (T *L2L) Transport(aaddr, baddr *Addr) (*L2LSwap, error) {
    if T.used.setTrue() {
        return nil, errors.New("vforward: 不能重复调用 L2L.Transport")
    }
    T.init()
    var err error
    T.alisten, err = net.Listen(aaddr.Network, aaddr.Local.String())
    if err != nil {
        T.logf("监听地址 %s 失败: %v", aaddr.Local.String(), err)
        return nil, err
    }
    T.blisten, err = net.Listen(baddr.Network, baddr.Local.String())
    if err != nil {
        T.alisten.Close()
        T.alisten = nil
        T.logf("监听地址 %s 失败: %v", baddr.Local.String(), err)
        return nil, err
    }
    go T.bufConn(T.alisten, &T.acp)
    go T.bufConn(T.blisten, &T.bcp)

    return &L2LSwap{ll:T}, nil
}

//Close 关闭L2L
//	error   错误
func (T *L2L) Close() error {
    T.closed.setTrue()
    if T.alisten != nil {
        T.alisten.Close()
    }
    if T.blisten != nil {
        T.blisten.Close()
    }
    return nil
}

func (T *L2L) logf(format string, v ...interface{}){
	if T.ErrorLog != nil {
		T.ErrorLog.Output(2, fmt.Sprintf(format+"\n", v...))
	}
}
