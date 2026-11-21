package deviceservice

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/monkeyWie/reignhub/internal/nativepath"
	"github.com/monkeyWie/reignhub/pkg/database"
	"gorm.io/gorm"
)

const signingSeedKey = "signing_seed"

func readSigningSeed(db *gorm.DB) ([]byte, error) {
	var key Metadata
	if err := db.First(&key, "key = ?", signingSeedKey).Error; err != nil {
		return nil, err
	}
	seed, err := base64.RawURLEncoding.DecodeString(key.Value)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("invalid stored service signing identity")
	}
	return seed, nil
}

func relayTLSKey(seed []byte) ed25519.PrivateKey { return nativepath.Key(seed, "service-relay") }

// ReadRelayPin returns the public pin used by both native relay transports.
func ReadRelayPin(state string) (string, error) {
	db, err := database.OpenSQLiteReadOnly(state)
	if err != nil {
		return "", fmt.Errorf("open existing service state %q: %w", state, err)
	}
	raw, err := db.DB()
	if err != nil {
		return "", err
	}
	defer raw.Close()
	seed, err := readSigningSeed(db)
	if err != nil {
		return "", fmt.Errorf("read relay identity from %q: %w", state, err)
	}
	return nativepath.Pin(relayTLSKey(seed)), nil
}
