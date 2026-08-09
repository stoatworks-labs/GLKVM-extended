package handler

import (
	"net"
	"strconv"
	"strings"

	"rttys/internal/domain/vncendpoint"
	"rttys/internal/http/dto"
	"rttys/internal/http/middleware"
	"rttys/internal/store/sqlite"

	"github.com/gin-gonic/gin"
)

type VncEndpointHandler struct {
	repo *sqlite.VncEndpointRepo
}

func NewVncEndpointHandler(repo *sqlite.VncEndpointRepo) *VncEndpointHandler {
	return &VncEndpointHandler{repo: repo}
}

type vncEndpointReq struct {
	Name        string `json:"name"`
	Addr        string `json:"addr"`
	ViaDevice   string `json:"viaDevice"`
	Description string `json:"description"`
}

// validateVncAddr checks host:port shape. Tunnelled endpoints must be IPv4
// literals because the rtty http-proxy frame carries a fixed 4-byte address;
// direct endpoints may use hostnames (resolved by the cloud at dial time).
func validateVncAddr(addr, viaDevice string) string {
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
	}
	return ""
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
	if msg := validateVncAddr(req.Addr, req.ViaDevice); msg != "" {
		dto.Write(c, dto.Err(traceID, dto.CodeInvalidArgument, msg, map[string]any{"field": "addr"}))
		return
	}

	id, err := h.repo.Create(c.Request.Context(), &vncendpoint.Endpoint{
		Name:        req.Name,
		Addr:        strings.TrimSpace(req.Addr),
		ViaDevice:   strings.TrimSpace(req.ViaDevice),
		Description: req.Description,
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
	if msg := validateVncAddr(req.Addr, req.ViaDevice); msg != "" {
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

	err = h.repo.Update(c.Request.Context(), &vncendpoint.Endpoint{
		ID:          id,
		Name:        req.Name,
		Addr:        strings.TrimSpace(req.Addr),
		ViaDevice:   strings.TrimSpace(req.ViaDevice),
		Description: req.Description,
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
