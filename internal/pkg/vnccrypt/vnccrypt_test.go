package vnccrypt

import "testing"

func TestRoundTrip(t *testing.T) {
	enc, err := Encrypt("server-secret", "hunter2")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if enc == "" {
		t.Fatal("ciphertext is empty")
	}
	got, err := Decrypt("server-secret", enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != "hunter2" {
		t.Fatalf("round-trip = %q, want hunter2", got)
	}
}

func TestWrongKeyFails(t *testing.T) {
	enc, _ := Encrypt("key-a", "pw")
	if _, err := Decrypt("key-b", enc); err == nil {
		t.Fatal("decrypt with wrong key should fail")
	}
}

func TestNoSecret(t *testing.T) {
	if _, err := Encrypt("", "pw"); err != ErrNoSecret {
		t.Fatalf("Encrypt with no secret: got %v, want ErrNoSecret", err)
	}
	// Empty ciphertext decrypts to empty password without needing a key.
	if got, err := Decrypt("", ""); err != nil || got != "" {
		t.Fatalf("Decrypt empty: got (%q,%v)", got, err)
	}
}
