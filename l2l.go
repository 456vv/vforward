package vforward

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/456vv/vconn"
	"github.com/456vv/vconnpool/v3"
)

type L2LSwap struct {
	ll        L2L
	ctx       context.Context
	cancel    func()
	aOpt      *Option
	bOpt      *Option
	al        net.Listener
	bl        net.Listener
	cp        *vconnpool.Pool
	running   atomic.Bool
	conns     sync.Map
	connCount atomic.Int64
	closed    atomic.Bool
}

func (T *L2LSwap) ConnNum() int32 {
	return int32(T.connCount.Load())
}

func (T *L2LSwap) acceptLoop(l net.Listener, opt *Option) {
	for {
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || T.ctx.Err() != nil {
				return
			}
			// 处理临时错误
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			return
		}
		if T.ctx.Err() != nil {
			conn.Close()
			return
		}
		// 优化：为TCP连接设置KeepAlive，提高长连接稳定性
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(30 * time.Second)
			tcpConn.SetNoDelay(true)
		}
		go T.examineConn(conn, opt, false)
	}
}

func (T *L2LSwap) LimitMaxConn(addr net.Addr) bool {
	return T.ll.MaxConn > 0 && (T.connCount.Load()+int64(T.cp.NumIdle(addr))) >= int64(T.ll.MaxConn)
}

func (T *L2LSwap) examineConn(conn net.Conn, opt *Option, skipVerify bool) {
	if T.LimitMaxConn(opt.Local) {
		conn.Close()
		return
	}

	if !skipVerify && opt.Verify != nil && !opt.Verify(conn) {
		conn.Close()
		return
	}

	if err := T.cp.Put(conn, opt.Local); err != nil {
		conn.Close()
	}
}

func (T *L2LSwap) pairConnections() error {
	for {
		var wg sync.WaitGroup
		wg.Go(func() {
			select {
			case <-T.ctx.Done():
			case <-T.cp.WaitConnIdleGeq(T.aOpt.Local, 1):

			}
		})
		wg.Go(func() {
			select {
			case <-T.ctx.Done():
			case <-T.cp.WaitConnIdleGeq(T.bOpt.Local, 1):
			}
		})
		wg.Wait()

		if T.ctx.Err() != nil {
			T.Close()
			return nil
		}

		T.connCount.Add(1)
		conna, bufa, err := T.cp.GetFull(T.aOpt.Local)
		if err != nil {
			T.connCount.Add(-1)
			continue
		}
		if bufa != nil {
			conna = vconn.NewData(conna, bufa)
		}

		connb, bufb, err := T.cp.GetFull(T.bOpt.Local)
		if err != nil {
			T.connCount.Add(-1)
			// 如果A连接存在数据，关闭连接
			if bufa != nil {
				conna.Close()
				continue
			}

			// 检查并回收conna连接
			T.examineConn(conna, T.aOpt, true)
			continue
		}
		if bufb != nil {
			connb = vconn.NewData(connb, bufb)
		}

		T.conns.Store(conna, connb)
		go T.dataCopy(conna, connb)
	}
}

func (T *L2LSwap) Swap() error {
	if T.running.Swap(true) {
		return errors.New("vforward: 交换数据已经开启不需重复调用")
	}
	if err := T.ctx.Err(); err != nil {
		return err
	}
	go T.acceptLoop(T.al, T.aOpt)
	go T.acceptLoop(T.bl, T.bOpt)
	return T.pairConnections()
}

func (T *L2LSwap) dataCopy(conna, connb net.Conn) {
	defer T.conns.Delete(conna)
	defer T.connCount.Add(-1)
	defer conna.Close()
	defer connb.Close()

	if T.ctx.Err() != nil {
		return
	}

	// 双向复制
	var cpWg sync.WaitGroup
	cpWg.Go(func() {
		// 方向 A -> B
		copyData(connb, conna, T.ll.BufSize)
	})
	cpWg.Go(func() {
		// 方向 B -> A
		copyData(conna, connb, T.ll.BufSize)
	})

	// 等待任意一端关闭
	cpWg.Wait()
}

func (T *L2LSwap) Close() error {
	if T.closed.Swap(true) {
		return nil
	}

	T.cancel()

	if T.al != nil {
		T.al.Close()
	}
	if T.bl != nil {
		T.bl.Close()
	}

	if T.cp != nil {
		T.cp.Close()
	}

	T.conns.Range(func(key any, value any) bool {
		if c, ok := key.(net.Conn); ok {
			c.Close()
		}
		if c, ok := value.(net.Conn); ok {
			c.Close()
		}
		return true
	})

	return nil
}

type L2L struct {
	BufSize     int
	MaxConn     int
	IdleConn    int
	IdleTimeout time.Duration
}

func (T L2L) New(ctx context.Context, a, b *Option) (*L2LSwap, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a == nil || b == nil || a.Local == nil || b.Local == nil {
		return nil, errors.New("vforward: 选项或本地地址不能为零")
	}

	alistenAny, err := connectListen(a.Local)
	if err != nil {
		return nil, err
	}
	alisten, ok := alistenAny.(net.Listener)
	if !ok {
		// 关闭可能的PacketConn
		if c, ok := alistenAny.(io.Closer); ok {
			c.Close()
		}
		return nil, errors.New("vforward: a.Local is not a Listener type")
	}

	blistenAny, err := connectListen(b.Local)
	if err != nil {
		alisten.Close()
		return nil, err
	}
	blisten, ok := blistenAny.(net.Listener)
	if !ok {
		alisten.Close()
		if c, ok := blistenAny.(io.Closer); ok {
			c.Close()
		}
		return nil, errors.New("vforward: b.Local is not a Listener type")
	}

	lls := &L2LSwap{
		ll:   T,
		aOpt: a,
		bOpt: b,
		al:   alisten,
		bl:   blisten,
		cp:   &vconnpool.Pool{},
	}

	lls.ctx, lls.cancel = context.WithCancel(ctx)
	return lls, nil
}
