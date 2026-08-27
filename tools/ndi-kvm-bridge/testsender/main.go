// ndisender — a minimal NDI source that advertises KVM support and logs every
// metadata frame it receives. Used to verify that ndikvm's browser input is
// correctly emitted as <ndi_kvm u="..."/> metadata upstream, without injecting
// input into any real machine.
package main

/*
#cgo CFLAGS: -I"/Library/NDI SDK for Apple/include"
#cgo LDFLAGS: -L/usr/local/lib -lndi -Wl,-rpath,/usr/local/lib
#include <Processing.NDI.Lib.h>
#include <stdlib.h>
#include <string.h>

static int ndi_send_capture_meta(NDIlib_send_instance_t s, unsigned int timeout_ms, NDIlib_metadata_frame_t* mf) {
	return (int)NDIlib_send_capture(s, mf, timeout_ms);
}
*/
import "C"

import (
	"flag"
	"log"
	"time"
	"unsafe"
)

func main() {
	var name, caps string
	flag.StringVar(&name, "name", "KVMTEST", "NDI source name")
	flag.StringVar(&caps, "caps", `<ndi_capabilities ndi_version="4" web_control="true" kvm="true"/>`, "capabilities metadata to advertise")
	flag.Parse()

	if !bool(C.NDIlib_initialize()) {
		log.Fatal("NDIlib_initialize failed")
	}

	cname := C.CString(name)
	var sc C.NDIlib_send_create_t
	sc.p_ndi_name = cname
	sc.clock_video = true
	send := C.NDIlib_send_create(&sc)
	C.free(unsafe.Pointer(cname))
	if send == nil {
		log.Fatal("NDIlib_send_create failed")
	}
	log.Printf("NDI sender %q up; advertising caps: %s", name, caps)

	// Advertise KVM capability to every receiver that connects.
	ccaps := C.CString(caps)
	var cm C.NDIlib_metadata_frame_t
	cm.p_data = ccaps
	cm.length = C.int(len(caps) + 1)
	C.NDIlib_send_add_connection_metadata(send, &cm)
	C.free(unsafe.Pointer(ccaps))

	// Send a solid-color BGRA frame at ~15fps so we are a valid live source.
	const w, h = 320, 180
	buf := C.malloc(C.size_t(w * h * 4))
	defer C.free(buf)
	px := unsafe.Slice((*byte)(buf), w*h*4)
	for i := 0; i < len(px); i += 4 {
		px[i+0], px[i+1], px[i+2], px[i+3] = 40, 90, 160, 255 // B,G,R,X
	}
	var vf C.NDIlib_video_frame_v2_t
	vf.xres, vf.yres = w, h
	vf.FourCC = C.NDIlib_FourCC_video_type_BGRX
	vf.p_data = (*C.uint8_t)(buf)
	vf.frame_rate_N, vf.frame_rate_D = 30000, 1001
	go func() {
		t := time.NewTicker(66 * time.Millisecond)
		defer t.Stop()
		for range t.C {
			C.NDIlib_send_send_video_v2(send, &vf)
		}
	}()

	// Log every metadata frame a receiver sends us (this is where <ndi_kvm> lands).
	log.Println("waiting for KVM metadata from receivers...")
	var mf C.NDIlib_metadata_frame_t
	for {
		t := C.ndi_send_capture_meta(send, 1000, &mf)
		if t == C.NDIlib_frame_type_metadata {
			xml := C.GoString(mf.p_data)
			log.Printf("META  %s", xml)
			C.NDIlib_send_free_metadata(send, &mf)
		}
	}
}
