package vforward

import (
	"net"
    "io"
    "errors"
    "log"
    "time"
    "github.com/456vv/vconnpool"
    "github.com/456vv/vmap/v2"
    "fmt"
    "sync/atomic"
)

type L2LSwap struct {
    ll              *L2L                    // 引用父结构体 L2L
    conns           *vmap.Map               // 连接存储，方便关闭已经连接的连接
    closed          atomicBool                    // 关闭
    used            atomicBool                    // 正在使用
}
//ConnNum 当前正在转发的连接数量
//  返：
//      int     实时连接数量
func (lls *L2LSwap) ConnNum() int {
    return lls.ll.currUseConns()
}

func (lls *L2LSwap) Swap() error {
    if lls.used.setTrue() {
        return errors.New("vforward: 交换数据已经开启不需重复调用")
    }

    lls.closed.setFalse()
    
    bufSize := lls.ll.ReadBufSize
    if lls.ll.ReadBufSize == 0 {
        bufSize = DefaultReadBufSize
    }

    var wait, maxDelay time.Duration = 0, time.Second

    for  {
        //延时
        wait = delay(wait, maxDelay)

        //程序退出
        if lls.closed.isTrue() {
            return nil
        }
        //如果父级被关闭，则子级也执行关闭
        if lls.ll.closed.isTrue() {
            return lls.Close()
        }
        if lls.ll.acp.ConnNum() > 0 && lls.ll.bcp.ConnNum() > 0 {
            atomic.AddInt32(&lls.ll.currUseConn, 1)
            conna, err := lls.ll.aGetConn()
            if err != nil {
            	atomic.AddInt32(&lls.ll.currUseConn, -1)
                continue
            }
            
            atomic.AddInt32(&lls.ll.currUseConn, 1)
            connb, err := lls.ll.bGetConn()
            if err != nil {
            	atomic.AddInt32(&lls.ll.currUseConn, -2)
                conna.Close()
                continue
            }
            
            wait = 0
            	
            //记录当前连接
            lls.conns.Set(conna, connb)

            go func(conna, connb net.Conn, ll *L2L, lls *L2LSwap, bufSize int){
                copyData(conna, connb, bufSize)
                conna.Close()
            	atomic.AddInt32(&ll.currUseConn, -1)
            }(conna, connb, lls.ll, lls, bufSize)

            go func(conna, connb net.Conn, ll *L2L, lls *L2LSwap, bufSize int){
                copyData(connb, conna, bufSize)
                connb.Close()

                //删除记录的连接，如果是以 .Close() 关闭的。不再重复删除。
                if !lls.closed.isTrue() {
                    lls.conns.Del(conna)
                }
            	atomic.AddInt32(&ll.currUseConn, -1)
            }(conna, connb, lls.ll, lls, bufSize)
        }
    }
    return nil
}
func (lls *L2LSwap) Close() error {
    lls.closed.setTrue()
    lls.conns.Range(func(k, v interface{}) bool {
        if c, ok := k.(io.Closer); ok {
            c.Close()
        }
        if c, ok := v.(io.Closer); ok {
            c.Close()
        }
        return true
    })
    lls.conns.Reset()
    lls.used.setFalse()
    return nil
}

//L2L 是在公网主机上面监听两个TCP端口，由两个内网客户端连接。 L2L使这两个连接进行交换数据，达成内网到内网通道。
//注意：1）双方必须主动连接公网L2L。2）不支持UDP协议。
//	------------------------------------
//  |     |  →  |        |  ←  |     |（1，A和B同时连接[公网-D2D]，由[公网-D2D]互相桥接A和B这两个连接）
//  |A内网|  ←  |公网-D2D|  ←  |B内网|（2，B 往 A 发送数据）
//  |     |  →  |        |  →  |     |（3，A 往 B 发送数据）
//	------------------------------------
type L2L struct {
    MaxConn         int                     // 限制连接最大的数量
    KeptIdeConn     int                     // 保持一方连接数量，以备快速互相连接。
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
func (ll *L2L) init(){
    ll.acp.IdeConn=ll.KeptIdeConn
    ll.bcp.IdeConn=ll.KeptIdeConn
}

//当前连接数量
func (ll *L2L) currUseConns() int {
    var i int = int(atomic.LoadInt32(&ll.currUseConn))
    if i%2 != 0 {
        return (i/2)+1
    }else{
        return (i/2)
    }
}


//快速取得连接
func (ll *L2L) aGetConn() (net.Conn, error) {
    return ll.acp.Get(ll.alisten.Addr())
}
func (ll *L2L) bGetConn() (net.Conn, error) {
    return ll.bcp.Get(ll.blisten.Addr())
}

func (ll *L2L) bufConn(l net.Listener, cp *vconnpool.ConnPool) error {
    var tempDelay time.Duration
    var ok bool
    for  {
    	conn, err := l.Accept()
        if err != nil {
            if tempDelay, ok = temporaryError(err, tempDelay, time.Second); ok {
                continue
            }
            ll.logf("L2L.bufConn", "监听地址 %s， 并等待连接过程中失败: %v", l.Addr(), err)
            return err
        }
        tempDelay = 0

        //1，连接最大限制
        //2，空闲连接限制
        if ll.currUseConns()+cp.ConnNum() >= ll.MaxConn || cp.ConnNum() >= ll.KeptIdeConn {
            conn.Close()
            continue
        }
        cp.Add(l.Addr(), conn)
    }
    return nil
}
//Transport 支持协议类型："tcp", "tcp4","tcp6", "unix" 或 "unixpacket".
//	aaddr, baddr *Addr  A&B监听地址
//	*L2LSwap    交换数据
//	error       错误
func (ll *L2L) Transport(aaddr, baddr *Addr) (*L2LSwap, error) {
    if ll.used.setTrue() {
        return nil, errors.New("vforward: 不能重复调用 L2L.Transport")
    }
    ll.init()
    var err error
    ll.alisten, err = net.Listen(aaddr.Network, aaddr.Local.String())
    if err != nil {
        ll.logf("L2L.Transport监听地址 %s 失败: %v", aaddr.Local.String(), err)
        return nil, err
    }
    ll.blisten, err = net.Listen(baddr.Network, baddr.Local.String())
    if err != nil {
        ll.alisten.Close()
        ll.alisten = nil
        ll.logf("L2L.Transport监听地址 %s 失败: %v", baddr.Local.String(), err)
        return nil, err
    }
    go ll.bufConn(ll.alisten, &ll.acp)
    go ll.bufConn(ll.blisten, &ll.bcp)

    return &L2LSwap{ll:ll, conns:vmap.NewMap()}, nil
}

//Close 关闭L2L
//  返：
//      error   错误
func (ll *L2L) Close() error {
    ll.closed.setTrue()
    if ll.alisten != nil {
        ll.alisten.Close()
    }
    if ll.blisten != nil {
        ll.blisten.Close()
    }
    return nil
}

func (ll *L2L) logf(funcName string, format string, v ...interface{}){
    if ll.ErrorLog != nil{
        ll.ErrorLog.Printf(fmt.Sprint(funcName, "->", format), v...)
    }
}