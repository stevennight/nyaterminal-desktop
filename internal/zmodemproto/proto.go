package zmodemproto

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
)

const (
	ZPAD byte = 42
	ZDLE byte = 24
	XON  byte = 17

	SubpacketMaxSize = 8192
	SubpacketPerAck  = 200
)

type Mode string

const (
	ModeReceive Mode = "receive"
	ModeSend    Mode = "send"
)

type Detection struct {
	Terminal []byte
	Mode     Mode
	Protocol []byte
}

type HeaderDetector struct {
	buffer []byte
}

var (
	receiveHeaderPrefix = []byte("**\x18B00")
	sendHeaderPrefix    = []byte("**\x18B01")
)

func (d *HeaderDetector) Consume(data []byte) Detection {
	d.buffer = append(d.buffer, data...)
	if mode, offset, ok := detectHeader(d.buffer); ok {
		terminal := append([]byte(nil), d.buffer[:offset]...)
		protocol := append([]byte(nil), d.buffer[offset:]...)
		d.buffer = nil
		return Detection{Terminal: terminal, Mode: mode, Protocol: protocol}
	}
	retain := longestHeaderPrefixSuffix(d.buffer)
	if retain == 0 {
		terminal := append([]byte(nil), d.buffer...)
		d.buffer = nil
		return Detection{Terminal: terminal}
	}
	if len(d.buffer) <= retain {
		return Detection{}
	}
	terminal := append([]byte(nil), d.buffer[:len(d.buffer)-retain]...)
	d.buffer = append([]byte(nil), d.buffer[len(d.buffer)-retain:]...)
	return Detection{Terminal: terminal}
}

func (d *HeaderDetector) Reset() {
	d.buffer = nil
}

func detectHeader(data []byte) (Mode, int, bool) {
	receive := bytes.Index(data, receiveHeaderPrefix)
	send := bytes.Index(data, sendHeaderPrefix)
	if receive >= 0 && (send < 0 || receive < send) {
		return ModeReceive, receive, true
	}
	if send >= 0 {
		return ModeSend, send, true
	}
	return "", 0, false
}

func longestHeaderPrefixSuffix(buffer []byte) int {
	maxLength := min(len(buffer), len(receiveHeaderPrefix)-1)
	for retain := maxLength; retain > 0; retain-- {
		if matchesPrefix(buffer, retain, receiveHeaderPrefix) || matchesPrefix(buffer, retain, sendHeaderPrefix) {
			return retain
		}
	}
	return 0
}

func matchesPrefix(buffer []byte, length int, pattern []byte) bool {
	if length > len(pattern) {
		return false
	}
	offset := len(buffer) - length
	return bytes.Equal(buffer[offset:], pattern[:length])
}

type Encoding byte

const (
	EncodingZBIN   Encoding = 0x41
	EncodingZHEX   Encoding = 0x42
	EncodingZBIN32 Encoding = 0x43
)

type Frame byte

const (
	FrameZRQINIT Frame = iota
	FrameZRINIT
	FrameZSINIT
	FrameZACK
	FrameZFILE
	FrameZSKIP
	FrameZNAK
	FrameZABORT
	FrameZFIN
	FrameZRPOS
	FrameZDATA
	FrameZEOF
	FrameZFERR
	FrameZCRC
	FrameZCHALLENGE
	FrameZCOMPL
	FrameZCAN
	FrameZFREECNT
	FrameZCOMMAND
	FrameZSTDERR
)

type Zrinit byte

const (
	ZrinitCANFDX  Zrinit = 1
	ZrinitCANOVIO Zrinit = 2
	ZrinitCANFC32 Zrinit = 32
)

type Header struct {
	Encoding Encoding
	Frame    Frame
	Flags    [4]byte
}

func NewHeader(encoding Encoding, frame Frame) Header {
	return Header{Encoding: encoding, Frame: frame}
}

func (h Header) Count() uint32 {
	return binary.LittleEndian.Uint32(h.Flags[:])
}

func (h Header) WithCount(count uint32) Header {
	binary.LittleEndian.PutUint32(h.Flags[:], count)
	return h
}

func (h Header) Encode() []byte {
	result := []byte{ZPAD}
	if h.Encoding == EncodingZHEX {
		result = append(result, ZPAD)
	}
	result = append(result, ZDLE, byte(h.Encoding))
	payload := []byte{byte(h.Frame), h.Flags[0], h.Flags[1], h.Flags[2], h.Flags[3]}
	if h.Encoding == EncodingZBIN32 {
		crc := crc32ISOHDLC(payload)
		var crcBytes [4]byte
		binary.LittleEndian.PutUint32(crcBytes[:], crc)
		payload = append(payload, crcBytes[:]...)
	} else {
		crc := crc16Xmodem(payload)
		payload = append(payload, byte(crc>>8), byte(crc))
	}
	if h.Encoding == EncodingZHEX {
		hexPayload := make([]byte, hex.EncodedLen(len(payload)))
		hex.Encode(hexPayload, payload)
		result = append(result, hexPayload...)
		result = append(result, '\r', '\n')
		if h.Frame != FrameZACK && h.Frame != FrameZFIN {
			result = append(result, XON)
		}
		return result
	}
	return append(result, writeSliceEscaped(payload)...)
}

func createZrinit(bufferSize int, flags Zrinit) Header {
	header := NewHeader(EncodingZHEX, FrameZRINIT)
	header.Flags[0] = byte(bufferSize)
	header.Flags[1] = byte(bufferSize >> 8)
	header.Flags[3] = byte(flags)
	return header
}

func decodeHeader(encoding Encoding, data []byte) (Header, error) {
	payload := data
	if encoding == EncodingZHEX {
		if len(data)%2 != 0 {
			return Header{}, errors.New("malformed ZMODEM header")
		}
		payload = make([]byte, hex.DecodedLen(len(data)))
		if _, err := hex.Decode(payload, data); err != nil {
			return Header{}, fmt.Errorf("malformed ZMODEM header: %w", err)
		}
	}
	crcLen := 2
	if encoding == EncodingZBIN32 {
		crcLen = 4
	}
	if len(payload) < 5+crcLen {
		return Header{}, errors.New("malformed ZMODEM header")
	}
	headerPayload := payload[:5]
	crcBytes := payload[5 : 5+crcLen]
	if encoding == EncodingZBIN32 {
		expected := crc32ISOHDLC(headerPayload)
		if binary.LittleEndian.Uint32(crcBytes) != expected {
			return Header{}, errors.New("unexpected ZMODEM CRC32")
		}
	} else {
		expected := crc16Xmodem(headerPayload)
		received := uint16(crcBytes[0])<<8 | uint16(crcBytes[1])
		if received != expected {
			return Header{}, errors.New("unexpected ZMODEM CRC16")
		}
	}
	frame := Frame(headerPayload[0])
	if frame > FrameZSTDERR {
		return Header{}, fmt.Errorf("malformed ZMODEM frame: %d", frame)
	}
	var flags [4]byte
	copy(flags[:], headerPayload[1:5])
	return Header{Encoding: encoding, Frame: frame, Flags: flags}, nil
}

type headerReader struct {
	state         int
	zpadState     int
	buf           []byte
	encoding      Encoding
	expectedLen   int
	escapePending bool
}

func (r *headerReader) read(input []byte, startOffset int) (Header, int, bool, error) {
	consumed := startOffset
	for consumed < len(input) {
		switch r.state {
		case 0:
			b := input[consumed]
			consumed++
			if r.advanceZpadState(b) {
				r.state = 1
			}
		case 1:
			b := input[consumed]
			consumed++
			switch Encoding(b) {
			case EncodingZBIN, EncodingZHEX, EncodingZBIN32:
				r.encoding = Encoding(b)
			default:
				r.reset()
				return Header{}, consumed, false, fmt.Errorf("malformed ZMODEM packet type: 0x%02x", b)
			}
			r.expectedLen = headerReadSize(r.encoding)
			r.escapePending = false
			r.buf = nil
			r.state = 2
		case 2:
			for len(r.buf) < r.expectedLen && consumed < len(input) {
				b := input[consumed]
				consumed++
				if r.escapePending {
					r.escapePending = false
					r.buf = append(r.buf, unzdle(b))
				} else if b == ZDLE {
					r.escapePending = true
				} else {
					r.buf = append(r.buf, b)
				}
			}
			if len(r.buf) >= r.expectedLen {
				header, err := decodeHeader(r.encoding, r.buf)
				r.reset()
				return header, consumed, true, err
			}
		}
	}
	return Header{}, consumed, false, nil
}

func (r *headerReader) reset() {
	r.state = 0
	r.zpadState = 0
	r.buf = nil
	r.encoding = 0
	r.expectedLen = 0
	r.escapePending = false
}

func (r *headerReader) advanceZpadState(b byte) bool {
	switch r.zpadState {
	case 0:
		if b == ZPAD {
			r.zpadState = 1
		}
	case 1, 2:
		if b == ZDLE {
			r.zpadState = 0
			return true
		}
		if b == ZPAD {
			r.zpadState = 2
		} else {
			r.zpadState = 0
		}
	}
	return false
}

func headerReadSize(encoding Encoding) int {
	if encoding == EncodingZHEX {
		return 14
	}
	if encoding == EncodingZBIN32 {
		return 9
	}
	return 7
}

type byteBuffer struct {
	data     []byte
	capacity int
}

func newByteBuffer(capacity int) byteBuffer {
	return byteBuffer{capacity: capacity}
}

func (b *byteBuffer) clear() {
	b.data = nil
}

func (b *byteBuffer) push(value byte) error {
	if len(b.data) >= b.capacity {
		return errors.New("ZMODEM buffer overflow")
	}
	b.data = append(b.data, value)
	return nil
}

func (b *byteBuffer) extend(values []byte) error {
	for _, value := range values {
		if err := b.push(value); err != nil {
			return err
		}
	}
	return nil
}

func (b *byteBuffer) slice(start int) []byte {
	return append([]byte(nil), b.data[start:]...)
}

type SubpacketType byte

const (
	SubpacketZCRCE SubpacketType = 0x68
	SubpacketZCRCG SubpacketType = 0x69
	SubpacketZCRCQ SubpacketType = 0x6a
	SubpacketZCRCW SubpacketType = 0x6b
)

func subpacketTypeFromByte(value byte) (SubpacketType, bool) {
	switch SubpacketType(value) {
	case SubpacketZCRCE, SubpacketZCRCG, SubpacketZCRCQ, SubpacketZCRCW:
		return SubpacketType(value), true
	default:
		return 0, false
	}
}

type SenderEvent string

const (
	SenderFileComplete    SenderEvent = "FileComplete"
	SenderSessionComplete SenderEvent = "SessionComplete"
)

type FileRequest struct {
	Offset int64
	Len    int
}

type senderState int

const (
	sendWaitReceiverInit senderState = iota
	sendReadyForFile
	sendWaitFilePos
	sendNeedFileData
	sendWaitFileAck
	sendWaitFileDone
	sendWaitFinish
	sendDone
)

type Sender struct {
	state               senderState
	fileName            string
	fileSize            int64
	hasFile             bool
	pendingRequest      *FileRequest
	frameRemaining      int
	frameNeedsHeader    bool
	maxSubpacketSize    int
	maxSubpacketsPerAck int
	outgoing            byteBuffer
	headerReader        headerReader
	pendingEvent        SenderEvent
	finishRequested     bool
}

func NewSender(initiator bool) *Sender {
	s := &Sender{
		state:               sendWaitReceiverInit,
		maxSubpacketSize:    SubpacketMaxSize,
		maxSubpacketsPerAck: SubpacketPerAck,
		outgoing:            newByteBuffer(256 * 1024),
	}
	if initiator {
		s.queueZrqinit()
	}
	return s
}

func (s *Sender) StartFile(name string, size int64) error {
	if s.state == sendDone || s.state == sendWaitFinish ||
		(s.state != sendWaitReceiverInit && s.state != sendReadyForFile) {
		return errors.New("unsupported ZMODEM sender state")
	}
	s.fileName = name
	s.fileSize = size
	s.hasFile = true
	s.pendingRequest = nil
	s.frameRemaining = 0
	s.frameNeedsHeader = false
	if s.state == sendReadyForFile {
		if s.hasOutgoing() {
			return errors.New("ZMODEM sender has pending outgoing data")
		}
		if err := s.queueZfile(); err != nil {
			return err
		}
		s.state = sendWaitFilePos
	}
	return nil
}

func (s *Sender) FinishSession() error {
	s.finishRequested = true
	if s.state == sendReadyForFile {
		if s.hasOutgoing() {
			return errors.New("ZMODEM sender has pending outgoing data")
		}
		s.queueZfin()
		s.state = sendWaitFinish
	}
	return nil
}

func (s *Sender) PollFile() *FileRequest {
	return s.pendingRequest
}

func (s *Sender) FeedFile(data []byte) error {
	if s.state != sendNeedFileData || s.pendingRequest == nil {
		return errors.New("unsupported ZMODEM sender file state")
	}
	request := s.pendingRequest
	if len(data) == 0 || len(data) > request.Len {
		return errors.New("unexpected EOF while sending ZMODEM file")
	}
	remaining := s.fileSize - request.Offset
	if int64(len(data)) > remaining {
		return errors.New("unexpected EOF while sending ZMODEM file")
	}
	if s.hasOutgoing() {
		return errors.New("ZMODEM sender has pending outgoing data")
	}
	nextOffset := request.Offset + int64(len(data))
	remainingAfter := s.fileSize - nextOffset
	maxLen := min(s.maxSubpacketSize, int(remainingAfter))
	isLastInFrame := s.frameRemaining <= 1 || len(data) < request.Len || remainingAfter == 0
	kind := SubpacketZCRCG
	if isLastInFrame {
		kind = SubpacketZCRCW
	}
	if err := s.queueZdata(request.Offset, data, kind, s.frameNeedsHeader); err != nil {
		return err
	}
	s.frameNeedsHeader = false
	if s.frameRemaining > 0 {
		s.frameRemaining--
	}
	if isLastInFrame {
		s.pendingRequest = nil
		s.state = sendWaitFileAck
		s.frameRemaining = 0
	} else {
		s.pendingRequest = &FileRequest{Offset: nextOffset, Len: maxLen}
	}
	return nil
}

func (s *Sender) FeedIncoming(input []byte) (int, error) {
	consumed := 0
	for {
		if s.hasOutgoing() || s.state == sendDone || s.pendingRequest != nil {
			break
		}
		before := consumed
		header, next, ok, err := s.headerReader.read(input, consumed)
		if err != nil {
			return next, err
		}
		if !ok {
			break
		}
		consumed = next
		if err := s.handleHeader(header); err != nil {
			return consumed, err
		}
		if consumed == before || consumed == len(input) {
			break
		}
	}
	return consumed, nil
}

func (s *Sender) DrainOutgoing() []byte {
	data := s.outgoing.slice(0)
	s.outgoing.clear()
	return data
}

func (s *Sender) PollEvent() SenderEvent {
	event := s.pendingEvent
	s.pendingEvent = ""
	return event
}

func (s *Sender) hasOutgoing() bool {
	return len(s.outgoing.data) > 0
}

func (s *Sender) queueZrqinit() {
	s.outgoing.clear()
	_ = s.outgoing.extend(NewHeader(EncodingZHEX, FrameZRQINIT).Encode())
}

func (s *Sender) queueZfile() error {
	result := NewHeader(EncodingZBIN32, FrameZFILE).Encode()
	fileInfo := append([]byte(s.fileName), 0)
	fileInfo = append(fileInfo, []byte(strconv.FormatInt(s.fileSize, 10))...)
	fileInfo = append(fileInfo, 0)
	result = append(result, writeSliceEscaped(fileInfo)...)
	result = append(result, ZDLE, byte(SubpacketZCRCW))
	crc := newCRC32()
	crc.update(fileInfo)
	crc.updateByte(byte(SubpacketZCRCW))
	result = append(result, writeSliceEscaped(uint32LE(crc.finalize()))...)
	s.outgoing.clear()
	return s.outgoing.extend(result)
}

func (s *Sender) queueZdata(offset int64, data []byte, kind SubpacketType, includeHeader bool) error {
	var result []byte
	if includeHeader {
		result = append(result, NewHeader(EncodingZBIN32, FrameZDATA).WithCount(uint32(offset)).Encode()...)
	}
	result = append(result, writeSliceEscaped(data)...)
	result = append(result, ZDLE, byte(kind))
	crc := newCRC32()
	crc.update(data)
	crc.updateByte(byte(kind))
	result = append(result, writeSliceEscaped(uint32LE(crc.finalize()))...)
	s.outgoing.clear()
	return s.outgoing.extend(result)
}

func (s *Sender) queueZeof(offset int64) {
	s.outgoing.clear()
	_ = s.outgoing.extend(NewHeader(EncodingZBIN32, FrameZEOF).WithCount(uint32(offset)).Encode())
}

func (s *Sender) queueZfin() {
	s.outgoing.clear()
	_ = s.outgoing.extend(NewHeader(EncodingZHEX, FrameZFIN).Encode())
}

func (s *Sender) queueOo() {
	s.outgoing.clear()
	_ = s.outgoing.extend([]byte{'O', 'O'})
}

func (s *Sender) handleHeader(header Header) error {
	switch header.Frame {
	case FrameZRINIT:
		return s.onZrinit(header)
	case FrameZRPOS, FrameZACK:
		s.onZrpos(int64(header.Count()))
	case FrameZFIN:
		s.onZfin()
	default:
		if s.state == sendWaitReceiverInit {
			s.queueZrqinit()
		}
	}
	return nil
}

func (s *Sender) onZrinit(header Header) error {
	caps := header.Flags[3]
	if Zrinit(caps)&ZrinitCANOVIO != 0 {
		s.maxSubpacketsPerAck = SubpacketPerAck
	} else {
		s.maxSubpacketsPerAck = 1
	}
	switch s.state {
	case sendWaitReceiverInit:
		if s.hasFile {
			if err := s.queueZfile(); err != nil {
				return err
			}
			s.state = sendWaitFilePos
		} else {
			s.state = sendReadyForFile
			if s.finishRequested {
				s.queueZfin()
				s.state = sendWaitFinish
			}
		}
	case sendWaitFileDone:
		s.pendingEvent = SenderFileComplete
		s.hasFile = false
		if s.finishRequested {
			s.queueZfin()
			s.state = sendWaitFinish
		} else {
			s.state = sendReadyForFile
		}
	case sendWaitFinish:
		s.queueOo()
		s.state = sendDone
		s.pendingEvent = SenderSessionComplete
	}
	return nil
}

func (s *Sender) onZrpos(offset int64) {
	switch s.state {
	case sendWaitReceiverInit:
		s.queueZrqinit()
	case sendWaitFilePos, sendWaitFileAck, sendNeedFileData:
		if offset >= s.fileSize {
			s.queueZeof(offset)
			s.state = sendWaitFileDone
			s.pendingRequest = nil
		} else {
			remaining := s.fileSize - offset
			maxSubpackets := (int(remaining) + s.maxSubpacketSize - 1) / s.maxSubpacketSize
			s.frameRemaining = min(s.maxSubpacketsPerAck, maxSubpackets)
			s.frameNeedsHeader = true
			s.pendingRequest = &FileRequest{Offset: offset, Len: min(s.maxSubpacketSize, int(remaining))}
			s.state = sendNeedFileData
		}
	}
}

func (s *Sender) onZfin() {
	if s.state == sendWaitFinish {
		s.queueOo()
		s.state = sendDone
		s.pendingEvent = SenderSessionComplete
	}
}

type ReceiverEvent string

const (
	ReceiverFileStart       ReceiverEvent = "FileStart"
	ReceiverFileComplete    ReceiverEvent = "FileComplete"
	ReceiverSessionComplete ReceiverEvent = "SessionComplete"
)

type recvState int

const (
	recvSessionBegin recvState = iota
	recvFileBegin
	recvFileReadingMetadata
	recvFileReadingSubpacket
	recvFileWaitingSubpacket
	recvSessionEnd
)

type Receiver struct {
	state                  recvState
	count                  int64
	fileName               string
	fileSize               int64
	buf                    byteBuffer
	bufWriteOffset         int
	dataEncoding           Encoding
	headerReader           headerReader
	subpacketState         int
	subpacketType          SubpacketType
	subpacketEscapePending bool
	crcEscapePending       bool
	crcBytesRead           int
	crcBuf                 []byte
	crc16                  crc16
	crc32                  crc32
	outgoing               byteBuffer
	events                 []ReceiverEvent
}

func NewReceiver() *Receiver {
	r := &Receiver{
		state:         recvSessionBegin,
		buf:           newByteBuffer(SubpacketMaxSize),
		dataEncoding:  EncodingZBIN,
		subpacketType: SubpacketZCRCG,
		crc16:         newCRC16(),
		crc32:         newCRC32(),
		outgoing:      newByteBuffer(256 * 1024),
	}
	r.queueZrinit()
	return r
}

func (r *Receiver) FeedIncoming(input []byte) (int, error) {
	consumed := 0
	for {
		if r.hasFileData() || len(r.events) >= 4 {
			break
		}
		before := consumed
		if r.state == recvFileReadingSubpacket || r.state == recvFileReadingMetadata {
			next, done, err := r.processSubpacket(input, consumed)
			if err != nil {
				return next, err
			}
			consumed = next
			if done {
				if r.hasOutgoing() || r.hasFileData() || len(r.events) >= 4 || consumed == before {
					break
				}
				continue
			}
			break
		}
		header, next, ok, err := r.headerReader.read(input, consumed)
		if err != nil {
			return next, err
		}
		if !ok {
			break
		}
		consumed = next
		r.handleHeader(header)
		if len(r.events) >= 4 || r.hasOutgoing() || consumed == before || consumed == len(input) {
			break
		}
	}
	return consumed, nil
}

func (r *Receiver) DrainOutgoing() []byte {
	data := r.outgoing.slice(0)
	r.outgoing.clear()
	return data
}

func (r *Receiver) DrainFile() []byte {
	if r.subpacketState != 2 {
		return nil
	}
	data := r.buf.slice(r.bufWriteOffset)
	r.finishSubpacket(r.subpacketType)
	return data
}

func (r *Receiver) PollEvent() ReceiverEvent {
	if len(r.events) == 0 {
		return ""
	}
	event := r.events[0]
	copy(r.events, r.events[1:])
	r.events = r.events[:len(r.events)-1]
	return event
}

func (r *Receiver) FileName() string {
	return r.fileName
}

func (r *Receiver) FileSize() int64 {
	return r.fileSize
}

func (r *Receiver) hasOutgoing() bool {
	return len(r.outgoing.data) > 0
}

func (r *Receiver) hasFileData() bool {
	return r.subpacketState == 2
}

func (r *Receiver) pushEvent(event ReceiverEvent) {
	r.events = append(r.events, event)
}

func (r *Receiver) queueZrinit() {
	header := createZrinit(SubpacketMaxSize, ZrinitCANFDX|ZrinitCANOVIO|ZrinitCANFC32)
	r.outgoing.clear()
	_ = r.outgoing.extend(header.Encode())
}

func (r *Receiver) queueZrpos(count int64) {
	r.outgoing.clear()
	_ = r.outgoing.extend(NewHeader(EncodingZHEX, FrameZRPOS).WithCount(uint32(count)).Encode())
}

func (r *Receiver) queueZack() {
	r.outgoing.clear()
	_ = r.outgoing.extend(NewHeader(EncodingZHEX, FrameZACK).WithCount(uint32(r.count)).Encode())
}

func (r *Receiver) queueZfin() {
	r.outgoing.clear()
	_ = r.outgoing.extend(NewHeader(EncodingZHEX, FrameZFIN).Encode())
}

func (r *Receiver) handleHeader(header Header) {
	switch header.Frame {
	case FrameZRQINIT:
		if r.state == recvSessionBegin {
			r.queueZrinit()
		}
	case FrameZFILE:
		if r.state == recvSessionBegin || r.state == recvFileBegin {
			r.dataEncoding = header.Encoding
			r.state = recvFileReadingMetadata
			r.subpacketState = 1
			r.subpacketEscapePending = false
			r.resetCrc()
			r.buf.clear()
			r.bufWriteOffset = 0
		}
	case FrameZDATA:
		if r.state == recvSessionBegin {
			r.queueZrinit()
		} else if r.state == recvFileBegin || r.state == recvFileWaitingSubpacket {
			if int64(header.Count()) != r.count {
				r.queueZrpos(r.count)
				return
			}
			r.dataEncoding = header.Encoding
			r.state = recvFileReadingSubpacket
			r.subpacketState = 1
			r.subpacketEscapePending = false
			r.resetCrc()
			r.buf.clear()
			r.bufWriteOffset = 0
		}
	case FrameZEOF:
		if r.state == recvFileWaitingSubpacket && int64(header.Count()) == r.count {
			r.queueZrinit()
			r.state = recvFileBegin
			r.pushEvent(ReceiverFileComplete)
		}
	case FrameZFIN:
		if r.state == recvFileWaitingSubpacket || r.state == recvFileBegin {
			r.queueZfin()
			r.state = recvSessionEnd
			r.pushEvent(ReceiverSessionComplete)
		}
	}
}

func (r *Receiver) resetCrc() {
	r.crc16 = newCRC16()
	r.crc32 = newCRC32()
	r.crcEscapePending = false
	r.crcBytesRead = 0
	r.crcBuf = nil
}

func (r *Receiver) updateCrc(b byte) {
	if r.dataEncoding == EncodingZBIN32 {
		r.crc32.updateByte(b)
	} else {
		r.crc16.updateByte(b)
	}
}

func (r *Receiver) processSubpacket(input []byte, startOffset int) (int, bool, error) {
	consumed := startOffset
	for consumed < len(input) {
		b := input[consumed]
		switch r.subpacketState {
		case 1:
			if r.subpacketEscapePending {
				r.subpacketEscapePending = false
				if packetType, ok := subpacketTypeFromByte(b); ok {
					r.updateCrc(byte(packetType))
					r.subpacketType = packetType
					r.subpacketState = 3
				} else {
					unescaped := unzdle(b)
					if err := r.buf.push(unescaped); err != nil {
						return consumed, false, err
					}
					r.updateCrc(unescaped)
				}
				consumed++
			} else if b == ZDLE {
				r.subpacketEscapePending = true
				consumed++
			} else {
				if err := r.buf.push(b); err != nil {
					return consumed, false, err
				}
				r.updateCrc(b)
				consumed++
			}
		case 3:
			crcLen := 2
			if r.dataEncoding == EncodingZBIN32 {
				crcLen = 4
			}
			for consumed < len(input) && r.crcBytesRead < crcLen {
				current := input[consumed]
				if r.crcEscapePending {
					r.crcEscapePending = false
					r.crcBuf = append(r.crcBuf, unzdle(current))
					r.crcBytesRead++
					consumed++
				} else if current == ZDLE {
					r.crcEscapePending = true
					consumed++
				} else {
					r.crcBuf = append(r.crcBuf, current)
					r.crcBytesRead++
					consumed++
				}
			}
			if r.crcBytesRead < crcLen {
				return consumed, false, nil
			}
			if r.dataEncoding == EncodingZBIN32 {
				expected := r.crc32.finalize()
				received := binary.LittleEndian.Uint32(r.crcBuf[:4])
				if expected != received {
					return consumed, false, errors.New("unexpected ZMODEM CRC32")
				}
			} else {
				expected := r.crc16.finalize()
				received := uint16(r.crcBuf[0])<<8 | uint16(r.crcBuf[1])
				if expected != received {
					return consumed, false, errors.New("unexpected ZMODEM CRC16")
				}
			}
			if r.state == recvFileReadingMetadata {
				if err := r.parseZfileBuf(); err != nil {
					return consumed, false, err
				}
				r.buf.clear()
				r.bufWriteOffset = 0
				r.resetCrc()
				r.subpacketEscapePending = false
				r.queueZrpos(0)
				r.state = recvFileBegin
				r.subpacketState = 0
				r.pushEvent(ReceiverFileStart)
			} else {
				r.subpacketState = 2
				r.bufWriteOffset = 0
				if len(r.buf.data) == 0 {
					r.finishSubpacket(r.subpacketType)
				}
			}
			return consumed, true, nil
		case 2:
			return consumed, true, nil
		default:
			return consumed, false, errors.New("unsupported ZMODEM receiver state")
		}
	}
	return consumed, false, nil
}

func (r *Receiver) parseZfileBuf() error {
	fields := bytes.Split(r.buf.data, []byte{0})
	if len(fields) == 0 || len(fields[0]) == 0 {
		return errors.New("malformed ZMODEM file name")
	}
	r.fileName = string(fields[0])
	if len(fields) > 1 && len(fields[1]) > 0 {
		sizeField := fields[1]
		if index := bytes.IndexByte(sizeField, ' '); index >= 0 {
			sizeField = sizeField[:index]
		}
		size, err := strconv.ParseInt(string(sizeField), 10, 64)
		if err != nil {
			return errors.New("malformed ZMODEM file size")
		}
		r.fileSize = size
	} else {
		r.fileSize = 0
	}
	r.count = 0
	return nil
}

func (r *Receiver) finishSubpacket(packet SubpacketType) {
	r.count += int64(len(r.buf.data))
	r.buf.clear()
	r.bufWriteOffset = 0
	r.resetCrc()
	switch packet {
	case SubpacketZCRCW:
		r.queueZack()
		r.state = recvFileWaitingSubpacket
		r.subpacketState = 0
	case SubpacketZCRCQ:
		r.queueZack()
		r.subpacketState = 1
	case SubpacketZCRCG:
		r.subpacketState = 1
	case SubpacketZCRCE:
		r.state = recvFileWaitingSubpacket
		r.subpacketState = 0
	}
	r.subpacketEscapePending = false
}

func writeSliceEscaped(data []byte) []byte {
	result := make([]byte, 0, len(data))
	for _, b := range data {
		escaped, needsPrefix := zdle(b)
		if needsPrefix {
			result = append(result, ZDLE)
		}
		result = append(result, escaped)
	}
	return result
}

func zdle(value byte) (byte, bool) {
	switch value {
	case 0x0d, 0x10, 0x11, 0x13, 0x18, 0x8d, 0x90, 0x91, 0x93:
		return value ^ 0x40, true
	case 0x7f:
		return 0x6c, true
	case 0xff:
		return 0x6d, true
	default:
		return value, false
	}
}

func unzdle(value byte) byte {
	switch value {
	case 0x6c:
		return 0x7f
	case 0x6d:
		return 0xff
	case 0xcd:
		return 0x8d
	case 0xd0:
		return 0x90
	case 0xd1:
		return 0x91
	case 0xd3:
		return 0x93
	default:
		if value >= 0x40 && value <= 0x5f {
			return value ^ 0x40
		}
		return value
	}
}

type crc16 struct {
	value uint16
}

func newCRC16() crc16 {
	return crc16{}
}

func (c *crc16) update(data []byte) {
	for _, b := range data {
		c.updateByte(b)
	}
}

func (c *crc16) updateByte(b byte) {
	c.value = crc16Update(c.value, b)
}

func (c *crc16) finalize() uint16 {
	return c.value
}

func crc16Xmodem(data []byte) uint16 {
	crc := uint16(0)
	for _, b := range data {
		crc = crc16Update(crc, b)
	}
	return crc
}

func crc16Update(crc uint16, b byte) uint16 {
	crc ^= uint16(b) << 8
	for i := 0; i < 8; i++ {
		if crc&0x8000 != 0 {
			crc = (crc << 1) ^ 0x1021
		} else {
			crc <<= 1
		}
	}
	return crc
}

type crc32 struct {
	value uint32
}

func newCRC32() crc32 {
	return crc32{value: 0xffffffff}
}

func (c *crc32) update(data []byte) {
	for _, b := range data {
		c.updateByte(b)
	}
}

func (c *crc32) updateByte(b byte) {
	c.value = crc32Update(c.value, b)
}

func (c *crc32) finalize() uint32 {
	return c.value ^ 0xffffffff
}

func crc32ISOHDLC(data []byte) uint32 {
	crc := uint32(0xffffffff)
	for _, b := range data {
		crc = crc32Update(crc, b)
	}
	return crc ^ 0xffffffff
}

func crc32Update(crc uint32, b byte) uint32 {
	crc ^= uint32(b)
	for i := 0; i < 8; i++ {
		if crc&1 != 0 {
			crc = (crc >> 1) ^ 0xedb88320
		} else {
			crc >>= 1
		}
	}
	return crc
}

func uint32LE(value uint32) []byte {
	result := make([]byte, 4)
	binary.LittleEndian.PutUint32(result, value)
	return result
}
