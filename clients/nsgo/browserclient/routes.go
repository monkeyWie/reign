package browserclient

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ConnectRoutes races independent authenticated routes. No user request is
// retried when a route fails; later requests use the best surviving route.
func ConnectRoutes(ctx context.Context, primary, cloudflare Pairing) (*Client, error) {
	if cloudflare.Relay == "" && primary.DiscoverTunnel == nil {
		return Connect(ctx, primary)
	}
	life, cancel := context.WithCancel(context.Background())
	c := &Client{ctx: life, cancel: cancel, routes: make([]*Client, 2)}
	ready := make(chan struct{}, 2)
	for index, p := range []Pairing{cloudflare, primary} {
		go func(index int, p Pairing) {
			for life.Err() == nil {
				if index == 0 && primary.DiscoverTunnel != nil {
					discover, stop := context.WithTimeout(life, 5*time.Second)
					fresh, err := primary.DiscoverTunnel(discover)
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
				if index == 0 {
					p.RouteName = "Cloudflare Tunnel"
				}
				r, err := Connect(attempt, p)
				stop()
				if err == nil {
					c.mu.Lock()
					if life.Err() != nil {
						c.mu.Unlock()
						r.Close()
						return
					}
					c.routes[index] = r
					c.mu.Unlock()
					select {
					case ready <- struct{}{}:
					default:
					}
					select {
					case <-life.Done():
						r.Close()
						return
					case <-r.ctx.Done():
					}
					c.mu.Lock()
					if c.routes[index] == r {
						c.routes[index] = nil
					}
					c.mu.Unlock()
					r.Close()
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
	case <-ready:
		return c, nil
	case <-ctx.Done():
		c.Close()
		return nil, ctx.Err()
	case <-life.Done():
		return nil, errors.New("连接已关闭")
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
		if r != nil && r.Connected() {
			return r
		}
	}
	return nil
}
