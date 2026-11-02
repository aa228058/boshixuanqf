package account

import (
	"context"
	"errors"
	"net"
	"testing"

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

func TestProxyDialerSupportsContext(t *testing.T) {
	d, err := ProxyDialer("socks5://127.0.0.1:1080")
	if err != nil {
		t.Fatalf("create SOCKS5 dialer: %v", err)
	}
	if _, ok := d.(proxy.ContextDialer); !ok {
		t.Fatal("SOCKS5 dialer does not implement proxy.ContextDialer")
	}
}
