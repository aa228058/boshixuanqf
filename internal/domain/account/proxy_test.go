package account

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

type contextTestDialer struct {
	called bool
}

func (d *contextTestDialer) Dial(network, address string) (net.Conn, error) {
	return nil, errors.New("unexpected non-context dial")
}

func (d *contextTestDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.called = true
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestDialProxyContextHonorsCancellation(t *testing.T) {
	d := &contextTestDialer{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := dialProxyContext(ctx, d, "tcp", "example.com:443")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if !d.called {
		t.Fatal("expected context-aware dialer to be called")
	}
}

func TestDialProxyContextRejectsNonContextDialer(t *testing.T) {
	var d proxy.Dialer = proxy.Direct
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := dialProxyContext(ctx, d, "tcp", "example.com:443")
	if err == nil {
		t.Fatal("expected unsupported context dialer error")
	}
}
