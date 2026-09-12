package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// sessionKeyFileName is the name of the file, relative to a module's data
// directory, that holds the HMAC signing key used for session JWTs.
const sessionKeyFileName = "session.key"

// sessionKeySize is the length in bytes of the generated signing key.
const sessionKeySize = 32

// loadOrCreateKey returns the HMAC-SHA256 signing key stored at
// <dataDir>/session.key. If the file does not exist, dataDir is created
// (0750) and a fresh 32-byte key is generated with crypto/rand and written
// with mode 0600. If another process wins the race to create the file
// first, the key it wrote is read back instead. The key itself is never
// logged.
func loadOrCreateKey(dataDir string) ([]byte, error) {
	path := filepath.Join(dataDir, sessionKeyFileName)

	if key, err := readSessionKey(path); err == nil {
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, fmt.Errorf("auth: creating session key directory: %w", err)
	}

	key := make([]byte, sessionKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("auth: generating session key: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			// Lost the create race to another process; use the key it wrote.
			return readSessionKey(path)
		}
		return nil, fmt.Errorf("auth: creating session key file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(key); err != nil {
		return nil, fmt.Errorf("auth: writing session key: %w", err)
	}

	return key, nil
}

// readSessionKey reads and validates an existing key file. It returns an
// error satisfying errors.Is(err, os.ErrNotExist) when the file is absent,
// and a plain error for any other failure, including a length mismatch.
func readSessionKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("auth: reading session key: %w", err)
	}
	if len(data) != sessionKeySize {
		return nil, fmt.Errorf("auth: session key file %s has length %d, expected %d", path, len(data), sessionKeySize)
	}
	return data, nil
}
