package zmodemproto

import (
	"bytes"
	"testing"
)

func TestHeaderDetectorSplitReceive(t *testing.T) {
	var detector HeaderDetector
	first := detector.Consume([]byte("prompt\r\n**\x18"))
	if first.Mode != "" || string(first.Terminal) != "prompt\r\n" {
		t.Fatalf("unexpected first detection: %#v", first)
	}
	second := detector.Consume([]byte("B00payload"))
	if second.Mode != ModeReceive {
		t.Fatalf("mode = %q, want %q", second.Mode, ModeReceive)
	}
	if string(second.Terminal) != "" {
		t.Fatalf("terminal = %q", second.Terminal)
	}
	if !bytes.HasPrefix(second.Protocol, []byte("**\x18B00")) {
		t.Fatalf("protocol did not retain header: %q", second.Protocol)
	}
}

func TestSenderReceiverRoundTrip(t *testing.T) {
	sender := NewSender(true)
	receiver := NewReceiver()
	payload := bytes.Repeat([]byte("zmodem-data-"), 2048)
	name := "NHK发音词典 物书堂.zip"
	var received []byte
	var receiverStarted, receiverCompleted, senderCompleted bool
	var receiverName string

	if err := sender.StartFile(name, int64(len(payload))); err != nil {
		t.Fatal(err)
	}

	for step := 0; step < 10000; step++ {
		if request := sender.PollFile(); request != nil {
			end := int(request.Offset) + request.Len
			if end > len(payload) {
				end = len(payload)
			}
			if err := sender.FeedFile(payload[request.Offset:end]); err != nil {
				t.Fatal(err)
			}
		}
		if outgoing := sender.DrainOutgoing(); len(outgoing) > 0 {
			if _, err := receiver.FeedIncoming(outgoing); err != nil {
				t.Fatal(err)
			}
		}
		for event := receiver.PollEvent(); event != ""; event = receiver.PollEvent() {
			switch event {
			case ReceiverFileStart:
				receiverStarted = true
				receiverName = receiver.FileName()
			case ReceiverFileComplete:
				receiverCompleted = true
			case ReceiverSessionComplete:
				if !bytes.Equal(received, payload) {
					t.Fatalf("received payload mismatch: got %d bytes, want %d", len(received), len(payload))
				}
				if receiverName != name {
					t.Fatalf("receiver name = %q, want %q", receiverName, name)
				}
				if !receiverStarted || !receiverCompleted || !senderCompleted {
					t.Fatalf("missing events: start=%v receiverComplete=%v senderComplete=%v",
						receiverStarted, receiverCompleted, senderCompleted)
				}
				return
			}
		}
		if data := receiver.DrainFile(); len(data) > 0 {
			received = append(received, data...)
		}
		if outgoing := receiver.DrainOutgoing(); len(outgoing) > 0 {
			if _, err := sender.FeedIncoming(outgoing); err != nil {
				t.Fatal(err)
			}
		}
		for event := sender.PollEvent(); event != ""; event = sender.PollEvent() {
			if event == SenderFileComplete {
				senderCompleted = true
				if err := sender.FinishSession(); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	t.Fatal("round trip did not complete")
}
