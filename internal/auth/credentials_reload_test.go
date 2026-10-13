package auth

import (
	"path/filepath"
	"testing"
)

// A password set by the passwd command reaches an already-running server only if
// Verify re-reads the file, so this guards against the old password continuing to
// work after a rotation.
func TestVerifyPicksUpAPasswordSetByAnotherProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")

	serving, _, err := LoadOrCreateCredentials(path, "first-password")
	if err != nil {
		t.Fatal(err)
	}
	if !serving.Verify("first-password") {
		t.Fatal("the password it was created with should verify")
	}

	rotating, err := LoadCredentials(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := rotating.Set("second-password"); err != nil {
		t.Fatal(err)
	}

	if serving.Verify("first-password") {
		t.Error("the rotated-away password still verifies")
	}
	if !serving.Verify("second-password") {
		t.Error("the newly set password does not verify")
	}
}

func TestSessionRevokeAllByAnotherProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")

	serving, err := NewSessionStore(path, 3600e9)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := serving.Create("iPhone", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if !serving.Validate(tok) {
		t.Fatal("fresh session should validate")
	}

	// Simulate LastSeen becoming dirty in memory before external CLI revocation
	serving.mu.Lock()
	for _, s := range serving.sessions {
		s.LastSeen = s.LastSeen.Add(-2 * 60e9)
	}
	serving.mu.Unlock()
	if !serving.Validate(tok) {
		t.Fatal("session should still validate before revocation")
	}

	// External CLI process runs 'agy-server sessions revoke'
	cliStore, err := NewSessionStore(path, 3600e9)
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := cliStore.RevokeAll()
	if err != nil {
		t.Fatal(err)
	}
	if revoked != 1 {
		t.Fatalf("expected 1 revoked session, got %d", revoked)
	}

	if serving.Validate(tok) {
		t.Error("revoked session must not validate on the running server")
	}
	if err := serving.Flush(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewSessionStore(path, 3600e9)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Validate(tok) {
		t.Error("Flush on the running server must not resurrect sessions revoked by another process")
	}
}

func TestSessionStoreConcurrentValidateAndFlush(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store, err := NewSessionStore(path, 3600e9)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := store.Create("Mac", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			store.mu.Lock()
			if s, ok := store.sessions[hashToken(tok)]; ok {
				s.LastSeen = s.LastSeen.Add(-2 * 60e9)
			}
			store.mu.Unlock()
			_ = store.Validate(tok)
		}
	}()

	for i := 0; i < 50; i++ {
		_ = store.Flush()
	}
	<-done
}
