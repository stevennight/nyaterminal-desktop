package sshclient

import (
	"testing"
	"time"
)

func TestHandleOutputDropsDataDuringZmodemCancelDrainWindow(t *testing.T) {
	session := &terminalSession{
		output: make(chan []byte, 1),
		done:   make(chan struct{}),
	}
	session.zmodemDiscardUntil = time.Now().Add(time.Second)

	if err := session.handleOutput([]byte("residual zmodem bytes")); err != nil {
		t.Fatal(err)
	}
	select {
	case data := <-session.output:
		t.Fatalf("unexpected terminal output: %q", data)
	default:
	}
}

func TestHandleOutputAllowsDataAfterZmodemCancelDrainWindow(t *testing.T) {
	session := &terminalSession{
		output: make(chan []byte, 1),
		done:   make(chan struct{}),
	}
	session.zmodemDiscardUntil = time.Now().Add(-time.Second)

	if err := session.handleOutput([]byte("prompt$ ")); err != nil {
		t.Fatal(err)
	}
	select {
	case data := <-session.output:
		if string(data) != "prompt$ " {
			t.Fatalf("unexpected terminal output: %q", data)
		}
	default:
		t.Fatal("expected terminal output after drain window")
	}
}

func TestZmodemReceiveSizeWarning(t *testing.T) {
	if warning := zmodemReceiveSizeWarning("demo.bin", 3, 7); warning !=
		"demo.bin 大小与发送端声明不一致：声明 3 B，实际 7 B" {
		t.Fatalf("unexpected warning: %q", warning)
	}
	if warning := zmodemReceiveSizeWarning("demo.bin", 7, 7); warning != "" {
		t.Fatalf("unexpected matching-size warning: %q", warning)
	}
	if warning := zmodemReceiveSizeWarning("demo.bin", 0, 7); warning != "" {
		t.Fatalf("unexpected unknown-size warning: %q", warning)
	}
}
