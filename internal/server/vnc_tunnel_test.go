package server

import (
	"bufio"
	"context"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// readDeviceFrame reads one rtty frame from the device side of the pipe and
// returns (msgType, payload-after-sid). VNC frames carry an empty sid.
func readDeviceFrame(t *testing.T, br *bufio.Reader) (byte, []byte) {
	t.Helper()
	head := make([]byte, 3)
	if _, err := br.Read(head[:1]); err != nil {
		t.Fatalf("read type: %v", err)
	}
	if _, err := br.Read(head[1:3]); err != nil {
		t.Fatalf("read len: %v", err)
	}
	n := binary.BigEndian.Uint16(head[1:3])
	body := make([]byte, n)
	got := 0
	for got < int(n) {
		m, err := br.Read(body[got:])
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		got += m
	}
	return head[0], body
}

// TestVncTunnelBridge exercises the VNC-specific tunnel logic against a fake
// device: the initial dial-kick frame, browser->device chunking, and the
// device->browser return path via handleHttpMsg. The rtty framing itself is
// the same path the shipping web proxy uses.
func TestVncTunnelBridge(t *testing.T) {
	// Device with a pipe conn so we can read what the bridge writes to it.
	serverConn, deviceConn := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dev := &Device{
		id:    "test-dev",
		proto: 4, // proto>3 => sendHttpReq prefixes an https flag byte
		conn:  serverConn,
		ctx:   ctx,
	}

	frames := make(chan struct {
		typ  byte
		body []byte
	}, 16)
	go func() {
		br := bufio.NewReader(deviceConn)
		for {
			typ, body := readDeviceFrame(t, br)
			frames <- struct {
				typ  byte
				body []byte
			}{typ, body}
		}
	}()

	// Real gorilla websocket pair via httptest.
	var srvWS *websocket.Conn
	ready := make(chan struct{})
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		srvWS = c
		close(ready)
		vncTunnelBridge(dev, "127.0.0.1:5901", c)
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	cli, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer cli.Close()
	<-ready

	// 1) Initial dial-kick: an empty-payload frame so the device connects to
	//    the VNC server before the browser sends anything (RFB server speaks
	//    first).
	f := <-frames
	if f.typ != msgTypeHttp {
		t.Fatalf("dial-kick: got msgType %d, want %d", f.typ, msgTypeHttp)
	}
	// body = [httpsFlag][srcAddr 18][destAddr 6][payload]
	if len(f.body) != 1+18+6 {
		t.Fatalf("dial-kick body len = %d, want %d (no payload)", len(f.body), 1+18+6)
	}
	srcAddr := append([]byte(nil), f.body[1:1+18]...)
	destIP := net.IP(f.body[1+18 : 1+18+4])
	destPort := binary.BigEndian.Uint16(f.body[1+18+4 : 1+18+6])
	if destIP.String() != "127.0.0.1" || destPort != 5901 {
		t.Fatalf("dest = %s:%d, want 127.0.0.1:5901", destIP, destPort)
	}

	// 2) Browser -> device: payload is forwarded verbatim.
	if err := cli.WriteMessage(websocket.BinaryMessage, []byte("hello-rfb")); err != nil {
		t.Fatalf("client write: %v", err)
	}
	f = <-frames
	payload := f.body[1+18+6:]
	if string(payload) != "hello-rfb" {
		t.Fatalf("forwarded payload = %q, want %q", payload, "hello-rfb")
	}

	// 3) Device -> browser: handleHttpMsg writes to the conn registered under
	//    srcAddr (our vncTunnelConn), which pushes onto the websocket.
	msg := append(append([]byte(nil), srcAddr...), []byte("server-greeting")...)
	if err := handleHttpMsg(dev, msg); err != nil {
		t.Fatalf("handleHttpMsg: %v", err)
	}
	cli.SetReadDeadline(time.Now().Add(2 * time.Second))
	typ, data, err := cli.ReadMessage()
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if typ != websocket.BinaryMessage || string(data) != "server-greeting" {
		t.Fatalf("browser got (%d,%q), want binary %q", typ, data, "server-greeting")
	}

	// 4) Large browser payloads are split into <=vncChunkSize frames.
	big := make([]byte, vncChunkSize+100)
	for i := range big {
		big[i] = byte(i)
	}
	if err := cli.WriteMessage(websocket.BinaryMessage, big); err != nil {
		t.Fatalf("client write big: %v", err)
	}
	f = <-frames
	if got := len(f.body) - (1 + 18 + 6); got != vncChunkSize {
		t.Fatalf("first chunk payload = %d, want %d", got, vncChunkSize)
	}
	f = <-frames
	if got := len(f.body) - (1 + 18 + 6); got != 100 {
		t.Fatalf("second chunk payload = %d, want 100", got)
	}

	_ = srvWS
}
