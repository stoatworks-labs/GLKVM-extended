package handler

import (
	"strconv"
	"strings"
	"time"

	"rttys/internal/domain/vncendpoint"
	"rttys/internal/http/dto"
	"rttys/internal/http/middleware"
	"rttys/internal/pkg/rdptoken"

	"github.com/gin-gonic/gin"
)

// rdpTokenTTL bounds an association token's validity. It only needs to cover
// the RDCleanPath handshake at connect time; the session itself continues on
// the already-authorised websocket after that.
const rdpTokenTTL = 2 * time.Minute

// POST /api/vnc-endpoints/:id/rdp-session
//
// Mints a short-lived Devolutions Gateway association token so the browser
// (IronRDP-web) can open a client-side RDP session through the gateway sidecar.
// RDP credentials are entered in the browser and never sent here — this only
// authorises the transport to the endpoint's address.
func (h *VncEndpointHandler) RdpSession(c *gin.Context) {
	traceID := middleware.GetTraceID(c)

	if h.rdpKey == nil || h.rdpGatewayURL == "" {
		dto.Write(c, dto.Err(traceID, dto.CodeConflict,
			"Client-side RDP is not configured on this server", nil))
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		dto.Write(c, dto.Err(traceID, dto.CodeInvalidArgument, "Invalid id", nil))
		return
	}

	ep, err := h.repo.FindByID(c.Request.Context(), id)
	if err != nil {
		dto.Write(c, dto.Err(traceID, dto.CodeInternalError, "Internal error", nil))
		return
	}
	if ep == nil {
		dto.Write(c, dto.Err(traceID, dto.CodeNotFound, "Endpoint not found", nil))
		return
	}
	if ep.Kind != vncendpoint.KindRDP || ep.AuthMode != vncendpoint.AuthClient {
		dto.Write(c, dto.Err(traceID, dto.CodeInvalidArgument,
			"Endpoint is not a client-mode RDP endpoint", nil))
		return
	}
	// The gateway dials the destination itself, so it can only reach directly
	// routable hosts — not endpoints tunnelled through a device's rtty link.
	if ep.ViaDevice != "" {
		dto.Write(c, dto.Err(traceID, dto.CodeConflict,
			"Client-side RDP is not available for tunnelled endpoints; use proxy mode", nil))
		return
	}

	token, err := rdptoken.Sign(h.rdpKey, ep.Addr, rdpTokenTTL)
	if err != nil {
		dto.Write(c, dto.Err(traceID, dto.CodeInternalError, "Cannot mint RDP token", nil))
		return
	}

	dto.Write(c, dto.Ok(traceID, gin.H{
		"proxyAddress": h.rdpGatewayURL + "/jet/rdp",
		"authToken":    token,
		"destination":  ep.Addr,
		"username":     strings.TrimSpace(ep.Username),
		"domain":       strings.TrimSpace(ep.Domain),
	}))
}
