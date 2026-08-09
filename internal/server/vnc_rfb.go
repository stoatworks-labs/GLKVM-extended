package server

import (
	"crypto/des" //nolint:gosec // VNC Authentication is defined in terms of DES
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/gorilla/websocket"
)

// RFB auth-proxy: performs the RFB handshake on both sides so the browser
// never sees the VNC password. Toward the server it completes VNC
// Authentication (security type 2) using the stored password; toward the
// browser it offers "None" and reports success. After this returns, both
// peers are positioned at ClientInit and the caller pumps bytes transparently.

// wsReader adapts a websocket's binary messages to an io.Reader so exact RFB
// fields can be read regardless of frame boundaries. Leftover buffered bytes
// (browser data read past the handshake) are exposed via Leftover.
type wsReader struct {
	conn *websocket.Conn
	buf  []byte
}

func (r *wsReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		typ, data, err := r.conn.ReadMessage()
		if err != nil {
			return 0, err
		}
		if typ != websocket.BinaryMessage {
			continue
		}
		r.buf = data
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func (r *wsReader) Leftover() []byte { return r.buf }

// reverseBits reverses the bit order of a byte, as VNC Authentication mangles
// each key byte before use.
func reverseBits(b byte) byte {
	var out byte
	for i := 0; i < 8; i++ {
		out <<= 1
		out |= b & 1
		b >>= 1
	}
	return out
}

// vncDESResponse computes the 16-byte VNC Authentication response for a
// 16-byte challenge using the (bit-reversed) password key.
func vncDESResponse(password string, challenge []byte) ([]byte, error) {
	key := make([]byte, 8)
	copy(key, password) // truncated to 8 bytes / zero-padded, per VNC
	for i := range key {
		key[i] = reverseBits(key[i])
	}
	block, err := des.NewCipher(key) //nolint:gosec // required by the VNC spec
	if err != nil {
		return nil, err
	}
	out := make([]byte, 16)
	block.Encrypt(out[0:8], challenge[0:8])
	block.Encrypt(out[8:16], challenge[8:16])
	return out, nil
}

func parseRFBMinor(ver []byte) (int, bool) {
	if len(ver) < 12 || string(ver[0:4]) != "RFB " {
		return 0, false
	}
	minor, err := strconv.Atoi(string(ver[8:11]))
	if err != nil {
		return 0, false
	}
	return minor, true
}

// rfbAuthProxy runs the handshake on both sides. On success it forwards any
// browser bytes buffered past the handshake to the server, so the caller can
// begin a clean transparent pump.
func rfbAuthProxy(server net.Conn, ws *websocket.Conn, w *wsWriter, password string) error {
	// ---- server side ----
	sver := make([]byte, 12)
	if _, err := io.ReadFull(server, sver); err != nil {
		return fmt.Errorf("read server version: %w", err)
	}
	minor, ok := parseRFBMinor(sver)
	if !ok {
		return fmt.Errorf("unexpected server version %q", sver)
	}

	ourMinor := 8
	if minor < 8 {
		ourMinor = minor
	}
	if _, err := server.Write([]byte(fmt.Sprintf("RFB 003.%03d\n", ourMinor))); err != nil {
		return err
	}

	var chosen byte
	if minor >= 7 {
		cnt := make([]byte, 1)
		if _, err := io.ReadFull(server, cnt); err != nil {
			return err
		}
		if cnt[0] == 0 {
			return fmt.Errorf("server offered no security types")
		}
		types := make([]byte, cnt[0])
		if _, err := io.ReadFull(server, types); err != nil {
			return err
		}
		chosen = selectSecurityType(types)
		if chosen == 0 {
			return fmt.Errorf("no supported security type (offered %v)", types)
		}
		if _, err := server.Write([]byte{chosen}); err != nil {
			return err
		}
	} else {
		// RFB 3.3: server dictates a single 4-byte security type.
		t := make([]byte, 4)
		if _, err := io.ReadFull(server, t); err != nil {
			return err
		}
		chosen = byte(binary.BigEndian.Uint32(t))
		if chosen == 0 {
			return fmt.Errorf("server rejected connection")
		}
	}

	if chosen == 2 { // VNC Authentication
		challenge := make([]byte, 16)
		if _, err := io.ReadFull(server, challenge); err != nil {
			return err
		}
		resp, err := vncDESResponse(password, challenge)
		if err != nil {
			return err
		}
		if _, err := server.Write(resp); err != nil {
			return err
		}
	}

	// SecurityResult: sent by 3.7/3.8 always, and by 3.3 only after VNC auth.
	if minor >= 7 || chosen == 2 {
		res := make([]byte, 4)
		if _, err := io.ReadFull(server, res); err != nil {
			return err
		}
		if binary.BigEndian.Uint32(res) != 0 {
			return fmt.Errorf("server authentication failed")
		}
	}

	// ---- browser side ----
	br := &wsReader{conn: ws}
	if err := w.writeBinary([]byte("RFB 003.008\n")); err != nil {
		return err
	}
	cver := make([]byte, 12)
	if _, err := io.ReadFull(br, cver); err != nil {
		return fmt.Errorf("read browser version: %w", err)
	}
	// Offer only "None" — the browser is already authenticated to the cloud.
	if err := w.writeBinary([]byte{1, 1}); err != nil {
		return err
	}
	choice := make([]byte, 1)
	if _, err := io.ReadFull(br, choice); err != nil {
		return err
	}
	// SecurityResult OK.
	if err := w.writeBinary([]byte{0, 0, 0, 0}); err != nil {
		return err
	}

	// Forward any browser bytes already buffered past the handshake.
	if left := br.Leftover(); len(left) > 0 {
		if _, err := server.Write(left); err != nil {
			return err
		}
	}
	return nil
}

// selectSecurityType prefers VNC Authentication (2), falling back to None (1);
// returns 0 when neither is offered (e.g. VeNCrypt-only servers).
func selectSecurityType(types []byte) byte {
	hasNone := false
	for _, t := range types {
		if t == 2 {
			return 2
		}
		if t == 1 {
			hasNone = true
		}
	}
	if hasNone {
		return 1
	}
	return 0
}
