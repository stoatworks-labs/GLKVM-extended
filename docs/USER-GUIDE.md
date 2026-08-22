# GLKVM Extended user guide

This is **GL.iNet's self-hosted KVM cloud, extended so the same UI reaches ordinary machines too**
— VNC, RDP and xpra endpoints alongside the KVM devices it already managed.

The upstream project manages GLKVM hardware: each device dials home over its own tunnel, and the
web UI gives you SSH and a remote desktop through it, with NAT traversal handled for you. This fork
adds **endpoints**: a name and a `host:port` that is *not* tied to a GLKVM device at all.

The [README](../README.md) is upstream's, and covers self-hosting the platform. **This guide covers
what the fork adds.**

> **Before you rely on this:** the endpoint transport, the CIDR allowlist and the RFB auth-proxy
> are covered by tests, including one that pins the VNC authentication cipher against OpenSSL's
> own engine — an independent implementation — and one that drives the full handshake against a
> fake server. A password-protected VNC endpoint has been **verified rendering in a browser with no
> prompt**.
>
> **The guacd proxy path has not been verified rendering in a browser.** The codec and the full
> handshake are tested against a fake guacd and the handler fails gracefully when none is present,
> but no real guacd has drawn a frame here.
>
> This work was created with AI assistance, directed and reviewed by a human author.

---

## What an endpoint is

A name, a `host:port`, and a **protocol kind** — `vnc`, `rdp` or `xpra`.

The bridge underneath is **byte-transparent**, so all three kinds share one transport, one
allowlist and one tunnel. They differ only in **which client the browser loads**, and in whether
the VNC authentication proxy runs.

That is why adding a kind is cheap and why the security properties below apply to all of them
equally.

---

## Two ways an endpoint is reached

**Tunnelled — set a "via device".** The connection goes through that GLKVM device's existing link,
reusing the same raw-TCP proxy the platform already had, **so NAT traversal is preserved**. This is
how you reach a machine sitting on a remote site's LAN behind somebody else's router.

A tunnelled endpoint requires an **IPv4 literal**, because the tunnel frame carries a fixed 4-byte
address. A hostname cannot be expressed there.

**Direct — leave "via device" empty.** The cloud dials the VNC/RDP/xpra server itself. That is
right when the target is reachable from the server, and it is the case the allowlist exists for.

---

## The allowlist, and why direct dials need one

A direct-dial endpoint is a request that makes **your server** open a TCP connection to an address
**a user chose**. Left unbounded that is a server-side request forgery: someone with permission to
create an endpoint can reach anything your server can reach.

So there is a **CIDR allowlist** — configurable in the YAML or by environment variable — naming the
networks a direct endpoint may reach.

- **Empty means allow all**, which is the upstream behaviour and fine on an isolated deployment.
- It is enforced **twice**: at create and update time, so a bad endpoint cannot be saved, and again
  **authoritatively before dialling**, so an endpoint that was legal when saved cannot become a
  hole later.
- **A hostname must resolve entirely within the allowlist.** If any resolved address falls outside,
  the dial is refused — it fails closed rather than picking the address that happens to be
  allowed.
- **Tunnelled endpoints are exempt**, because they can only reach the tunnel device's own LAN.

Set it before you let anyone else create endpoints.

---

## Two ways to handle credentials

**`client`** — the browser authenticates, the way it always has.

**`proxy`** — **the browser never sees the password.**

For VNC, the server completes the authentication toward the VNC server using a stored password and
presents "no authentication" toward the browser. RFB 3.3, 3.7 and 3.8 are all handled, preferring
VNC authentication and falling back.

For RDP, the server connects through a **guacd** daemon, performs the handshake with server-held
credentials, and relays the instruction stream to the browser.

**`proxy` is rejected for xpra**, which has no guacd backend.

### How the password is held

**Encrypted at rest with AES-256-GCM**, under a key from the dedicated secret setting.

> **Storing a password with no secret configured is refused**, rather than silently kept in the
> clear. If you want stored credentials, set the secret first.

The plaintext is **never returned by the API** — a listing exposes only a `hasPassword` flag, and
the UI shows a lock badge. Updating an endpoint has **keep / clear / set** semantics, so editing a
name does not require re-typing the password, and there is an explicit "remove stored password"
checkbox rather than an empty field meaning two different things.

---

## Permissions

Endpoint **reads are available to all users; writes are admin-gated**, behind their own permissions
rather than borrowing the device ones. A remote session on an endpoint is written into the device
event log like any other session, so the audit trail is one list.

---

## If something is wrong

| Symptom | Cause |
| --- | --- |
| **Creating a direct endpoint is refused** | Its address is outside the CIDR allowlist. That check happens at save time as well as at dial time. |
| **A hostname endpoint is refused** | It resolves to at least one address outside the allowlist, and the check fails closed. |
| **A tunnelled endpoint will not accept a hostname** | It cannot — the tunnel frame carries a fixed 4-byte address. Use an IPv4 literal. |
| **Storing a password is refused** | No encryption secret is configured. Set one; a plaintext fallback is deliberately not offered. |
| **A proxy-mode RDP endpoint returns 502** | No guacd is reachable. It is a co-located sidecar by default. |
| **`proxy` will not save on an xpra endpoint** | Deliberate — there is no guacd backend for xpra. |
| **The browser prompts for a VNC password on a proxy endpoint** | The auth-proxy did not run. Check the endpoint's kind is `vnc` and that a password is stored. |
