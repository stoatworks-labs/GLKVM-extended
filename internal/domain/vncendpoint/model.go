package vncendpoint

// Endpoint is a standalone VNC server users can connect to through the
// cloud. When ViaDevice is empty the cloud dials Addr directly; otherwise
// the connection is tunnelled through the named device's rtty link, so the
// endpoint only needs to be reachable from that device's LAN.
type Endpoint struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Addr        string `json:"addr"`      // host:port of the VNC server
	ViaDevice   string `json:"viaDevice"` // device id to tunnel through, "" = direct
	Description string `json:"description"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}
