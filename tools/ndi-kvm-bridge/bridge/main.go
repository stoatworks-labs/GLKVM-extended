// ndikvm — a proof-of-concept NDI KVM web bridge.
//
// It receives an NDI source's video, streams it to the browser as MJPEG, and
// turns browser mouse/keyboard events into NDI KVM control messages sent back
// upstream to the sender (NDI Screen Capture / Scan Converter with KVM).
//
// The KVM functions live only in the NDI *Advanced* SDK. We declare their
// signatures here (from the public docs) and link the advanced runtime dylib;
// for distribution this would dlopen at runtime instead of link-time.
package main

/*
#cgo CFLAGS: -I"/Library/NDI SDK for Apple/include"
#cgo LDFLAGS: -L${SRCDIR} -lndi_advanced -Wl,-rpath,${SRCDIR}
#include <Processing.NDI.Lib.h>
#include <stdbool.h>
#include <stdlib.h>
#include <string.h>

// --- Advanced SDK KVM API (not in the standard headers; from public docs) ---
bool NDIlib_recv_kvm_is_supported(NDIlib_recv_instance_t p);
bool NDIlib_recv_kvm_send_mouse_position(NDIlib_recv_instance_t p, const float posn[2]);
bool NDIlib_recv_kvm_send_left_mouse_click(NDIlib_recv_instance_t p);
bool NDIlib_recv_kvm_send_left_mouse_release(NDIlib_recv_instance_t p);
bool NDIlib_recv_kvm_send_right_mouse_click(NDIlib_recv_instance_t p);
bool NDIlib_recv_kvm_send_right_mouse_release(NDIlib_recv_instance_t p);
bool NDIlib_recv_kvm_send_middle_mouse_click(NDIlib_recv_instance_t p);
bool NDIlib_recv_kvm_send_middle_mouse_release(NDIlib_recv_instance_t p);
bool NDIlib_recv_kvm_send_vertical_mouse_wheel(NDIlib_recv_instance_t p, const float no_units);
bool NDIlib_recv_kvm_send_horizontal_mouse_wheel(NDIlib_recv_instance_t p, const float no_units);
bool NDIlib_recv_kvm_send_keyboard_press(NDIlib_recv_instance_t p, const int key_sym_value);
bool NDIlib_recv_kvm_send_keyboard_release(NDIlib_recv_instance_t p, const int key_sym_value);

// Small wrappers so Go passes plain scalars.
static bool kvm_mouse_pos(NDIlib_recv_instance_t r, float x, float y) {
	float p[2] = {x, y};
	return NDIlib_recv_kvm_send_mouse_position(r, p);
}
static int ndi_capture_video(NDIlib_recv_instance_t r, unsigned int timeout_ms, NDIlib_video_frame_v2_t* vf) {
	NDIlib_frame_type_e t = NDIlib_recv_capture_v2(r, vf, NULL, NULL, timeout_ms);
	return (int)t;
}
*/
import "C"

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// recv is the single NDI receiver instance. All NDI recv calls (capture + KVM)
// are funnelled onto one goroutine (ndiLoop) because a recv instance is not
// safe for concurrent use.
var recv C.NDIlib_recv_instance_t

// latest holds the most recent JPEG frame for the MJPEG stream.
var latest struct {
	sync.RWMutex
	jpeg []byte
	w, h int
}

// inputCh carries browser events to ndiLoop for dispatch as KVM messages.
var inputCh = make(chan inputEvent, 256)

type inputEvent struct {
	T      string  `json:"t"`      // move|down|up|wheel|kdown|kup
	X      float32 `json:"x"`      // normalized 0..1
	Y      float32 `json:"y"`      // normalized 0..1
	Button int     `json:"button"` // 0 left,1 middle,2 right
	DX     float32 `json:"dx"`     // wheel
	DY     float32 `json:"dy"`     // wheel
	Keysym int     `json:"keysym"` // X11 keysym
}

func main() {
	var addr, want string
	flag.StringVar(&addr, "addr", "127.0.0.1:8790", "http listen address")
	flag.StringVar(&want, "source", "", "substring of the NDI source name to connect to (default: first found)")
	flag.Parse()

	if !bool(C.NDIlib_initialize()) {
		log.Fatal("NDIlib_initialize failed (CPU not supported?)")
	}
	log.Println("NDI initialized")

	name := findSource(want)
	log.Printf("connecting to NDI source: %s", name)

	// Create a receiver in BGRX/BGRA so frames are 4-byte RGB we can JPEG-encode.
	// Connect by name (a Go-owned C string) — the finder's own source memory is
	// gone by now, so we must not reference it.
	cname := C.CString(name)
	var src C.NDIlib_source_t
	src.p_ndi_name = cname
	var cr C.NDIlib_recv_create_v3_t
	cr.source_to_connect_to = src
	cr.color_format = C.NDIlib_recv_color_format_BGRX_BGRA
	cr.bandwidth = C.NDIlib_recv_bandwidth_highest
	cr.allow_video_fields = false
	rname := C.CString("ndikvm-bridge")
	cr.p_ndi_recv_name = rname
	recv = C.NDIlib_recv_create_v3(&cr)
	C.free(unsafe.Pointer(cname))
	C.free(unsafe.Pointer(rname))
	if recv == nil {
		log.Fatal("NDIlib_recv_create_v3 failed")
	}

	go ndiLoop()

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/stream", handleStream)
	http.HandleFunc("/input", handleInput)
	log.Printf("ndikvm bridge on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

// findSource discovers NDI sources and returns the NAME of the one whose name
// contains `want` (or the first one if want is empty). Blocks until a source
// appears. Returning the name (not the struct) avoids referencing finder-owned
// memory after the finder is destroyed.
func findSource(want string) string {
	finder := C.NDIlib_find_create_v2(nil)
	if finder == nil {
		log.Fatal("NDIlib_find_create_v2 failed")
	}
	defer C.NDIlib_find_destroy(finder)
	for attempt := 0; ; attempt++ {
		C.NDIlib_find_wait_for_sources(finder, 2000)
		var n C.uint32_t
		arr := C.NDIlib_find_get_current_sources(finder, &n)
		sources := unsafe.Slice(arr, int(n))
		names := make([]string, 0, int(n))
		for _, s := range sources {
			names = append(names, C.GoString(s.p_ndi_name))
		}
		if len(names) > 0 {
			log.Printf("found %d NDI source(s): %s", len(names), strings.Join(names, " | "))
		}
		for _, nm := range names {
			if want == "" || strings.Contains(strings.ToLower(nm), strings.ToLower(want)) {
				return nm
			}
		}
		if attempt%5 == 0 {
			log.Printf("waiting for NDI source matching %q ...", want)
		}
	}
}

// ndiLoop owns the recv instance: it drains pending input as KVM messages and
// captures video frames, encoding the latest to JPEG for the MJPEG stream.
func ndiLoop() {
	var vf C.NDIlib_video_frame_v2_t
	kvmLogged := false
	for {
		// Drain all pending input events first (snappy control).
		for drained := false; !drained; {
			select {
			case ev := <-inputCh:
				dispatch(ev)
			default:
				drained = true
			}
		}

		// Capture one frame (short timeout keeps input latency low).
		t := C.ndi_capture_video(recv, 30, &vf)
		if t != C.NDIlib_frame_type_video {
			continue
		}
		if !kvmLogged {
			ok := bool(C.NDIlib_recv_kvm_is_supported(recv))
			log.Printf("KVM supported by source: %v", ok)
			kvmLogged = true
		}
		encodeLatest(&vf)
		C.NDIlib_recv_free_video_v2(recv, &vf)
	}
}

// dispatch turns one browser event into NDI KVM calls.
func dispatch(ev inputEvent) {
	switch ev.T {
	case "move":
		C.kvm_mouse_pos(recv, C.float(ev.X), C.float(ev.Y))
	case "down":
		C.kvm_mouse_pos(recv, C.float(ev.X), C.float(ev.Y))
		switch ev.Button {
		case 1:
			C.NDIlib_recv_kvm_send_middle_mouse_click(recv)
		case 2:
			C.NDIlib_recv_kvm_send_right_mouse_click(recv)
		default:
			C.NDIlib_recv_kvm_send_left_mouse_click(recv)
		}
	case "up":
		switch ev.Button {
		case 1:
			C.NDIlib_recv_kvm_send_middle_mouse_release(recv)
		case 2:
			C.NDIlib_recv_kvm_send_right_mouse_release(recv)
		default:
			C.NDIlib_recv_kvm_send_left_mouse_release(recv)
		}
	case "wheel":
		if ev.DY != 0 {
			C.NDIlib_recv_kvm_send_vertical_mouse_wheel(recv, C.float(ev.DY))
		}
		if ev.DX != 0 {
			C.NDIlib_recv_kvm_send_horizontal_mouse_wheel(recv, C.float(ev.DX))
		}
	case "kdown":
		C.NDIlib_recv_kvm_send_keyboard_press(recv, C.int(ev.Keysym))
	case "kup":
		C.NDIlib_recv_kvm_send_keyboard_release(recv, C.int(ev.Keysym))
	}
}

// encodeLatest converts a BGRX/BGRA NDI frame to JPEG and stores it.
func encodeLatest(vf *C.NDIlib_video_frame_v2_t) {
	w, h := int(vf.xres), int(vf.yres)
	// BGRX/BGRA NDI frames are tightly packed (4 bytes/pixel, no row padding).
	stride := w * 4
	if vf.p_data == nil || w <= 0 || h <= 0 {
		return
	}
	src := unsafe.Slice((*byte)(unsafe.Pointer(vf.p_data)), stride*h)
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		srow := src[y*stride : y*stride+w*4]
		drow := rgba.Pix[y*rgba.Stride : y*rgba.Stride+w*4]
		for x := 0; x < w; x++ {
			b := srow[x*4+0]
			g := srow[x*4+1]
			r := srow[x*4+2]
			drow[x*4+0] = r
			drow[x*4+1] = g
			drow[x*4+2] = b
			drow[x*4+3] = 255
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: 65}); err != nil {
		return
	}
	latest.Lock()
	latest.jpeg = buf.Bytes()
	latest.w, latest.h = w, h
	latest.Unlock()
}

func handleStream(w http.ResponseWriter, r *http.Request) {
	const boundary = "ndijpeg"
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+boundary)
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flusher", 500)
		return
	}
	tick := time.NewTicker(50 * time.Millisecond) // ~20fps
	defer tick.Stop()
	var lastLen int
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
		latest.RLock()
		frame := latest.jpeg
		latest.RUnlock()
		if len(frame) == 0 || len(frame) == lastLen {
			// still send occasionally to keep the stream alive
		}
		lastLen = len(frame)
		if len(frame) == 0 {
			continue
		}
		fmt.Fprintf(w, "--%s\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", boundary, len(frame))
		if _, err := w.Write(frame); err != nil {
			return
		}
		w.Write([]byte("\r\n"))
		flusher.Flush()
	}
}

func handleInput(w http.ResponseWriter, r *http.Request) {
	var ev inputEvent
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	select {
	case inputCh <- ev:
	default: // drop if backed up
	}
	w.WriteHeader(204)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	latest.RLock()
	sw, sh := latest.w, latest.h
	latest.RUnlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, indexHTML, sw, sh)
}
