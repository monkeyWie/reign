// Package browserclient implements the existing encrypted wire protocol for the browser UI.
// Cryptographic framing mirrors internal/remote; this package has no UDP/Pion dependency.
package browserclient

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"sync"
	"sync/atomic"
)

type cryptor struct {
	aead       cipher.AEAD
	prefix     [4]byte
	counter    atomic.Uint64
	mu         sync.Mutex
	highest    uint64
	seen       map[uint64]struct{}
	random     bool
	randomSeen map[string]struct{}
}

func newCryptor(secret string) (*cryptor, error) {
	raw, err := hex.DecodeString(secret)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("invalid pairing key")
	}
	key := sha256.Sum256(append([]byte("reign/p2p/aead/"), raw...))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	c := &cryptor{aead: aead, seen: make(map[uint64]struct{})}
	if _, err = rand.Read(c.prefix[:]); err != nil {
		return nil, err
	}
	return c, nil
}
func (c *cryptor) seal(aad string, plain []byte) ([]byte, error) {
	nonce := make([]byte, 12)
	if c.random {
		if _, err := rand.Read(nonce); err != nil {
			return nil, err
		}
		return c.aead.Seal(nonce, nonce, plain, []byte(aad)), nil
	}
	copy(nonce, c.prefix[:])
	sequence := c.counter.Add(1)
	if sequence == 0 {
		return nil, errors.New("nonce exhausted")
	}
	binary.BigEndian.PutUint64(nonce[4:], sequence)
	return c.aead.Seal(nonce, nonce, plain, []byte(aad)), nil
}
func (c *cryptor) open(aad string, packet []byte) ([]byte, error) {
	n := c.aead.NonceSize()
	if len(packet) < n {
		return nil, errors.New("short packet")
	}
	plain, err := c.aead.Open(nil, packet[:n], packet[n:], []byte(aad))
	if err != nil {
		return nil, err
	}
	if c.random {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.randomSeen == nil {
			c.randomSeen = map[string]struct{}{}
		}
		nonce := string(packet[:n])
		if _, seen := c.randomSeen[nonce]; seen {
			return nil, errors.New("replayed handshake")
		}
		if len(c.randomSeen) >= 32 {
			return nil, errors.New("too many handshake packets")
		}
		c.randomSeen[nonce] = struct{}{}
		return plain, nil
	}
	sequence := binary.BigEndian.Uint64(packet[4:12])
	c.mu.Lock()
	defer c.mu.Unlock()
	if sequence == 0 || (c.highest >= 4096 && sequence <= c.highest-4096) {
		return nil, errors.New("stale packet")
	}
	if _, ok := c.seen[sequence]; ok {
		return nil, errors.New("replayed packet")
	}
	c.seen[sequence] = struct{}{}
	if sequence > c.highest {
		c.highest = sequence
	}
	if len(c.seen) > 4096 {
		for seq := range c.seen {
			if seq <= c.highest-4096 {
				delete(c.seen, seq)
			}
		}
	}
	return plain, nil
}
