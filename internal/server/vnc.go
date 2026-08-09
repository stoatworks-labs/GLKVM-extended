package server

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"rttys/internal/store/sqlite"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// VNC bridge: relays RFB between a browser-side noVNC WebSocket and a VNC
// server, either dialled directly by the cloud or tunnelled through a
// device's rtty connection (the same msgTypeHttp raw-TCP proxy the web
// proxy uses — both directions are byte-transparent, so RFB passes as-is).

const vncDialTimeout = 10 * time.Second

// vncChunkSize keeps each msgTypeHttp frame well under the uint16 length
// limit of the rtty framing (and matches the web proxy's read size).
const vncChunkSize = 4096

// wsWriter serialises writes to a websocket connection, since both the
// backend reader and control paths may write concurrently.
type wsWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
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

	// Resolve the transport before upgrading so connection errors surface
	// as HTTP statuses the frontend can distinguish.
	var dev *Device
	if ep.ViaDevice != "" {
		dev = srv.GetDevice(c.Query("group"), ep.ViaDevice)
		if dev == nil {
			c.Status(http.StatusBadGateway) // tunnel device offline
			return
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
			cont.NotificationSvc.NotifyRemoteAccess("VNC", ep.ViaDevice, ep.Name, actorName, c.ClientIP())
		}
	}
	defer func() {
		if logID > 0 && cont.DeviceLogSvc != nil {
			cont.DeviceLogSvc.EndSession(context.Background(), logID)
		}
	}()

	if dev != nil {
		vncTunnelBridge(dev, ep.Addr, conn)
	} else {
		vncDirectBridge(ep.Addr, conn)
	}
}

// vncDirectBridge dials the VNC server from the cloud host.
func vncDirectBridge(addr string, ws *websocket.Conn) {
	tcp, err := net.DialTimeout("tcp", addr, vncDialTimeout)
	if err != nil {
		log.Warn().Err(err).Msgf("vnc: direct dial %s failed", addr)
		return
	}
	defer tcp.Close()

	w := &wsWriter{conn: ws}
	done := make(chan struct{}, 2)

	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 32*1024)
		for {
			n, err := tcp.Read(buf)
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
			if _, err := tcp.Write(data); err != nil {
				return
			}
		}
	}()

	<-done
	// Unblock the peer goroutine.
	tcp.Close()
	ws.Close()
	<-done
}

// vncTunnelConn adapts the device tunnel to the net.Conn the msgTypeHttp
// return path (handleHttpMsg) expects: Write delivers VNC-server bytes to
// the browser, Close tears the websocket down.
type vncTunnelConn struct {
	w     *wsWriter
	ws    *websocket.Conn
	close sync.Once
}

func (t *vncTunnelConn) Write(p []byte) (int, error) {
	if err := t.w.writeBinary(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (t *vncTunnelConn) Close() error {
	t.close.Do(func() { t.ws.Close() })
	return nil
}

func (t *vncTunnelConn) Read(p []byte) (int, error)       { return 0, net.ErrClosed }
func (t *vncTunnelConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (t *vncTunnelConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (t *vncTunnelConn) SetDeadline(time.Time) error      { return nil }
func (t *vncTunnelConn) SetReadDeadline(time.Time) error  { return nil }
func (t *vncTunnelConn) SetWriteDeadline(time.Time) error { return nil }

// vncTunnelBridge relays through the device's rtty connection. The device
// side (rtty http proxy) dials destAddr on the first frame for an unseen
// source address — an empty payload still triggers the dial, which matters
// because RFB servers speak first.
func vncTunnelBridge(dev *Device, addr string, ws *websocket.Conn) {
	destAddr := genDestAddr(addr)
	if destAddr == nil {
		log.Warn().Msgf("vnc: invalid tunnel addr %s", addr)
		return
	}

	tcpAddr, ok := ws.RemoteAddr().(*net.TCPAddr)
	if !ok {
		log.Warn().Msg("vnc: websocket remote addr is not TCP")
		return
	}
	srcAddr := tcpAddr2Bytes(tcpAddr)
	key := string(srcAddr)

	w := &wsWriter{conn: ws}
	tc := &vncTunnelConn{w: w, ws: ws}

	dev.https.Store(key, net.Conn(tc))
	defer func() {
		dev.https.Delete(key)
		// Tell the device to close its side.
		sendHttpReq(dev, false, srcAddr, destAddr, nil)
	}()

	// Close the websocket when the device drops.
	ctx, cancel := context.WithCancel(dev.ctx)
	defer cancel()
	go func() {
		<-ctx.Done()
		tc.Close()
	}()

	// Kick off the device-side dial so the VNC server's greeting can flow
	// before the browser has sent a single byte.
	sendHttpReq(dev, false, srcAddr, destAddr, nil)

	for {
		typ, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if typ != websocket.BinaryMessage {
			continue
		}
		for len(data) > 0 {
			n := min(len(data), vncChunkSize)
			sendHttpReq(dev, false, srcAddr, destAddr, data[:n])
			data = data[n:]
		}
	}
}

func (w *wsWriter) writeBinary(p []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(websocket.BinaryMessage, p)
}
