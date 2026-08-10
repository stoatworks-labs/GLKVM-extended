package handler

import (
	"crypto/rsa"
	"net"
	"strconv"
	"strings"

	"rttys/internal/domain/vncendpoint"
	"rttys/internal/http/dto"
	"rttys/internal/http/middleware"
	"rttys/internal/pkg/rdptoken"
	"rttys/internal/pkg/vnccrypt"
	"rttys/internal/store/sqlite"
	"rttys/utils"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

type VncEndpointHandler struct {
	repo      *sqlite.VncEndpointRepo
	secret    string
	allowlist []*net.IPNet
	// rdpGatewayURL is the Devolutions Gateway base ws(s):// URL for
	// client-side RDP; rdpKey signs the association tokens. Both empty/nil
	// when client-side RDP is not configured.
	rdpGatewayURL string
	rdpKey        *rsa.PrivateKey
}

func NewVncEndpointHandler(repo *sqlite.VncEndpointRepo, secret string, allowlist []string, rdpGatewayURL, rdpKeyPath string) *VncEndpointHandler {
	h := &VncEndpointHandler{
		repo:          repo,
		secret:        secret,
		allowlist:     utils.ParseCIDRs(allowlist),
		rdpGatewayURL: strings.TrimRight(strings.TrimSpace(rdpGatewayURL), "/"),
	}
	if p := strings.TrimSpace(rdpKeyPath); p != "" {
		key, err := rdptoken.LoadKey(p)
		if err != nil {
			log.Warn().Err(err).Msgf("rdp: cannot load provisioner key %s; client-side RDP disabled", p)
		} else {
			h.rdpKey = key
		}
	}
	return h
}

type vncEndpointReq struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	AuthMode    string `json:"authMode"`
	Addr        string `json:"addr"`
	Username    string `json:"username"`
	Domain      string `json:"domain"`
	ViaDevice   string `json:"viaDevice"`
	Description string `json:"description"`
	// Password: nil = leave unchanged (update) / none (create); "" = clear;
	// non-empty = set. Never echoed back.
	Password *string `json:"password"`
}

// resolveKind defaults an empty kind to VNC and validates it.
func resolveKind(k string) (vncendpoint.Kind, bool) {
	if k == "" {
		return vncendpoint.KindVNC, true
	}
	kind := vncendpoint.Kind(k)
	return kind, vncendpoint.ValidKind(kind)
}

// resolveAuthMode defaults an empty mode to client and validates it against the
// kind (proxy is only available for VNC/RDP — xpra has no guacd backend).
func resolveAuthMode(m string, kind vncendpoint.Kind) (vncendpoint.AuthMode, string) {
	mode := vncendpoint.AuthMode(m)
	if m == "" {
		mode = vncendpoint.AuthClient
	}
	if !vncendpoint.ValidAuthMode(mode) {
		return "", "invalid authMode"
	}
	if mode == vncendpoint.AuthProxy && kind == vncendpoint.KindXpra {
		return "", "proxy mode is not available for X (xpra)"
	}
	return mode, ""
}

// validateVncAddr checks host:port shape. Tunnelled endpoints must be IPv4
// literals because the rtty http-proxy frame carries a fixed 4-byte address;
// direct endpoints may use hostnames (resolved by the cloud at dial time).
// For direct endpoints the host is also checked against the CIDR allowlist.
func (h *VncEndpointHandler) validateVncAddr(addr, viaDevice string) string {
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil || host == "" {
		return "addr must be host:port"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return "invalid port"
	}
	if viaDevice != "" {
		ip := net.ParseIP(host)
		if ip == nil || ip.To4() == nil {
			return "tunnelled endpoints require an IPv4 address"
		}
		return "" // tunnelled endpoints are exempt from the allowlist
	}
	if !utils.HostAllowed(h.allowlist, host) {
		return "address is outside the permitted range (vnc-direct-allowlist)"
	}
	return ""
}

// encodePassword resolves the create/update password semantics into the
// ciphertext to persist: keep (existingEnc), clear (""), or a fresh
// encryption. Returns an error string for the client on failure.
func (h *VncEndpointHandler) encodePassword(p *string, existingEnc string) (string, string) {
	if p == nil {
		return existingEnc, "" // unchanged
	}
	if *p == "" {
		return "", "" // cleared
	}
	enc, err := vnccrypt.Encrypt(h.secret, *p)
	if err != nil {
		return "", "server has no VNC secret configured; cannot store a password"
	}
	return enc, ""
}

// GET /api/vnc-endpoints
func (h *VncEndpointHandler) List(c *gin.Context) {
	traceID := middleware.GetTraceID(c)

	items, err := h.repo.List(c.Request.Context())
	if err != nil {
		dto.Write(c, dto.Err(traceID, dto.CodeInternalError, "Internal error", nil))
		return
	}
	if items == nil {
		items = []*vncendpoint.Endpoint{}
	}
	dto.Write(c, dto.Ok(traceID, gin.H{"items": items}))
}

// POST /api/vnc-endpoints
func (h *VncEndpointHandler) Create(c *gin.Context) {
	traceID := middleware.GetTraceID(c)

	var req vncEndpointReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		dto.Write(c, dto.Err(traceID, dto.CodeInvalidArgument, "Invalid argument", map[string]any{"field": "name"}))
		return
	}
	kind, ok := resolveKind(req.Kind)
	if !ok {
		dto.Write(c, dto.Err(traceID, dto.CodeInvalidArgument, "Invalid kind", map[string]any{"field": "kind"}))
		return
	}
	authMode, amErr := resolveAuthMode(req.AuthMode, kind)
	if amErr != "" {
		dto.Write(c, dto.Err(traceID, dto.CodeInvalidArgument, amErr, map[string]any{"field": "authMode"}))
		return
	}
	if msg := h.validateVncAddr(req.Addr, req.ViaDevice); msg != "" {
		dto.Write(c, dto.Err(traceID, dto.CodeInvalidArgument, msg, map[string]any{"field": "addr"}))
		return
	}
	passwordEnc, perr := h.encodePassword(req.Password, "")
	if perr != "" {
		dto.Write(c, dto.Err(traceID, dto.CodeInvalidArgument, perr, map[string]any{"field": "password"}))
		return
	}

	id, err := h.repo.Create(c.Request.Context(), &vncendpoint.Endpoint{
		Name:        req.Name,
		Kind:        kind,
		AuthMode:    authMode,
		Addr:        strings.TrimSpace(req.Addr),
		Username:    strings.TrimSpace(req.Username),
		Domain:      strings.TrimSpace(req.Domain),
		ViaDevice:   strings.TrimSpace(req.ViaDevice),
		Description: req.Description,
		PasswordEnc: passwordEnc,
	})
	if err != nil {
		dto.Write(c, dto.Err(traceID, dto.CodeInternalError, "Internal error", nil))
		return
	}
	dto.Write(c, dto.Ok(traceID, gin.H{"id": id}))
}

// PUT /api/vnc-endpoints/:id
func (h *VncEndpointHandler) Update(c *gin.Context) {
	traceID := middleware.GetTraceID(c)

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		dto.Write(c, dto.Err(traceID, dto.CodeInvalidArgument, "Invalid argument", map[string]any{"field": "id"}))
		return
	}

	var req vncEndpointReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		dto.Write(c, dto.Err(traceID, dto.CodeInvalidArgument, "Invalid argument", map[string]any{"field": "name"}))
		return
	}
	kind, ok := resolveKind(req.Kind)
	if !ok {
		dto.Write(c, dto.Err(traceID, dto.CodeInvalidArgument, "Invalid kind", map[string]any{"field": "kind"}))
		return
	}
	authMode, amErr := resolveAuthMode(req.AuthMode, kind)
	if amErr != "" {
		dto.Write(c, dto.Err(traceID, dto.CodeInvalidArgument, amErr, map[string]any{"field": "authMode"}))
		return
	}
	if msg := h.validateVncAddr(req.Addr, req.ViaDevice); msg != "" {
		dto.Write(c, dto.Err(traceID, dto.CodeInvalidArgument, msg, map[string]any{"field": "addr"}))
		return
	}

	existing, err := h.repo.FindByID(c.Request.Context(), id)
	if err != nil {
		dto.Write(c, dto.Err(traceID, dto.CodeInternalError, "Internal error", nil))
		return
	}
	if existing == nil {
		dto.Write(c, dto.Err(traceID, dto.CodeNotFound, "Not found", nil))
		return
	}
	passwordEnc, perr := h.encodePassword(req.Password, existing.PasswordEnc)
	if perr != "" {
		dto.Write(c, dto.Err(traceID, dto.CodeInvalidArgument, perr, map[string]any{"field": "password"}))
		return
	}

	err = h.repo.Update(c.Request.Context(), &vncendpoint.Endpoint{
		ID:          id,
		Name:        req.Name,
		Kind:        kind,
		AuthMode:    authMode,
		Addr:        strings.TrimSpace(req.Addr),
		Username:    strings.TrimSpace(req.Username),
		Domain:      strings.TrimSpace(req.Domain),
		ViaDevice:   strings.TrimSpace(req.ViaDevice),
		Description: req.Description,
		PasswordEnc: passwordEnc,
	})
	if err != nil {
		dto.Write(c, dto.Err(traceID, dto.CodeInternalError, "Internal error", nil))
		return
	}
	dto.Write(c, dto.Ok(traceID, gin.H{"id": id}))
}

// DELETE /api/vnc-endpoints/:id
func (h *VncEndpointHandler) Delete(c *gin.Context) {
	traceID := middleware.GetTraceID(c)

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		dto.Write(c, dto.Err(traceID, dto.CodeInvalidArgument, "Invalid argument", map[string]any{"field": "id"}))
		return
	}
	if err := h.repo.Delete(c.Request.Context(), id); err != nil {
		dto.Write(c, dto.Err(traceID, dto.CodeInternalError, "Internal error", nil))
		return
	}
	dto.Write(c, dto.Ok(traceID, gin.H{"id": id}))
}
