package server

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeVncAuthServer plays the server side of an RFB 3.8 VNC-Authentication
// handshake against conn, using a fixed 16-byte challenge. It records the
// client's 16-byte response and, on success, sends a one-byte marker that the
// test can observe flowing through to the browser.
func fakeVncAuthServer(conn net.Conn, challenge []byte, gotResp chan<- []byte) {
	defer conn.Close()
	conn.Write([]byte("RFB 003.008\n"))
	ver := make([]byte, 12)
	if _, err := io.ReadFull(conn, ver); err != nil {
		return
	}
	// Offer security types: [count=1][VNC Auth (2)]
	conn.Write([]byte{1, 2})
	choice := make([]byte, 1)
	if _, err := io.ReadFull(conn, choice); err != nil || choice[0] != 2 {
		return
	}
	conn.Write(challenge)
	resp := make([]byte, 16)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return
	}
	gotResp <- resp
	conn.Write([]byte{0, 0, 0, 0}) // SecurityResult OK
	// After ClientInit from the browser, emit a marker as "ServerInit".
	ci := make([]byte, 1)
	io.ReadFull(conn, ci)
	conn.Write([]byte{0xAB})
}

func TestRfbAuthProxy(t *testing.T) {
	const password = "secret12"
	challenge := []byte("0123456789abcdef")

	// Expected DES response computed by the same routine the proxy uses.
	wantResp, err := vncDESResponse(password, challenge)
	if err != nil {
		t.Fatalf("vncDESResponse: %v", err)
	}

	serverSide, proxySide := net.Pipe()
	gotResp := make(chan []byte, 1)
	go fakeVncAuthServer(serverSide, challenge, gotResp)

	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		vncRelay(proxySide, c, password)
	}))
	defer ts.Close()

	cli, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer cli.Close()

	br := &wsReader{conn: cli}

	// Browser side of the handshake: the proxy must present RFB 3.8 + None.
	ver := make([]byte, 12)
	if _, err := io.ReadFull(br, ver); err != nil {
		t.Fatalf("read version: %v", err)
	}
	if string(ver) != "RFB 003.008\n" {
		t.Fatalf("browser version = %q, want RFB 003.008", ver)
	}
	cli.WriteMessage(websocket.BinaryMessage, []byte("RFB 003.008\n"))

	sec := make([]byte, 2)
	if _, err := io.ReadFull(br, sec); err != nil {
		t.Fatalf("read sec: %v", err)
	}
	if sec[0] != 1 || sec[1] != 1 { // count=1, None(1)
		t.Fatalf("security offer = %v, want [1 1] (None only)", sec)
	}
	cli.WriteMessage(websocket.BinaryMessage, []byte{1}) // choose None

	res := make([]byte, 4)
	if _, err := io.ReadFull(br, res); err != nil {
		t.Fatalf("read SecurityResult: %v", err)
	}
	if binary.BigEndian.Uint32(res) != 0 {
		t.Fatalf("SecurityResult = %v, want OK", res)
	}

	// The server must have received the correct DES response.
	select {
	case resp := <-gotResp:
		if !bytes.Equal(resp, wantResp) {
			t.Fatalf("DES response mismatch:\n got %x\nwant %x", resp, wantResp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never received auth response")
	}

	// ClientInit -> ServerInit marker flows transparently after auth.
	cli.WriteMessage(websocket.BinaryMessage, []byte{1}) // ClientInit (shared)
	marker := make([]byte, 1)
	cli.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(br, marker); err != nil {
		t.Fatalf("read post-auth marker: %v", err)
	}
	if marker[0] != 0xAB {
		t.Fatalf("post-auth marker = %#x, want 0xAB", marker[0])
	}
}
