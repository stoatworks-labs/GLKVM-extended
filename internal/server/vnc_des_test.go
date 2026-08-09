package server

import (
	"encoding/hex"
	"testing"
)

// TestVncDESCrossCheck pins vncDESResponse to an independent DES engine.
// Reference produced by OpenSSL 3.5 (legacy provider):
//
//	key = bit-reverse(each byte of "secret12") = cea6c64ea62e8c4c
//	printf '0123456789abcdef' | \
//	  openssl enc -des-ecb -K cea6c64ea62e8c4c -nopad -provider legacy
//	=> 5f15f4f0e1684cdc260ea962ab82fa3b
//
// This confirms the VNC-specific bit-reversal + DES-ECB matches a separate
// implementation, not just our own.
func TestVncDESCrossCheck(t *testing.T) {
	resp, err := vncDESResponse("secret12", []byte("0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	got := hex.EncodeToString(resp)
	const want = "5f15f4f0e1684cdc260ea962ab82fa3b"
	if got != want {
		t.Fatalf("VNC DES mismatch:\n got %s\nwant %s (OpenSSL)", got, want)
	}
}
