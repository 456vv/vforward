package vforward

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/456vv/vconnpool/v3"
)

// L2DSwap 数据交换
type L2DSwap struct {
	ld     L2D
	aOpt   *Option
	bOpt   *Option
	ctx    context.Context
	cancel context.CancelFunc

	cp *vconnpool.Pool

	connsTCP sync.Map // lconn -> rconn
	connsUDP sync.Map // laddrStr -> net.Conn 或 wrapper

	running atomic.Bool

	// 性能优化：缓冲池
	bufPool sync.Pool

	// 连接计数和保护
	connCount atomic.Int32

	lc io.Closer
}

func (T *L2DSwap) ConnNum() int32 {
	return int32(T.connCount.Load())
}

func (T *L2DSwap) keepAvailable() {
	var tickerC <-chan time.Time
	if T.ld.TryTime > 0 {
		ticker := time.NewTicker(T.ld.TryTime)
		defer ticker.Stop()
		tickerC = ticker.C
	}

	targetIdle := T.ld.IdleConn
	if targetIdle == 0 {
		targetIdle = 1 // 至少保持1个
	}

	ctx := context.WithValue(T.ctx, vconnpool.PriorityContextKey, true)
	for {
		select {
		case <-T.ctx.Done():
			return
		case <-tickerC:
		default:
		}

		if ok := <-T.cp.WaitConnIdleLeq(T.bOpt.Remote, targetIdle-1); !ok {
			return
		}

		T.dialConn(ctx)
	}
}

func (T *L2DSwap) dialConn(ctx context.Context) {
	rconn, err := T.cp.DialContext(ctx, T.bOpt.Remote.Network(), T.bOpt.Remote.String())
	if err != nil {
		return
	}
	defer rconn.Close()
	if T.bOpt.Verify != nil && !T.bOpt.Verify(rconn) {
		rconn.Discard()
		return
	}
}

func (T *L2DSwap) getConn(ctx context.Context) (net.Conn, error) {
	if T.ld.DialTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, T.ld.DialTimeout)
		defer cancel()
	}

	rconn, err := T.cp.DialContext(ctx, T.bOpt.Remote.Network(), T.bOpt.Remote.String())
	if err != nil {
		return nil, err
	}
	rconn.Discard()
	if !rconn.IsReuseConn() {
		if T.bOpt.Verify != nil && !T.bOpt.Verify(rconn) {
			rconn.Close()
			return nil, errors.New("connection verification failed")
		}
	}
	return rconn, nil
}

// tcp
func (T *L2DSwap) connRemoteTCP(lconn net.Conn) {
	defer lconn.Close()

	if T.ld.MaxConn > 0 && T.connCount.Load() >= int32(T.ld.MaxConn) {
		return
	}

	T.connCount.Add(1)
	defer T.connCount.Add(-1)

	rconn, err := T.getConn(T.ctx)
	if err != nil {
		return
	}

	T.connsTCP.Store(lconn, rconn)
	defer T.connsTCP.Delete(lconn)
	defer rconn.Close()

	if T.ctx.Err() != nil {
		return
	}

	// 双向复制
	var cpWg sync.WaitGroup
	cpWg.Go(func() {
		// 方向 A -> B
		copyData(rconn, lconn, T.ld.BufSize)
	})
	cpWg.Go(func() {
		// 方向 B -> A
		copyData(lconn, rconn, T.ld.BufSize)
	})

	// 等待任意一端关闭
	cpWg.Wait()
}

// udp
func (T *L2DSwap) connReadReply(laddr net.Addr, lconn net.PacketConn, rconn net.Conn) {
	// 没有超时是危险的，连接不会断开，默认一分钟超时
	timeout := T.ld.ReadTimeout
	if timeout <= 0 {
		timeout = time.Minute
	}

	buf := T.getBuf()
	defer T.putBuf(buf)
	for {
		// 设置UDP读取超时，由于UDP是无连接，无法知道对方状态
		if err := rconn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			return
		}
		// 读取数据
		n, err := rconn.Read(buf)
		if n > 0 {
			if _, werr := lconn.WriteTo(buf[:n], laddr); werr != nil {
				return
			}
		}

		if err != nil {
			return
		}
	}
}

func (T *L2DSwap) connRemoteUDP(buf []byte, laddr net.Addr, lconn net.PacketConn) {
	if T.ctx.Err() != nil {
		return
	}
	// 如果已经建立连接
	if v, ok := T.connsUDP.Load(laddr.String()); ok {
		v.(net.Conn).Write(buf)
		return
	}

	if T.ld.MaxConn > 0 && T.connCount.Load() >= int32(T.ld.MaxConn) {
		return
	}

	T.connCount.Add(1)
	defer T.connCount.Add(-1)

	rconn, err := T.getConn(T.ctx)
	if err != nil {
		return
	}

	T.connsUDP.Store(laddr.String(), rconn)
	defer T.connsUDP.Delete(laddr.String())
	defer rconn.Close()

	if T.ctx.Err() != nil {
		return
	}

	rconn.Write(buf)
	T.connReadReply(laddr, lconn, rconn)
}

// getBuf 从池获取缓冲
func (T *L2DSwap) getBuf() []byte {
	return T.bufPool.Get().([]byte)
}

// putBuf 归还缓冲
func (T *L2DSwap) putBuf(b []byte) {
	if cap(b) > 0 { // 安全检查
		T.bufPool.Put(b)
	}
}

func (T *L2DSwap) acceptConn(listen any) error {
	switch l := listen.(type) {
	case net.Listener:
		for {
			conn, err := l.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) || T.ctx.Err() != nil {
					return nil // 正常关闭
				}
				return err
			}
			go T.examineConn(conn)
		}
	case net.PacketConn:
		for {
			buf := T.getBuf()
			n, laddr, err := l.ReadFrom(buf)
			if err != nil {
				if n > 0 {
					T.connRemoteUDP(buf[:n], laddr, l)
				}
				if errors.Is(err, net.ErrClosed) || T.ctx.Err() != nil {
					return nil
				}
				return err
			}
			if n > 0 {
				go T.connRemoteUDP(buf[:n], laddr, l)
			}
		}
	}
	return nil
}

func (T *L2DSwap) examineConn(lconn net.Conn) {
	if T.aOpt.Verify != nil && !T.aOpt.Verify(lconn) {
		lconn.Close()
		return
	}
	T.connRemoteTCP(lconn)
}

func (T *L2DSwap) Swap() error {
	if T.running.Swap(true) {
		return errors.New("vforward: 交换数据已经开启不需重复调用")
	}

	if err := T.ctx.Err(); err != nil {
		return err
	}

	listen, err := connectListen(T.aOpt.Local)
	if err != nil {
		return err
	}

	T.lc = listen.(io.Closer)
	go T.keepAvailable()
	return T.acceptConn(listen)
}

func (T *L2DSwap) Close() error {
	if !T.running.Load() {
		return errors.New("vforward: 交换数据未开启无法关闭")
	}

	T.cancel()

	if T.lc != nil {
		T.lc.Close()
	}
	if T.cp != nil {
		T.cp.Close()
	}

	T.connsTCP.Range(func(k, v any) bool {
		if c, ok := k.(io.Closer); ok {
			c.Close()
		}
		if c, ok := v.(io.Closer); ok {
			c.Close()
		}
		return true
	})
	T.connsUDP.Range(func(k, v any) bool {
		if c, ok := v.(io.Closer); ok {
			c.Close()
		}
		return true
	})

	return nil
}

type L2D struct {
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	DialTimeout  time.Duration
	MaxConn      int
	IdleConn     int
	IdleTimeout  time.Duration
	TryTime      time.Duration
	BufSize      int
}

func (T L2D) New(ctx context.Context, a, b *Option) (*L2DSwap, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cp := &vconnpool.Pool{
		IdleConn:    T.IdleConn,
		IdleTimeout: T.IdleTimeout,
		MaxConn:     T.MaxConn,
	}
	if T.DialTimeout == 0 {
		T.DialTimeout = 30 * time.Second
	}

	lds := &L2DSwap{
		ld:   T,
		cp:   cp,
		aOpt: a,
		bOpt: b,
		bufPool: sync.Pool{
			New: func() any {
				size := T.BufSize
				if size <= 0 {
					size = DefaultReadBufSize
				}
				return make([]byte, size)
			},
		},
	}
	// 设置 dialer
	lds.cp.Dialer = &net.Dialer{
		Timeout:   T.DialTimeout,
		LocalAddr: b.Local,
		KeepAlive: 30 * time.Second,
	}

	lds.ctx, lds.cancel = context.WithCancel(ctx)

	return lds, nil
}
