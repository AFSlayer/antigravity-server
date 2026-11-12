package auth

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	algorithm        = "pbkdf2-sha256"
	iterations       = 200000
	saltLen          = 16
	keyLen           = 32
	MinPasswordLen   = 8
	maxVerifyWorkers = 4
)

var verifySem = make(chan struct{}, maxVerifyWorkers)

const passwordAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// Credentials is the stored password verifier. The plaintext is never written.
type Credentials struct {
	Algorithm  string    `json:"algorithm"`
	Iterations int       `json:"iterations"`
	Salt       string    `json:"salt"`
	Hash       string    `json:"hash"`
	UpdatedAt  time.Time `json:"updated_at"`

	path     string
	lastStat os.FileInfo
	mu       sync.Mutex
}

// GeneratePassword returns a random password in xxxx-xxxx-xxxx form, drawn from
// an alphabet without visually ambiguous characters so it can be read off a
// screen and typed on a phone.
func GeneratePassword() string {
	groups := make([]byte, 0, 14)
	for i := 0; i < 12; i++ {
		if i > 0 && i%4 == 0 {
			groups = append(groups, '-')
		}
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(passwordAlphabet))))
		if err != nil {
			return ""
		}
		groups = append(groups, passwordAlphabet[n.Int64()])
	}
	return string(groups)
}

func derive(password string, salt []byte, iters int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, salt, iters, keyLen)
}

func newCredentials(path, password string) (*Credentials, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}

	hash, err := derive(password, salt, iterations)
	if err != nil {
		return nil, err
	}

	return &Credentials{
		Algorithm:  algorithm,
		Iterations: iterations,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Hash:       base64.StdEncoding.EncodeToString(hash),
		UpdatedAt:  time.Now().UTC(),
		path:       path,
	}, nil
}

// LoadCredentials reads an existing verifier from disk.
func LoadCredentials(path string) (*Credentials, error) {
	info, statErr := os.Stat(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.Algorithm != algorithm {
		return nil, fmt.Errorf("unsupported password algorithm %q in %s", c.Algorithm, path)
	}
	c.path = path
	if statErr == nil {
		c.lastStat = info
	}
	return &c, nil
}

// LoadOrCreateCredentials loads the verifier at path, creating one on first run.
// envPassword seeds it when set; otherwise a password is generated and returned
// as plaintext so the caller can show it once.
func LoadOrCreateCredentials(path, envPassword string) (creds *Credentials, plaintext string, err error) {
	if c, err := LoadCredentials(path); err == nil {
		return c, "", nil
	} else if !os.IsNotExist(err) {
		return nil, "", err
	}

	password := envPassword
	generated := ""
	if password == "" {
		password = GeneratePassword()
		generated = password
	}
	if len(password) < MinPasswordLen {
		return nil, "", fmt.Errorf("password must be at least %d characters", MinPasswordLen)
	}

	c, err := newCredentials(path, password)
	if err != nil {
		return nil, "", err
	}
	if err := c.save(); err != nil {
		return nil, "", err
	}
	return c, generated, nil
}

func (c *Credentials) save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".credentials-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, c.path); err != nil {
		return err
	}
	if info, err := os.Stat(c.path); err == nil {
		c.lastStat = info
	}
	return nil
}

// Verify reports whether password matches, comparing in constant time.
//
// The verifier metadata on disk is checked on every attempt and reloaded whenever
// the file inode, modification time, or size changes. The passwd command runs as a
// separate process against an already-running server, so without this a rotated
// password would silently keep accepting the old one until the service restarted.
// A failed stat or read leaves the in-memory copy in place, so a transient
// filesystem problem cannot lock everyone out. Concurrent PBKDF2 derivations are
// bounded by verifySem to prevent CPU exhaustion under parallel login floods.
func (c *Credentials) Verify(password string) bool {
	return c.VerifyContext(context.Background(), password)
}

// VerifyContext reports whether password matches while honoring ctx cancellation
// while waiting for a PBKDF2 worker slot.
func (c *Credentials) VerifyContext(ctx context.Context, password string) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return false
	}
	select {
	case verifySem <- struct{}{}:
		defer func() { <-verifySem }()
	case <-ctx.Done():
		return false
	}
	if ctx.Err() != nil {
		return false
	}

	c.mu.Lock()
	if c.path != "" {
		if info, statErr := os.Stat(c.path); statErr == nil {
			unchanged := c.lastStat != nil &&
				os.SameFile(c.lastStat, info) &&
				c.lastStat.ModTime().Equal(info.ModTime()) &&
				c.lastStat.Size() == info.Size() &&
				c.Salt != "" && c.Hash != ""
			if !unchanged {
				if fresh, err := LoadCredentials(c.path); err == nil {
					c.Algorithm, c.Iterations, c.Salt, c.Hash, c.UpdatedAt, c.lastStat =
						fresh.Algorithm, fresh.Iterations, fresh.Salt, fresh.Hash, fresh.UpdatedAt, fresh.lastStat
				}
			}
		}
	}
	saltEnc, hashEnc, iters := c.Salt, c.Hash, c.Iterations
	c.mu.Unlock()

	salt, err := base64.StdEncoding.DecodeString(saltEnc)
	if err != nil {
		return false
	}
	want, err := base64.StdEncoding.DecodeString(hashEnc)
	if err != nil {
		return false
	}

	if iters <= 0 {
		iters = iterations
	}

	got, err := derive(password, salt, iters)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// Set replaces the password. Existing sessions stay valid; revoke them
// separately if that is not what you want.
func (c *Credentials) Set(password string) error {
	if len(password) < MinPasswordLen {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLen)
	}

	next, err := newCredentials(c.path, password)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.Algorithm = next.Algorithm
	c.Iterations = next.Iterations
	c.Salt = next.Salt
	c.Hash = next.Hash
	c.UpdatedAt = next.UpdatedAt
	return c.save()
}
