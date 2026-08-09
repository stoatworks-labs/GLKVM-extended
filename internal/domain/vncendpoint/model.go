package vncendpoint

// Kind identifies the remote-desktop protocol an endpoint speaks. The cloud
// bridge is byte-transparent, so all kinds share the same transport; they
// differ only in the browser client used and whether the server-side
// RFB auth-proxy applies (VNC only).
type Kind string

const (
	KindVNC  Kind = "vnc"
	KindRDP  Kind = "rdp"
	KindXpra Kind = "xpra"
)

// ValidKind reports whether k is a supported endpoint kind.
func ValidKind(k Kind) bool {
	switch k {
	case KindVNC, KindRDP, KindXpra:
		return true
	default:
		return false
	}
}

// Endpoint is a remote-desktop server users can connect to through the cloud.
// When ViaDevice is empty the cloud dials Addr directly; otherwise the
// connection is tunnelled through the named device's rtty link, so the
// endpoint only needs to be reachable from that device's LAN.
type Endpoint struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Kind        Kind   `json:"kind"`      // vnc | rdp | xpra
	Addr        string `json:"addr"`      // host:port of the server
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
