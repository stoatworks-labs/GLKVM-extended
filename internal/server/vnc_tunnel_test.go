package server

import (
	"bufio"
	"context"
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

// readDeviceFrame reads one rtty frame from the device side of the pipe and
// returns (msgType, payload-after-sid). VNC frames carry an empty sid.
func readDeviceFrame(t *testing.T, br *bufio.Reader) (byte, []byte) {
	t.Helper()
	head := make([]byte, 3)
	if _, err := io.ReadFull(br, head); err != nil {
		t.Fatalf("read header: %v", err)
	}
	n := binary.BigEndian.Uint16(head[1:3])
	body := make([]byte, n)
	if _, err := io.ReadFull(br, body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	return head[0], body
}

// newFakeDevice returns a Device whose conn is a pipe, plus a channel of the
// frames it writes (msgType + payload-after-sid).
func newFakeDevice(t *testing.T) (*Device, <-chan []byte, context.CancelFunc) {
	t.Helper()
	serverConn, deviceConn := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	dev := &Device{id: "test-dev", proto: 4, conn: serverConn, ctx: ctx}

	frames := make(chan []byte, 32)
	go func() {
		br := bufio.NewReader(deviceConn)
		for {
			typ, body := readDeviceFrame(t, br)
			if typ == msgTypeHttp {
				frames <- body
			}
		}
	}()
	return dev, frames, cancel
}

// framePayload extracts the payload from a proto>3 msgHttp frame body:
// [httpsFlag][srcAddr 18][destAddr 6][payload].
func framePayload(body []byte) []byte { return body[1+18+6:] }

// TestVncTunnelBridge exercises the tunnel transport end-to-end through
// vncRelay: the initial dial-kick, browser->device chunking, and the
// device->browser return path via handleHttpMsg.
func TestVncTunnelBridge(t *testing.T) {
	dev, frames, cancel := newFakeDevice(t)
	defer cancel()

	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		server := dialVncTunnel(dev, "127.0.0.1:5901")
		if server == nil {
			t.Errorf("dialVncTunnel returned nil")
			return
		}
		vncRelay(server, c, "") // no password: transparent
	}))
	defer ts.Close()

	cli, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer cli.Close()

	// 1) Dial-kick: empty-payload frame so the device connects first.
	body := <-frames
	if len(body) != 1+18+6 {
		t.Fatalf("dial-kick body len = %d, want %d", len(body), 1+18+6)
	}
	srcAddr := append([]byte(nil), body[1:1+18]...)
	destIP := net.IP(body[1+18 : 1+18+4])
	destPort := binary.BigEndian.Uint16(body[1+18+4 : 1+18+6])
	if destIP.String() != "127.0.0.1" || destPort != 5901 {
		t.Fatalf("dest = %s:%d, want 127.0.0.1:5901", destIP, destPort)
	}

	// 2) Browser -> device forwarding.
	if err := cli.WriteMessage(websocket.BinaryMessage, []byte("hello-rfb")); err != nil {
		t.Fatalf("client write: %v", err)
	}
	if got := framePayload(<-frames); string(got) != "hello-rfb" {
		t.Fatalf("forwarded payload = %q, want %q", got, "hello-rfb")
	}

	// 3) Device -> browser via handleHttpMsg.
	if err := handleHttpMsg(dev, append(append([]byte(nil), srcAddr...), []byte("server-greeting")...)); err != nil {
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

	// 4) Large browser payloads split into <=vncChunkSize frames.
	big := make([]byte, vncChunkSize+100)
	if err := cli.WriteMessage(websocket.BinaryMessage, big); err != nil {
		t.Fatalf("client write big: %v", err)
	}
	if got := len(framePayload(<-frames)); got != vncChunkSize {
		t.Fatalf("first chunk = %d, want %d", got, vncChunkSize)
	}
	if got := len(framePayload(<-frames)); got != 100 {
		t.Fatalf("second chunk = %d, want 100", got)
	}
}
