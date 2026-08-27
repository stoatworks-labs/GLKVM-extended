// Package rdptoken mints Devolutions Gateway "association" tokens for
// client-side RDP. The browser (IronRDP-web) presents one of these to the
// gateway sidecar over RDCleanPath; the gateway validates the signature with
// the provisioner public key we configured it with, then forwards raw RDP to
// the destination. RDP credentials are entered in the browser and travel
// inside the RDP/TLS/CredSSP stream — the cloud never sees them.
//
// The token is a short-lived RS256 JWS with the claim set the gateway expects
// for a forwarding session (jet_cm=fwd, jet_ap=rdp, dst_hst=<target>). See the
// gateway's AssociationClaims.
package rdptoken

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
)

// LoadKey reads an RSA private key (PKCS#8 or PKCS#1, PEM) from path. The
// gateway must be configured with the matching public key as its provisioner.
func LoadKey(path string) (*rsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in %s", path)
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("provisioner key is not RSA")
	}
	return key, nil
}

// associationClaims mirrors the gateway's forwarding-session token. iat/scope
// are intentionally absent (the gateway's struct omits them).
type associationClaims struct {
	Exp    int64  `json:"exp"`
	Nbf    int64  `json:"nbf"`
	Jti    string `json:"jti"`
	JetCm  string `json:"jet_cm"`
	JetAp  string `json:"jet_ap"`
	JetAid string `json:"jet_aid"`
	JetRec string `json:"jet_rec"`
	DstHst string `json:"dst_hst"`
}

// Sign builds a signed association token authorising a single RDP forwarding
// session to destination (a "host:port"). ttl bounds its validity.
func Sign(key *rsa.PrivateKey, destination string, ttl time.Duration) (string, error) {
	if key == nil {
		return "", fmt.Errorf("no provisioner key configured")
	}
	now := time.Now()
	claims := associationClaims{
		Exp: now.Add(ttl).Unix(),
		// Small negative skew so a slightly-behind gateway clock still accepts it.
		Nbf:    now.Add(-30 * time.Second).Unix(),
		Jti:    uuid.NewString(),
		JetCm:  "fwd",
		JetAp:  "rdp",
		JetAid: uuid.NewString(),
		JetRec: "none",
		// tcp:// scheme is the gateway's canonical destination form.
		DstHst: "tcp://" + destination,
	}
	return signJWT(key, claims)
}

func signJWT(key *rsa.PrivateKey, claims any) (string, error) {
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	hb, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	cb, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := b64(hb) + "." + b64(cb)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + b64(sig), nil
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
