# ndi-kvm-bridge (proof of concept)

A standalone **NDI KVM** web bridge: it receives an NDI source's video, streams
it to the browser as MJPEG, and turns browser mouse/keyboard/wheel events into
NDI KVM control messages sent back upstream to the sender (NDI Screen Capture /
Studio Monitor with KVM enabled).

Unlike the vnc/rdp/xpra endpoints, NDI KVM is **not** byte-transparent: the
browser can't speak NDI, so this bridge runs a real NDI receiver + decode +
re-encode pipeline and translates input into `NDIlib_recv_kvm_*` calls. It is
therefore a **companion** that must run on (or near) the target's LAN, not in
the cloud.

## Status

End-to-end verified as a POC:

- **Video** — NDI source → BGRA decode → JPEG → MJPEG → browser `<img>`, tested
  against a live macOS screen source (full res, correct colour).
- **Control** — browser input → `recv_kvm_send_*` → `<ndi_kvm u="…"/>` metadata
  on the wire, decoded byte-exact (mouse position as normalized floats, X11
  keysyms for keys, opcodes for click/release/wheel). Verified with `testsender`,
  a minimal NDI source that logs the metadata it receives.

## Requirements

- **NDI standard SDK** for the headers (`/Library/NDI SDK for Apple/include` on
  macOS) — provides all the core types.
- **NDI Advanced SDK runtime** (`libndi_advanced.dylib`) for the `recv_kvm_*`
  functions — the standard `libndi` does **not** export them. The KVM function
  signatures are declared in-source (from the public docs) because the Advanced
  SDK headers are a separate, licensed download.

The Advanced runtime is **not** committed (see `.gitignore`). Point the build at
your local copy, e.g. symlink it next to the source:

```
ln -s "/path/to/libndi_advanced.dylib" bridge/libndi_advanced.dylib
```

(The bridge's `#cgo LDFLAGS` uses `-L${SRCDIR} -lndi_advanced`.)

For distribution this must **dlopen the runtime at runtime**, never link/bake it
in — the NDI redistributable is licensed and can't be bundled.

## Run

```
# 1) a KVM-capable NDI source must be on the network (NDI 5+ Screen Capture).
# 2) the bridge:
cd bridge && go build -o bridge . && ./bridge -addr 127.0.0.1:8790 -source "SomeSource"
# open http://127.0.0.1:8790  — click the image to grab the keyboard.
```

### Verifying the control path without a real machine

`testsender` is a minimal NDI source that advertises KVM and logs every metadata
frame it receives — so you can confirm the bridge emits correct `<ndi_kvm>`
messages without injecting input into any real OS:

```
cd testsender && go build -o testsender . && ./testsender -name KVMTEST
# then run the bridge with -source KVMTEST and drive input; watch testsender's log.
```

## KVM wire format (decoded)

Messages are `<ndi_kvm u="BASE64"/>` metadata, receiver→sender. The base64
payload is little-endian:

| opcode | meaning | payload |
|--------|---------|---------|
| `03` | mouse position | float32 x, float32 y (0.0–1.0), `01` flag |
| `04`/`05`/`06` | left/middle/right click | — |
| `07`/`08`/`09` | left/middle/right release | — |
| `0A` | vertical mouse wheel | float32 units |
| `0C` | keyboard | int32 X11 keysym, int32 press(1)/release(0) |

`recv_kvm_send_*` emits this metadata regardless of `is_supported`; that flag
only reflects the sender's advertised `<ndi_capabilities>`.
