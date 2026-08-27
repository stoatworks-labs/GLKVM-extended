package server

import (
	"bufio"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGuacEncode(t *testing.T) {
	got := string(guacEncode("select", "rdp"))
	if got != "6.select,3.rdp;" {
		t.Fatalf("encode = %q, want 6.select,3.rdp;", got)
	}
	// UTF-8 length is in characters, not bytes.
	if got := string(guacEncode("é")); got != "1.é;" {
		t.Fatalf("utf8 encode = %q, want 1.é;", got)
	}
}

func TestGuacReadInstruction(t *testing.T) {
	br := bufio.NewReader(strings.NewReader("4.args,13.VERSION_1_3_0,8.hostname,4.port;5.ready,3.abc;"))
	got, err := guacReadInstruction(br)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"args", "VERSION_1_3_0", "hostname", "port"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed %v, want %v", got, want)
	}
	got2, _ := guacReadInstruction(br)
	if !reflect.DeepEqual(got2, []string{"ready", "abc"}) {
		t.Fatalf("second = %v", got2)
	}
}

// TestGuacHandshake drives guacHandshake against a fake guacd that follows the
// real protocol: read select, send args, read the client handshake, send ready.
func TestGuacHandshake(t *testing.T) {
	clientSide, guacSide := net.Pipe()

	type result struct {
		connect []string
		err     string
	}
	res := make(chan result, 1)

	// Fake guacd.
	go func() {
		defer guacSide.Close()
		br := bufio.NewReader(guacSide)
		// expect select,rdp
		sel, err := guacReadInstruction(br)
		if err != nil || len(sel) != 2 || sel[0] != "select" || sel[1] != "rdp" {
			res <- result{err: "bad select"}
			return
		}
		// send args: version + param names
		guacSide.Write(guacEncode("args", "VERSION_1_3_0", "hostname", "port", "username", "password", "domain"))
		// read size, audio, video, image, connect
		var connect []string
		for i := 0; i < 5; i++ {
			ins, err := guacReadInstruction(br)
			if err != nil {
				res <- result{err: "read handshake: " + err.Error()}
				return
			}
			if ins[0] == "connect" {
				connect = ins
			}
		}
		// send ready
		guacSide.Write(guacEncode("ready", "$conn-id"))
		res <- result{connect: connect}
	}()

	params := map[string]string{
		"hostname": "100.94.198.79",
		"port":     "3389",
		"username": "rdptest",
		"password": "RdpTest2026",
		"domain":   "",
	}
	br, ready, err := guacHandshake(clientSide, "rdp", params, 1280, 800, 96)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if len(ready) == 0 || ready[0] != "ready" || ready[1] != "$conn-id" {
		t.Fatalf("ready = %v", ready)
	}
	_ = br

	select {
	case r := <-res:
		if r.err != "" {
			t.Fatalf("fake guacd: %s", r.err)
		}
		// connect echoes the version, then values in args order:
		// hostname,port,username,password,domain
		want := []string{"connect", "VERSION_1_3_0", "100.94.198.79", "3389", "rdptest", "RdpTest2026", ""}
		if !reflect.DeepEqual(r.connect, want) {
			t.Fatalf("connect = %v, want %v", r.connect, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fake guacd timed out")
	}
}
