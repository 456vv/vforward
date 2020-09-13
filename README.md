# vforward [![Build Status](https://travis-ci.org/456vv/vforward.svg?branch=master)](https://travis-ci.org/456vv/vforward)
golang vforward，TCP/UDP port forwarding，端口转发，主动连接，被动连接，大多用于内网端口反弹。

D2D 命令行：
====================
内网开放端口，外网无法访问的情况下。内网使用D2D主动连接外网端口。以便外网发来数据转发到内网端口中去。<br/>
#### 工作原理：
    |A内网|  ←  |A内网-D2D|  →  |B外网|（1，B收到[A内网-D2D]发来连接）
    |A内网|  ←  |A内网-D2D|  ←  |B外网|（2，B然后向[A内网-D2D]回应数据，数据将转发到A内网。）
    |A内网|  →  |A内网-D2D|  →  |B外网|（3，A内网收到数据再发出数据，由[A内网-D2D]转发到B外网。）

#### 命令行：
    -ARemote string
          A端远程请求连接地址 (format "12.13.14.15:123")
    -ALocal string
          A端本地发起连接地址 (default "0.0.0.0")
    -BRemote string
          B端远程请求连接地址 (format "22.23.24.25:234")
    -BLocal string
          B端本地发起连接地址 (default "0.0.0.0")
    -KeptIdeConn int
          保持一方连接数量，以备快速互相连接。 (default 2)
    -IdeTimeout duration
        空闲连接超时。单位：ns, us, ms, s, m, h
    -MaxConn int
          限制连接最大的数量
    -Network string
          网络地址类型 (default "tcp")
    -ReadBufSize int
          交换数据缓冲大小。单位：字节 (default 4096)
    -Timeout duration
          转发连接时候，请求远程连接超时。单位：ns, us, ms, s, m, h (default 5s)
    -TryConnTime duration
          尝试或发起连接时间，可能一方不在线，会一直尝试连接对方。单位：ns, us, ms, s, m, h (default 500ms)

L2D
====================
是在内网或公网都可以使用，配合D2D或L2L使用功能更自由。L2D功能主要是转发连接（端口转发）。<br/>
#### 工作原理：
    |A端口|  →  |L2D转发|  →  |B端口|（1，B收到A发来数据）
    |A端口|  ←  |L2D转发|  ←  |B端口|（2，然后向A回应数据）
    |A端口|  →  |L2D转发|  →  |B端口|（3，B然后再收到A数据）

#### 命令行：
    -FromLocal string
          转发请求的源地址 (default "0.0.0.0")
    -Listen string
          本地网卡监听地址 (format "0.0.0.0:123")
    -MaxConn int
          限制连接最大的数量
    -Network string
          网络地址类型 (default "tcp")
    -ReadBufSize int
          交换数据缓冲大小。单位：字节 (default 4096)
    -Timeout duration
          转发连接时候，请求远程连接超时。单位：ns, us, ms, s, m, h (default 5s)
    -ToRemote string
          转发请求的目地址 (format "22.23.24.25:234")

L2L 命令行：
====================
是在公网主机上面监听两个TCP端口，由两个内网客户端连接。 L2L使这两个连接进行交换数据，达成内网到内网通道。 注意：1）双方必须主动连接公网L2L。2）不支持UDP协议。<br/>
#### 工作原理：
    |A内网|  →  |公网-D2D|  ←  |B内网|（1，A和B同时连接[公网-D2D]，由[公网-D2D]互相桥接A和B这两个连接）
    |A内网|  ←  |公网-D2D|  ←  |B内网|（2，B 往 A 发送数据）
    |A内网|  →  |公网-D2D|  →  |B内网|（3，A 往 B 发送数据）

#### 命令行：
    -ALocal string
          A本地监听网卡IP地址 (format "12.13.14.15:123")
    -BLocal string
          B本地监听网卡IP地址 (format "22.23.24.25:234")
    -KeptIdeConn int
          保持一方连接数量，以备快速互相连接。 (default 2)
    -IdeTimeout duration
        空闲连接超时。单位：ns, us, ms, s, m, h
    -MaxConn int
          限制连接最大的数量
    -Network string
          网络地址类型 (default "tcp")
    -ReadBufSize int
          交换数据缓冲大小。单位：字节 (default 4096)
    -Timeout duration
          转发连接时候，请求远程连接超时。单位：ns, us, ms, s, m, h (default 5s)

# **列表：**
```go
const DefaultReadBufSize int = 4096                                             // 默认交换数据缓冲大小
type Addr struct {                                                      // 地址
    Network       string                                                        // 网络类型
    Local, Remote net.Addr                                                      // 本地，远程
}
type D2D struct {                                                       // D2D（内网to内网）
    TryConnTime     time.Duration                                               // 尝试或发起连接时间，可能一方不在线，会一直尝试连接对方。
    MaxConn         int                                                         // 限制连接最大的数量
    KeptIdeConn     int                                                         // 保持一方连接数量，以备快速互相连接。
    IdeTimeout      time.Duration                                               // 空闲连接超时
    ReadBufSize     int                                                         // 交换数据缓冲大小
    Timeout         time.Duration                                               // 发起连接超时
    ErrorLog        *log.Logger                                                 // 日志
    Context         context.Context                                             // 上下文
}
    func (dd *D2D) Close() error                                                // 关闭
    func (dd *D2D) Transport(a, b *Addr) (*D2DSwap, error)                      // 建立连接
type D2DSwap struct {}                                                   // D2D交换数据
    func (dds *D2DSwap) Close() error                                           // 关闭
    func (dds *D2DSwap) ConnNum() int                                           // 当前连接数
    func (dds *D2DSwap) Swap() error                                            // 开始交换
type L2D struct {                                                        // L2D（端口转发）
    MaxConn         int                                                         // 限制连接最大的数量
    ReadBufSize     int                                                         // 交换数据缓冲大小
    Timeout         time.Duration                                               // 发起连接超时
    ErrorLog        *log.Logger                                                 // 日志
    Context         context.Context                                             // 上下文
}
    func (ld *L2D) Close() error                                                // 关闭
    func (ld *L2D) Transport(raddr, laddr *Addr) (*L2DSwap, error)              // 建立连接
type L2DSwap struct {}                                                    // L2D交换数据
    func (lds *L2DSwap) Close() error                                           // 关闭
    func (lds *L2DSwap) ConnNum() int                                           // 当前连接数
    func (lds *L2DSwap) Swap() error                                            // 开始交换
type L2L struct {                                                         // L2L（内网to内网）
    MaxConn         int                                                         // 限制连接最大的数量
    KeptIdeConn     int                                                         // 保持一方连接数量，以备快速互相连接。
    IdeTimeout      time.Duration                                               // 空闲连接超时
    ReadBufSize     int                                                         // 交换数据缓冲大小
    ErrorLog        *log.Logger                                                 // 日志
}
    func (ll *L2L) Close() error                                                // 关闭
    func (ll *L2L) Transport(aaddr, baddr *Addr) (*L2LSwap, error)              // 建立连接
type L2LSwap struct {}                                                    // L2L交换数据
    func (lls *L2LSwap) Close() error                                           // 关闭
    func (lls *L2LSwap) ConnNum() int                                           // 当前连接数
    func (lls *L2LSwap) Swap() error                                            // 开始交换
```