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
	// PasswordEnc is the AES-GCM ciphertext of the VNC password (base64),
	// or "" when no credential is stored. Never serialised to clients.
	PasswordEnc string `json:"-"`
	// HasPassword is a derived, safe-to-expose flag for the UI.
	HasPassword bool  `json:"hasPassword"`
	CreatedAt   int64 `json:"createdAt"`
	UpdatedAt   int64 `json:"updatedAt"`
}
