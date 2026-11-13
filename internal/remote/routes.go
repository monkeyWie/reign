package remote

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ConnectRoutes keeps independent authenticated connections. P2P always wins;
// otherwise Cloudflare wins over the private relay. A broken route reconnects
// independently. Each RPC is sent exactly once on its selected connection.
func ConnectRoutes(ctx context.Context, primary, cloudflare Pairing, opts Options) (*Client, error) {
	if cloudflare.Relay == "" && opts.DiscoverTunnel == nil {
		return Connect(ctx, primary, opts)
	}
	life, cancel := context.WithCancel(context.Background())
	c := &Client{routes: make([]*Client, 2), routeCtx: life, routeCancel: cancel}
	available := make(chan struct{}, 2)
	for index, p := range []Pairing{cloudflare, primary} {
		go func(index int, p Pairing) {
			for life.Err() == nil {
				if index == 0 && opts.DiscoverTunnel != nil {
					discover, stop := context.WithTimeout(life, 5*time.Second)
					fresh, err := opts.DiscoverTunnel(discover)
					stop()
					if err == nil {
						p = fresh
					} else {
						p = Pairing{}
					}
				}
				if p.Relay == "" {
					select {
					case <-life.Done():
						return
					case <-time.After(5 * time.Second):
					}
					continue
				}
				attempt, stop := context.WithTimeout(life, 12*time.Second)
				o := opts
				if index == 0 {
					o.RelayName = "Cloudflare Tunnel"
				}
				child, err := Connect(attempt, p, o)
				stop()
				if err == nil {
					c.mu.Lock()
					if life.Err() != nil {
						c.mu.Unlock()
						child.Close()
						return
					}
					c.routes[index] = child
					c.mu.Unlock()
					select {
					case available <- struct{}{}:
					default:
					}
					select {
					case <-life.Done():
						child.Close()
						return
					case <-child.done:
					}
					c.mu.Lock()
					if c.routes[index] == child {
						c.routes[index] = nil
					}
					c.mu.Unlock()
					child.Close()
				}
				select {
				case <-life.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
		}(index, p)
	}
	select {
	case <-available:
		return c, nil
	case <-ctx.Done():
		c.Close()
		return nil, ctx.Err()
	case <-life.Done():
		return nil, errors.New("connections closed")
	}
}
func (c *Client) route() *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.routes {
		if r != nil && strings.HasPrefix(r.Path(), "P2P") {
			return r
		}
	}
	for _, r := range c.routes {
		if r != nil && r.Path() != "Disconnected" {
			return r
		}
	}
	return nil
}
