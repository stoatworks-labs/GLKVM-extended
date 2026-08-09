package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"rttys/internal/domain/vncendpoint"
	"rttys/internal/pkg/vnccrypt"
	"rttys/internal/store/sqlite"
	"rttys/utils"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// VNC bridge: relays RFB between a browser-side noVNC WebSocket and a VNC
// server. Both transports expose the server as a net.Conn:
//   - direct: the cloud dials the server (net.Dial), gated by an optional
//     CIDR allowlist.
//   - tunnel: the connection rides a device's rtty link via the msgTypeHttp
//     raw-TCP proxy (byte-transparent, so RFB passes as-is).
//
// When the endpoint has a stored password, an RFB auth-proxy terminates VNC
// Authentication toward the server and presents "None" toward the browser,
// so the password never leaves the cloud.

const vncDialTimeout = 10 * time.Second

// vncChunkSize keeps each msgTypeHttp frame well under the uint16 length
// limit of the rtty framing (and matches the web proxy's read size).
const vncChunkSize = 4096

// wsWriter serialises writes to a websocket connection.
type wsWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (w *wsWriter) writeBinary(p []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(websocket.BinaryMessage, p)
}

func handleVncConnection(srv *RttyServer, c *gin.Context) {
	defer LogPanic()

	cont := sqlite.TryContainer()
	if cont == nil || cont.VncEndpointRepo == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.Status(http.StatusBadRequest)
		return
	}

	ep, err := cont.VncEndpointRepo.FindByID(c.Request.Context(), id)
	if err != nil || ep == nil {
		c.Status(http.StatusNotFound)
		return
	}

	if c.GetHeader("Upgrade") != "websocket" {
		c.Status(http.StatusBadRequest)
		return
	}

	// Resolve the transport before upgrading so errors surface as HTTP
	// statuses the frontend can distinguish.
	var dev *Device
	if ep.ViaDevice != "" {
		dev = srv.GetDevice(c.Query("group"), ep.ViaDevice)
		if dev == nil {
			c.Status(http.StatusBadGateway) // tunnel device offline
			return
		}
	} else {
		// Direct dial: enforce the CIDR allowlist before doing anything.
		host, _, splitErr := net.SplitHostPort(ep.Addr)
		if splitErr != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		allow := utils.ParseCIDRs(srv.cfg.VncDirectAllowlist)
		if !utils.HostAllowed(allow, host) {
			log.Warn().Msgf("vnc: direct dial to %s blocked by allowlist", ep.Addr)
			c.Status(http.StatusForbidden)
			return
		}
	}

	// The RFB auth-proxy only applies to VNC; RDP (IronRDP-web) and xpra
	// (xpra-html5) perform their own authentication client-side, so the bridge
	// stays byte-transparent for them.
	password := ""
	if ep.Kind == vncendpoint.KindVNC {
		secret := srv.cfg.VncSecret
		if secret == "" {
			secret = srv.cfg.Token
		}
		pw, derr := vnccrypt.Decrypt(secret, ep.PasswordEnc)
		if derr != nil {
			log.Warn().Err(derr).Msgf("vnc: cannot decrypt password for endpoint %d", ep.ID)
		} else {
			password = pw
		}
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Error().Err(err).Msg("vnc: upgrade to websocket failed")
		return
	}
	defer conn.Close()

	// Session audit log, mirroring the SSH/web/control paths.
	var logID int64
	if cont.DeviceLogSvc != nil {
		actorID, actorName := principalFromCtx(c)
		logID = cont.DeviceLogSvc.StartRemoteVncSession(
			c.Request.Context(), ep.ViaDevice, ep.Name, actorID, actorName, c.ClientIP(), ep.Addr)
		if cont.NotificationSvc != nil {
			cont.NotificationSvc.NotifyRemoteAccess(strings.ToUpper(string(ep.Kind)), ep.ViaDevice, ep.Name, actorName, c.ClientIP())
		}
	}
	defer func() {
		if logID > 0 && cont.DeviceLogSvc != nil {
			cont.DeviceLogSvc.EndSession(context.Background(), logID)
		}
	}()

	// Build the server-facing net.Conn for the chosen transport.
	var server net.Conn
	if dev != nil {
		server = dialVncTunnel(dev, ep.Addr)
		if server == nil {
			return
		}
	} else {
		tcp, derr := net.DialTimeout("tcp", ep.Addr, vncDialTimeout)
		if derr != nil {
			log.Warn().Err(derr).Msgf("vnc: direct dial %s failed", ep.Addr)
			return
		}
		server = tcp
	}
	defer server.Close()

	vncRelay(server, conn, password)
}

// vncRelay optionally runs the RFB auth-proxy, then pumps bytes transparently
// between the VNC server and the browser websocket in both directions.
func vncRelay(server net.Conn, ws *websocket.Conn, password string) {
	w := &wsWriter{conn: ws}

	if password != "" {
		if err := rfbAuthProxy(server, ws, w, password); err != nil {
			log.Warn().Err(err).Msg("vnc: RFB auth proxy failed")
			return
		}
	}

	done := make(chan struct{}, 2)

	// server -> browser
	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 32*1024)
		for {
			n, err := server.Read(buf)
			if n > 0 {
				if werr := w.writeBinary(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// browser -> server
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			typ, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if typ != websocket.BinaryMessage {
				continue
			}
			if _, err := server.Write(data); err != nil {
				return
			}
		}
	}()

	<-done
	server.Close()
	ws.Close()
	<-done
}

// --- tunnel transport as a net.Conn ------------------------------------

// tunnelInbound is stored in dev.https; handleHttpMsg calls Write with bytes
// arriving from the VNC server and Close when the device drops the socket.
// It feeds an io.Pipe that tunnelConn.Read drains. It satisfies net.Conn so
// the existing type assertion in handleHttpMsg holds; only Write/Close are
// exercised there.
type tunnelInbound struct {
	pw        *io.PipeWriter
	closeOnce sync.Once
}

func (t *tunnelInbound) Write(p []byte) (int, error) { return t.pw.Write(p) }
func (t *tunnelInbound) Close() error {
	t.closeOnce.Do(func() { t.pw.CloseWithError(io.EOF) })
	return nil
}
func (t *tunnelInbound) Read([]byte) (int, error)        { return 0, io.EOF }
func (t *tunnelInbound) LocalAddr() net.Addr             { return &net.TCPAddr{} }
func (t *tunnelInbound) RemoteAddr() net.Addr            { return &net.TCPAddr{} }
func (t *tunnelInbound) SetDeadline(time.Time) error     { return nil }
func (t *tunnelInbound) SetReadDeadline(time.Time) error { return nil }
func (t *tunnelInbound) SetWriteDeadline(time.Time) error {
	return nil
}

// tunnelConn is the server-facing net.Conn handed to vncRelay: Read drains
// bytes the device sent back; Write chunks outbound bytes into msgTypeHttp
// frames toward the device.
type tunnelConn struct {
	pr        *io.PipeReader
	dev       *Device
	srcAddr   []byte
	destAddr  []byte
	closeOnce sync.Once
}

func (t *tunnelConn) Read(p []byte) (int, error) { return t.pr.Read(p) }

func (t *tunnelConn) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		n := min(len(p), vncChunkSize)
		sendHttpReq(t.dev, false, t.srcAddr, t.destAddr, p[:n])
		p = p[n:]
	}
	return total, nil
}

func (t *tunnelConn) Close() error {
	t.closeOnce.Do(func() {
		t.dev.https.Delete(string(t.srcAddr))
		sendHttpReq(t.dev, false, t.srcAddr, t.destAddr, nil) // close device side
		t.pr.CloseWithError(io.EOF)
	})
	return nil
}

func (t *tunnelConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (t *tunnelConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (t *tunnelConn) SetDeadline(time.Time) error      { return nil }
func (t *tunnelConn) SetReadDeadline(time.Time) error  { return nil }
func (t *tunnelConn) SetWriteDeadline(time.Time) error { return nil }

// dialVncTunnel wires a device tunnel as a net.Conn to the VNC server. The
// device's rtty http proxy dials destAddr on the first frame for an unseen
// source address; an empty payload still triggers the dial, which matters
// because RFB servers speak first.
func dialVncTunnel(dev *Device, addr string) net.Conn {
	destAddr := genDestAddr(addr)
	if destAddr == nil {
		log.Warn().Msgf("vnc: invalid tunnel addr %s", addr)
		return nil
	}
	// A synthetic, unique source address keys this stream on the device.
	srcAddr := tcpAddr2Bytes(&net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: int(time.Now().UnixNano() & 0xffff),
	})
	key := string(srcAddr)

	pr, pw := io.Pipe()
	dev.https.Store(key, net.Conn(&tunnelInbound{pw: pw}))

	tc := &tunnelConn{pr: pr, dev: dev, srcAddr: srcAddr, destAddr: destAddr}

	// Close the tunnel when the device context ends.
	go func() {
		<-dev.ctx.Done()
		tc.Close()
	}()

	// Kick the device-side dial so the server greeting can arrive first.
	sendHttpReq(dev, false, srcAddr, destAddr, nil)
	return tc
}
