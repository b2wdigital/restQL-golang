package middleware

import (
	"context"
	"io"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/b2wdigital/restQL-golang/v6/pkg/restql"
)

// ConnManager keeps track of active TCP connections
// and their corresponding contexts.
//
// When a connection is closed by the client it cancel
// its context.
type ConnManager struct {
	enabled       bool
	watchInterval time.Duration
	log           restql.Logger
	contextIndex  sync.Map
}

// NewConnManager creates a connection manager
func NewConnManager(log restql.Logger, enabled bool, watchInterval time.Duration) *ConnManager {
	return &ConnManager{log: log, enabled: enabled, watchInterval: watchInterval}
}

// ContextForConnection get the connections context, if it exists.
// Otherwise it create a context and start the connection watcher.
func (cm *ConnManager) ContextForConnection(conn net.Conn) context.Context {
	if !cm.enabled {
		return context.Background()
	}

	connCtx, found := cm.contextIndex.Load(conn)
	if !found {
		return cm.initializeConnContext(conn)
	}

	ctx, ok := connCtx.(context.Context)
	if !ok {
		return cm.initializeConnContext(conn)
	}

	return ctx
}

func (cm *ConnManager) initializeConnContext(conn net.Conn) context.Context {
	ctx, cancel := context.WithCancel(context.Background())

	go cm.watchConn(conn, func() {
		cm.contextIndex.Delete(conn)
		cancel()
	})

	cm.contextIndex.Store(conn, ctx)

	return ctx
}

func (cm *ConnManager) watchConn(conn net.Conn, callback func()) {
	rc, err := conn.(syscall.Conn).SyscallConn()
	if err != nil {
		cm.log.Error("failed to cast connection to syscall interface", err)
		callback()
		return
	}

	var sysErr error = nil
	var buf = []byte{0}
	fdReader := func(fd uintptr) bool {
		n, _, err := syscall.Recvfrom(int(fd), buf, syscall.MSG_PEEK|syscall.MSG_DONTWAIT)
		switch {
		case n == 0 && err == nil:
			sysErr = io.EOF
		case err == syscall.EAGAIN || err == syscall.EWOULDBLOCK:
			sysErr = nil
		default:
			sysErr = err
		}
		return true
	}

	ticker := time.NewTicker(cm.watchInterval)
	defer ticker.Stop()

	for range ticker.C {
		err = rc.Read(fdReader)
		if err != nil {
			callback()
			return
		}

		if sysErr != nil {
			callback()
			return
		}
	}
}
