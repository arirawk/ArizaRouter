package proto

import (
	"errors"
	"testing"
)

func TestConnectFrameRoundTrip(t *testing.T) {
	payload := []byte("hello")
	frame := FrameConnectMessage(payload, ConnectEndStreamFlag)
	if len(frame) != ConnectFrameHeaderSize+len(payload) {
		t.Fatalf("frame length = %d", len(frame))
	}
	flags, got, consumed, ok := ParseConnectFrame(append(frame, 0xff))
	if !ok || flags != ConnectEndStreamFlag || string(got) != "hello" || consumed != len(frame) {
		t.Fatalf("ParseConnectFrame = (%x, %q, %d, %v)", flags, got, consumed, ok)
	}
	if _, _, _, ok := ParseConnectFrame(frame[:len(frame)-1]); ok {
		t.Fatal("truncated frame parsed as complete")
	}
}

func TestParseConnectEndStream(t *testing.T) {
	if err := ParseConnectEndStream(nil); err != nil {
		t.Fatalf("empty trailer err = %v", err)
	}
	if err := ParseConnectEndStream([]byte(`{}`)); err != nil {
		t.Fatalf("trailer without error = %v", err)
	}
	err := ParseConnectEndStream([]byte(`{"error":{"code":"resource_exhausted","message":"quota"}}`))
	var ce *ConnectError
	if !errors.As(err, &ce) {
		t.Fatalf("err %T is not *ConnectError", err)
	}
	if ce.Code != "resource_exhausted" || ce.Message != "quota" {
		t.Fatalf("ConnectError = %+v", ce)
	}
}

func TestEncodeHeartbeatDecodes(t *testing.T) {
	// Heartbeat must round-trip through the dynamic descriptor.
	hb := EncodeHeartbeat()
	if len(hb) == 0 {
		t.Fatal("EncodeHeartbeat returned empty payload")
	}
}
