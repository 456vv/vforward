# vforward
实现一个简单的端口工具，端口转发，主动连接，被动连接，大多用于内网端口反弹。支持TCP和UDP协议。


D2D 命令行：
====================
内网开放端口，外网无法访问的情况下。内网使用D2D主动连接外网端口。以便外网发来数据转发到内网端口中去。<br/>
#### 工作原理：
    |A内网|  ←  |D2D|  →  |B外网|（1，A和B收到[D2D]发来连接）
    |A内网|  ←  |D2D|  ←  |B外网|（2，B然后向[D2D]回应数据，数据将转发到A内网。）
    |A内网|  →  |D2D|  →  |B外网|（3，A内网收到数据再发出数据，由[D2D]转发到B外网。）

#### 命令行：
d2d-win-amd64.exe -ARemote 127.0.0.1:4321 -BRemote 127.0.0.1:1234 -MaxConn 2
```
-ALocal string
      A端本地发起连接地址 (default "0.0.0.0")
-ARemote string
      A端远程请求连接地址 (format "12.13.14.15:123")
-AVerify string
      A的验证字符串，桥接后的发出的第一条验证数据头。
-BLocal string
      B端本地发起连接地址 (default "0.0.0.0")
-BRemote string
      B端远程请求连接地址 (format "22.23.24.25:234")
-BVerify string
      B的验证字符串，桥接后的发出的第一条验证数据头。
-BufSize int
      交换数据缓冲大小。单位：字节 (default 4096)
-IdleConn int
      保持一方连接数量，以备快速互相连接。
-IdleTimeout string
      空闲连接超时。单位：ns, us, ms, s, m, h (default "10s")
-MaxConn int
      限制连接最大的数量
-Network string
      网络地址类型 (default "tcp")
-Timeout string
      请求远程连接超时。单位：ns, us, ms, s, m, h (default "5s")
-TryTime string
      尝试或发起连接时间，可能一方不在线，会间隔尝试连接对方。单位：ns, us, ms, s, m, h (default "500ms")
```

L2D
====================
是在内网或公网都可以使用，配合D2D或L2L使用功能更自由。L2D功能主要是转发连接（端口转发）。<br/>
#### 工作原理：
    |A端口|  →  |L2D|  →  |B端口|（1，B收到A发来数据）
    |A端口|  ←  |L2D|  ←  |B端口|（2，然后向A回应数据）
    |A端口|  →  |L2D|  →  |B端口|（3，B然后再收到A数据）

#### 命令行：
l2d-win-amd64.exe -Listen 0.0.0.0:100 -ToRemote 127.0.0.1:4321
```
-AVerify string
      监听端的验证字符串，收到客户端发来的验证数据头。
-BVerify string
      转发端的验证字符串，转发端发去出的验证数据头。
-BufSize int
      交换数据缓冲大小。单位：字节 (default 4096)
-FromLocal string
      转发请求的源地址 (default "0.0.0.0")
-IdleConn int
      保持一方连接数量，以备快速互相连接。
-IdleTimeout string
      空闲连接超时。单位：ns, us, ms, s, m, h (default "10s")
-Listen string
      本地网卡监听地址 (format "0.0.0.0:123")
-MaxConn int
      限制连接最大的数量
-Network string
      网络地址类型 (default "tcp")
-Timeout string
      请求远程连接超时。单位：ns, us, ms, s, m, h (default "5s")
-ToRemote string
      转发请求的目地址 (format "22.23.24.25:234")
-TryTime string
      尝试或发起连接时间，可能一方不在线，会间隔尝试连接对方。单位：ns, us, ms, s, m, h (default "500ms")
```
L2L 命令行：
====================
是在公网主机上面监听两个TCP端口，由两个内网客户端连接。 L2L使这两个连接进行交换数据，达成内网到内网通道。 注意：1）双方必须主动连接公网L2L。2）不支持UDP协议。<br/>
#### 工作原理：
    |A内网|  →  |L2L|  ←  |B内网|（1，A和B同时连接[L2L]，由[L2L]互相桥接A和B这两个连接）
    |A内网|  ←  |L2L|  ←  |B内网|（2，B 往 A 发送数据）
    |A内网|  →  |L2L|  →  |B内网|（3，A 往 B 发送数据）

#### 命令行：
l2l-win-amd64.exe -ALocal 127.0.0.1:100 -BLocal 127.0.0.1:1001
```
-ALocal string
      A本地监听网卡IP地址 (format "12.13.14.15:123")
-AVerify string
      A的验证字符串，桥接后客户端发来的验证数据头。
-BLocal string
      B本地监听网卡IP地址 (format "22.23.24.25:234")
-BVerify string
      B的验证字符串，桥接后客户端发来的验证数据头。
-BufSize int
      交换数据缓冲大小。单位：字节 (default 4096)
-IdleConn int
      保持一方连接数量，以备快速互相连接。
-IdleTimeout string
      空闲连接超时。单位：ns, us, ms, s, m, h (default "10s")
-MaxConn int
      限制连接最大的数量
-Network string
      网络地址类型 (default "tcp")
```