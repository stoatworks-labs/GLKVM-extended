package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"rttys/internal/domain/vncendpoint"
	"rttys/internal/pkg/vnccrypt"
	"rttys/internal/store/sqlite"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// Guacamole proxy mode: for endpoints with authMode=proxy, glkvm-cloud does not
// byte-tunnel the protocol. Instead it connects to a guacd daemon (Apache
// Guacamole), performs the guacd handshake with server-held credentials, and
// relays the Guacamole instruction stream to a guacamole-common-js viewer in
// the browser — so RDP/VNC credentials never reach the client.

const guacDialTimeout = 10 * time.Second

// guacEncode builds one Guacamole instruction: LEN.VALUE,LEN.VALUE,...;
// where LEN is the number of Unicode characters in VALUE.
func guacEncode(elems ...string) []byte {
	var b strings.Builder
	for i, e := range elems {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(utf8.RuneCountInString(e)))
		b.WriteByte('.')
		b.WriteString(e)
	}
	b.WriteByte(';')
	return []byte(b.String())
}

// guacReadInstruction reads one instruction from r and returns its elements.
func guacReadInstruction(r *bufio.Reader) ([]string, error) {
	var elems []string
	for {
		// length: digits until '.'
		lenStr, err := r.ReadString('.')
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(strings.TrimSuffix(lenStr, "."))
		if err != nil {
			return nil, fmt.Errorf("guac: bad length %q", lenStr)
		}
		// value: n runes
		val := make([]rune, 0, n)
		for i := 0; i < n; i++ {
			ru, _, err := r.ReadRune()
			if err != nil {
				return nil, err
			}
			val = append(val, ru)
		}
		elems = append(elems, string(val))
		// separator: ',' (more) or ';' (end)
		sep, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if sep == ';' {
			return elems, nil
		}
		if sep != ',' {
			return nil, fmt.Errorf("guac: bad separator %q", sep)
		}
	}
}

// guacHandshake performs the guacd handshake for a protocol using params, and
// returns the buffered reader positioned just after the `ready` instruction
// plus the `ready` instruction's elements (so the caller can forward it).
func guacHandshake(rw io.ReadWriter, protocol string, params map[string]string, w, h, dpi int) (*bufio.Reader, []string, error) {
	br := bufio.NewReader(rw)

	if _, err := rw.Write(guacEncode("select", protocol)); err != nil {
		return nil, nil, err
	}
	args, err := guacReadInstruction(br)
	if err != nil {
		return nil, nil, fmt.Errorf("read args: %w", err)
	}
	if len(args) == 0 || args[0] != "args" {
		return nil, nil, fmt.Errorf("expected args, got %v", firstOr(args))
	}

	// Client handshake: size, then supported audio/video/image mimetypes
	// (none advertised here), then connect with values for each requested arg.
	if _, err := rw.Write(guacEncode("size", strconv.Itoa(w), strconv.Itoa(h), strconv.Itoa(dpi))); err != nil {
		return nil, nil, err
	}
	if _, err := rw.Write(guacEncode("audio")); err != nil {
		return nil, nil, err
	}
	if _, err := rw.Write(guacEncode("video")); err != nil {
		return nil, nil, err
	}
	if _, err := rw.Write(guacEncode("image")); err != nil {
		return nil, nil, err
	}

	// args[1] is the protocol version; args[2:] are the parameter names, in the
	// order the connect instruction must supply values.
	connect := make([]string, 0, len(args))
	connect = append(connect, "connect")
	for _, name := range args[2:] {
		connect = append(connect, params[name]) // "" when we have no value
	}
	if _, err := rw.Write(guacEncode(connect...)); err != nil {
		return nil, nil, err
	}

	ready, err := guacReadInstruction(br)
	if err != nil {
		return nil, nil, fmt.Errorf("read ready: %w", err)
	}
	if len(ready) == 0 || ready[0] != "ready" {
		return nil, nil, fmt.Errorf("expected ready, got %v", firstOr(ready))
	}
	return br, ready, nil
}

func firstOr(s []string) string {
	if len(s) == 0 {
		return "<empty>"
	}
	return s[0]
}

// guacProtocolFor maps an endpoint kind to a guacd protocol. xpra is not
// supported by guacd (client-side only), so proxy mode rejects it.
func guacProtocolFor(k vncendpoint.Kind) (string, bool) {
	switch k {
	case vncendpoint.KindRDP:
		return "rdp", true
	case vncendpoint.KindVNC:
		return "vnc", true
	default:
		return "", false
	}
}

// guacParams builds the guacd connection parameters from an endpoint + its
// decrypted password.
func guacParams(ep *vncendpoint.Endpoint, password string) map[string]string {
	host, port, err := net.SplitHostPort(ep.Addr)
	if err != nil {
		host = ep.Addr
	}
	p := map[string]string{
		"hostname": host,
		"port":     port,
		"username": ep.Username,
		"password": password,
	}
	if ep.Kind == vncendpoint.KindRDP {
		p["domain"] = ep.Domain
		p["security"] = "any"     // negotiate (NLA/TLS/RDP)
		p["ignore-cert"] = "true" // self-signed desktops
		p["resize-method"] = "display-update"
	}
	return p
}

func (srv *RttyServer) guacdAddr() string {
	if v := strings.TrimSpace(srv.cfg.GuacdAddr); v != "" {
		return v
	}
	return "127.0.0.1:4822"
}

// handleGuacConnection bridges a browser (guacamole-common-js) to guacd for a
// proxy-mode endpoint.
func handleGuacConnection(srv *RttyServer, c *gin.Context) {
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
	if ep.AuthMode != vncendpoint.AuthProxy {
		c.Status(http.StatusBadRequest) // wrong endpoint for this route
		return
	}
	protocol, ok := guacProtocolFor(ep.Kind)
	if !ok {
		c.Status(http.StatusBadRequest) // xpra has no guacd proxy
		return
	}
	if c.GetHeader("Upgrade") != "websocket" {
		c.Status(http.StatusBadRequest)
		return
	}

	secret := srv.cfg.VncSecret
	if secret == "" {
		secret = srv.cfg.Token
	}
	password, _ := vnccrypt.Decrypt(secret, ep.PasswordEnc)

	// Screen size from the query (guacamole-common-js sends it), with defaults.
	w := atoiDefault(c.Query("width"), 1280)
	h := atoiDefault(c.Query("height"), 800)
	dpi := atoiDefault(c.Query("dpi"), 96)

	guac, err := net.DialTimeout("tcp", srv.guacdAddr(), guacDialTimeout)
	if err != nil {
		log.Warn().Err(err).Msgf("guac: dial guacd %s failed", srv.guacdAddr())
		c.Status(http.StatusBadGateway)
		return
	}
	defer guac.Close()

	br, ready, err := guacHandshake(guac, protocol, guacParams(ep, password), w, h, dpi)
	if err != nil {
		log.Warn().Err(err).Msgf("guac: handshake failed for endpoint %d", ep.ID)
		c.Status(http.StatusBadGateway)
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Audit log (reuse the VNC session type; detail carries proxy+protocol).
	var logID int64
	if cont.DeviceLogSvc != nil {
		actorID, actorName := principalFromCtx(c)
		logID = cont.DeviceLogSvc.StartRemoteVncSession(
			c.Request.Context(), ep.ViaDevice, ep.Name, actorID, actorName, c.ClientIP(), ep.Addr)
		if cont.NotificationSvc != nil {
			cont.NotificationSvc.NotifyRemoteAccess(strings.ToUpper(string(ep.Kind))+" (proxy)", ep.ViaDevice, ep.Name, actorName, c.ClientIP())
		}
	}
	defer func() {
		if logID > 0 && cont.DeviceLogSvc != nil {
			cont.DeviceLogSvc.EndSession(context.Background(), logID)
		}
	}()

	guacRelay(guac, br, ready, conn)
}

// guacRelay forwards the `ready` instruction and the subsequent guacd stream to
// the browser as text frames, and browser text frames back to guacd.
func guacRelay(guac net.Conn, br *bufio.Reader, ready []string, ws *websocket.Conn) {
	w := &wsWriter{conn: ws}
	_ = w.conn.WriteMessage(websocket.TextMessage, guacEncode(ready...))

	done := make(chan struct{}, 2)

	// guacd -> browser
	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 16*1024)
		for {
			n, err := br.Read(buf)
			if n > 0 {
				if werr := w.writeText(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// browser -> guacd
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			typ, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if typ != websocket.TextMessage {
				continue
			}
			if _, err := guac.Write(data); err != nil {
				return
			}
		}
	}()

	<-done
	guac.Close()
	ws.Close()
	<-done
}

func (w *wsWriter) writeText(p []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(websocket.TextMessage, p)
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return def
}
