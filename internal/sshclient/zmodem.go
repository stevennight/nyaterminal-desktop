package sshclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nyaterminal/nyaterminal-desktop/internal/zmodemproto"
)

type ZmodemStatus struct {
	SessionID    string `json:"sessionId"`
	ConnectionID string `json:"connectionId,omitempty"`
	Mode         string `json:"mode,omitempty"`
	Direction    string `json:"direction,omitempty"`
	Name         string `json:"name,omitempty"`
	Status       string `json:"status,omitempty"`
	BytesDone    int64  `json:"bytesDone,omitempty"`
	TotalBytes   int64  `json:"totalBytes,omitempty"`
	Message      string `json:"message"`
	Active       bool   `json:"active"`
}

type ZmodemSendFile struct {
	Path string
	Name string
	Size int64
}

type ZmodemReceiveFile interface {
	io.Writer
	Finish() error
	Cancel() error
}

type ZmodemHandler interface {
	BeginZmodemDownload(ctx context.Context, sessionID, name string, size int64) (ZmodemReceiveFile, error)
	ChooseZmodemUpload(ctx context.Context, sessionID string) ([]ZmodemSendFile, error)
	NotifyZmodemStatus(status ZmodemStatus)
}

type backendZmodemDetector = zmodemproto.HeaderDetector

type zmodemTransfer struct {
	session         *terminalSession
	handler         ZmodemHandler
	mode            zmodemproto.Mode
	receiver        *zmodemproto.Receiver
	sender          *zmodemproto.Sender
	receiveFile     ZmodemReceiveFile
	receiveName     string
	receiveSize     int64
	received        int64
	receiveInput    []byte
	receiveWarnings []string
	sendFiles       []ZmodemSendFile
	sendIndex       int
	sendFile        *os.File
	sendBytesDone   int64
	lastStatusAt    time.Time
}

const zmodemCancelDrainWindow = 1200 * time.Millisecond

func (s *terminalSession) handleOutput(data []byte) error {
	s.zmodemMu.Lock()
	defer s.zmodemMu.Unlock()
	if s.shouldDiscardOutput() {
		s.extendDiscardWindow()
		return nil
	}
	if s.zmodem != nil {
		return s.zmodem.consume(data)
	}
	handler := s.currentZmodemHandler()
	if handler == nil {
		return s.sendTerminal(data)
	}
	detection := s.detector.Consume(data)
	if len(detection.Terminal) > 0 {
		if err := s.sendTerminal(detection.Terminal); err != nil {
			return err
		}
	}
	if detection.Mode == "" {
		return nil
	}
	s.detector.Reset()
	transfer := &zmodemTransfer{
		session: s,
		handler: handler,
		mode:    detection.Mode,
	}
	s.zmodem = transfer
	if detection.Mode == zmodemproto.ModeReceive {
		transfer.receiver = zmodemproto.NewReceiver()
		transfer.notify("检测到远端 sz，正在接收文件", true)
		return transfer.consume(detection.Protocol)
	}
	transfer.sender = zmodemproto.NewSender(false)
	transfer.notify("检测到远端 rz，请选择要发送的文件", true)
	files, err := handler.ChooseZmodemUpload(context.Background(), s.id)
	if err != nil || len(files) == 0 {
		transfer.cancelLocal()
		s.zmodem = nil
		_ = s.writeRemoteCancel()
		if err != nil {
			transfer.notify(err.Error(), false)
			return nil
		}
		transfer.notify("ZMODEM 传输已取消", false)
		return nil
	}
	transfer.sendFiles = files
	if err := transfer.senderConsume(detection.Protocol); err != nil {
		transfer.fail(err)
		return nil
	}
	if err := transfer.startCurrentFile(); err != nil {
		transfer.fail(err)
		return nil
	}
	return transfer.flushSender()
}

func (s *terminalSession) currentZmodemHandler() ZmodemHandler {
	if s == nil || s.manager == nil {
		return nil
	}
	s.manager.mu.RLock()
	defer s.manager.mu.RUnlock()
	return s.manager.zmodemHandler
}

func (s *terminalSession) sendTerminal(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	copyOfData := append([]byte(nil), data...)
	select {
	case <-s.done:
		return io.ErrClosedPipe
	case s.output <- copyOfData:
		return nil
	}
}

func (s *terminalSession) writeRemote(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	select {
	case <-s.done:
		return io.ErrClosedPipe
	default:
	}
	_, err := s.stdin.Write(data)
	return err
}

func (s *terminalSession) writeRemoteCancel() error {
	return s.writeRemote(bytesRepeat(0x18, 8))
}

func (s *terminalSession) cancelZmodem() error {
	s.zmodemMu.Lock()
	defer s.zmodemMu.Unlock()
	if s.zmodem == nil {
		return nil
	}
	s.extendDiscardWindow()
	active := s.zmodem.hasActiveTransfer()
	name, done, total, direction := s.zmodem.currentTransferSnapshot()
	s.zmodem.cancelLocal()
	if active && name != "" {
		s.zmodem.notifyTransferWithDirection("cancelled", "ZMODEM 传输已取消", false, name, done, total, direction)
	}
	s.zmodem.notify("ZMODEM 传输已取消", false)
	s.zmodem = nil
	s.detector.Reset()
	return s.writeRemoteCancel()
}

func (z *zmodemTransfer) consume(data []byte) error {
	if z.mode == zmodemproto.ModeReceive {
		return z.receiverConsume(data)
	}
	return z.senderConsume(data)
}

func (z *zmodemTransfer) receiverConsume(data []byte) error {
	z.receiveInput = append(z.receiveInput, data...)
	for z.receiver != nil && len(z.receiveInput) > 0 {
		consumed, err := z.receiver.FeedIncoming(z.receiveInput)
		if consumed > 0 {
			z.receiveInput = z.receiveInput[consumed:]
		}
		if err != nil {
			z.fail(err)
			return nil
		}
		progress, err := z.flushReceiver()
		if err != nil {
			z.fail(err)
			return nil
		}
		if consumed == 0 && !progress {
			break
		}
	}
	if _, err := z.flushReceiver(); err != nil {
		z.fail(err)
	}
	return nil
}

func (z *zmodemTransfer) flushReceiver() (bool, error) {
	if z.receiver == nil {
		return false, nil
	}
	progress := false
	for {
		localProgress := false
		for event := z.receiver.PollEvent(); event != ""; event = z.receiver.PollEvent() {
			progress = true
			localProgress = true
			switch event {
			case zmodemproto.ReceiverFileStart:
				name := safeZmodemFilename(decodeProtocolFilename(z.receiver.FileName()))
				z.receiveName = name
				z.receiveSize = z.receiver.FileSize()
				z.received = 0
				file, err := z.handler.BeginZmodemDownload(context.Background(), z.session.id, name, z.receiveSize)
				if err != nil || file == nil {
					if err == nil {
						err = errors.New("已取消 ZMODEM 接收")
					}
					return progress, err
				}
				z.receiveFile = file
				z.notifyTransfer("running", fmt.Sprintf("正在接收 %s · 0%%", z.receiveName), true, z.receiveName, 0, z.receiveSize)
				z.notify(fmt.Sprintf("正在接收 %s · 0%%", z.receiveName), true)
			case zmodemproto.ReceiverFileComplete:
				if z.receiveFile != nil {
					if err := z.receiveFile.Finish(); err != nil {
						return progress, err
					}
					z.receiveFile = nil
				}
				warning := zmodemReceiveSizeWarning(z.receiveName, z.receiveSize, z.received)
				if warning != "" {
					z.receiveWarnings = append(z.receiveWarnings, warning)
				}
				message := zmodemReceiveCompleteMessage(z.receiveName, warning)
				z.notifyTransfer("completed", message, true, z.receiveName, z.received, z.received)
				z.notify(message, true)
			case zmodemproto.ReceiverSessionComplete:
				z.cancelLocal()
				message := "ZMODEM 接收完成"
				if len(z.receiveWarnings) > 0 {
					message += "（警告：" + strings.Join(z.receiveWarnings, "；") + "）"
				}
				z.notify(message, false)
				z.session.zmodem = nil
				z.session.detector.Reset()
				return true, nil
			}
		}
		for data := z.receiver.DrainFile(); len(data) > 0; data = z.receiver.DrainFile() {
			progress = true
			localProgress = true
			if z.receiveFile != nil {
				if _, err := z.receiveFile.Write(data); err != nil {
					return progress, err
				}
				z.received += int64(len(data))
				z.progressReceive()
			}
		}
		if outgoing := z.receiver.DrainOutgoing(); len(outgoing) > 0 {
			progress = true
			localProgress = true
			if err := z.session.writeRemote(outgoing); err != nil {
				return progress, err
			}
		}
		if !localProgress {
			break
		}
	}
	return progress, nil
}

func (z *zmodemTransfer) senderConsume(data []byte) error {
	if z.sender == nil {
		return nil
	}
	if _, err := z.sender.FeedIncoming(data); err != nil {
		return err
	}
	return z.flushSender()
}

func (z *zmodemTransfer) flushSender() error {
	if z.sender == nil {
		return nil
	}
	for {
		localProgress := false
		if outgoing := z.sender.DrainOutgoing(); len(outgoing) > 0 {
			localProgress = true
			if err := z.session.writeRemote(outgoing); err != nil {
				return err
			}
		}
		for request := z.sender.PollFile(); request != nil; request = z.sender.PollFile() {
			localProgress = true
			if z.sendFile == nil {
				if err := z.openCurrentSendFile(); err != nil {
					return err
				}
			}
			chunk := make([]byte, request.Len)
			n, err := z.sendFile.ReadAt(chunk, request.Offset)
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			if n <= 0 {
				return io.ErrUnexpectedEOF
			}
			chunk = chunk[:n]
			if err := z.sender.FeedFile(chunk); err != nil {
				return err
			}
			z.progressSend(request.Offset + int64(n))
			if outgoing := z.sender.DrainOutgoing(); len(outgoing) > 0 {
				if err := z.session.writeRemote(outgoing); err != nil {
					return err
				}
			}
		}
		for event := z.sender.PollEvent(); event != ""; event = z.sender.PollEvent() {
			localProgress = true
			switch event {
			case zmodemproto.SenderFileComplete:
				current := z.currentSendFile()
				z.sendBytesDone = current.Size
				z.closeSendFile()
				z.notifyTransfer("completed", fmt.Sprintf("已发送 %s", current.Name), true, current.Name, current.Size, current.Size)
				z.notify(fmt.Sprintf("已发送 %s", current.Name), true)
				z.sendIndex++
				if z.sendIndex < len(z.sendFiles) {
					if err := z.startCurrentFile(); err != nil {
						return err
					}
				} else if err := z.sender.FinishSession(); err != nil {
					return err
				}
			case zmodemproto.SenderSessionComplete:
				z.cancelLocal()
				z.notify("ZMODEM 发送完成", false)
				z.session.zmodem = nil
				z.session.detector.Reset()
				return nil
			}
		}
		if !localProgress {
			break
		}
	}
	return nil
}

func (z *zmodemTransfer) startCurrentFile() error {
	file := z.currentSendFile()
	if file.Path == "" {
		return errors.New("ZMODEM send file not found")
	}
	if err := z.openCurrentSendFile(); err != nil {
		return err
	}
	z.sendBytesDone = 0
	name := encodeProtocolFilename(safeZmodemFilename(file.Name))
	if err := z.sender.StartFile(name, file.Size); err != nil {
		return err
	}
	z.notifyTransfer("running", fmt.Sprintf("正在发送 %s · 0%%", file.Name), true, file.Name, 0, file.Size)
	z.notify(fmt.Sprintf("正在发送 %s · 0%%", file.Name), true)
	return nil
}

func (z *zmodemTransfer) currentSendFile() ZmodemSendFile {
	if z.sendIndex < 0 || z.sendIndex >= len(z.sendFiles) {
		return ZmodemSendFile{}
	}
	return z.sendFiles[z.sendIndex]
}

func (z *zmodemTransfer) openCurrentSendFile() error {
	if z.sendFile != nil {
		return nil
	}
	file := z.currentSendFile()
	handle, err := os.Open(file.Path)
	if err != nil {
		return err
	}
	z.sendFile = handle
	return nil
}

func (z *zmodemTransfer) closeSendFile() {
	if z.sendFile != nil {
		_ = z.sendFile.Close()
		z.sendFile = nil
	}
}

func (z *zmodemTransfer) cancelLocal() {
	if z.receiveFile != nil {
		_ = z.receiveFile.Cancel()
		z.receiveFile = nil
	}
	z.closeSendFile()
	z.receiver = nil
	z.sender = nil
	z.receiveInput = nil
}

func (z *zmodemTransfer) fail(err error) {
	active := z.hasActiveTransfer()
	name, done, total, direction := z.currentTransferSnapshot()
	z.cancelLocal()
	z.session.extendDiscardWindow()
	if active && name != "" {
		z.notifyTransferWithDirection("failed", err.Error(), false, name, done, total, direction)
	}
	z.notify(err.Error(), false)
	z.session.zmodem = nil
	z.session.detector.Reset()
	_ = z.session.writeRemoteCancel()
}

func (z *zmodemTransfer) notify(message string, active bool) {
	if z.handler != nil {
		z.handler.NotifyZmodemStatus(ZmodemStatus{
			SessionID:    z.session.id,
			ConnectionID: z.session.connectionID,
			Mode:         "zmodem",
			Message:      message,
			Active:       active,
		})
	}
}

func (z *zmodemTransfer) notifyTransfer(status, message string, active bool, name string, done, total int64) {
	z.notifyTransferWithDirection(status, message, active, name, done, total, z.direction())
}

func (z *zmodemTransfer) notifyTransferWithDirection(status, message string, active bool, name string, done, total int64, direction string) {
	if z.handler != nil {
		z.handler.NotifyZmodemStatus(ZmodemStatus{
			SessionID:    z.session.id,
			ConnectionID: z.session.connectionID,
			Mode:         "zmodem",
			Direction:    direction,
			Name:         name,
			Status:       status,
			BytesDone:    done,
			TotalBytes:   total,
			Message:      message,
			Active:       active,
		})
	}
}

func (z *zmodemTransfer) progressReceive() {
	if time.Since(z.lastStatusAt) < 250*time.Millisecond {
		return
	}
	z.lastStatusAt = time.Now()
	z.notifyTransfer("running", fmt.Sprintf("正在接收 %s · %s", z.receiveName, formatZmodemProgress(z.received, z.receiveSize)), true, z.receiveName, z.received, z.receiveSize)
	z.notify(fmt.Sprintf("正在接收 %s · %s", z.receiveName, formatZmodemProgress(z.received, z.receiveSize)), true)
}

func (z *zmodemTransfer) progressSend(done int64) {
	if time.Since(z.lastStatusAt) < 250*time.Millisecond {
		return
	}
	z.lastStatusAt = time.Now()
	z.sendBytesDone = done
	file := z.currentSendFile()
	z.notifyTransfer("running", fmt.Sprintf("正在发送 %s · %s", file.Name, formatZmodemProgress(done, file.Size)), true, file.Name, done, file.Size)
	z.notify(fmt.Sprintf("正在发送 %s · %s", file.Name, formatZmodemProgress(done, file.Size)), true)
}

func (z *zmodemTransfer) currentTransferSnapshot() (name string, done int64, total int64, direction string) {
	if z.mode == zmodemproto.ModeReceive {
		return z.receiveName, z.received, z.receiveSize, "download"
	}
	file := z.currentSendFile()
	if file.Path == "" {
		return "", 0, 0, "upload"
	}
	done = z.sendBytesDone
	if done > file.Size {
		done = file.Size
	}
	return file.Name, done, file.Size, "upload"
}

func (z *zmodemTransfer) direction() string {
	if z.mode == zmodemproto.ModeReceive {
		return "download"
	}
	return "upload"
}

func (z *zmodemTransfer) hasActiveTransfer() bool {
	if z.mode == zmodemproto.ModeReceive {
		return z.receiveFile != nil
	}
	return z.sendFile != nil
}

func safeZmodemFilename(value string) string {
	name := filepath.Base(strings.ReplaceAll(value, "\\", "/"))
	name = strings.ReplaceAll(name, "\x00", "")
	if name == "" || name == "." || name == ".." {
		return "transfer.bin"
	}
	return name
}

func encodeProtocolFilename(value string) string {
	return value
}

func decodeProtocolFilename(value string) string {
	return strings.ToValidUTF8(value, "")
}

func formatZmodemProgress(done, total int64) string {
	if total <= 0 {
		return formatZmodemSize(done)
	}
	percent := done * 100 / total
	if percent > 100 {
		percent = 100
	}
	return fmt.Sprintf("%d%% · %s / %s", percent, formatZmodemSize(done), formatZmodemSize(total))
}

func zmodemReceiveSizeWarning(name string, announced, actual int64) string {
	if announced > 0 && announced != actual {
		return fmt.Sprintf("%s 大小与发送端声明不一致：声明 %s，实际 %s",
			name, formatZmodemSize(announced), formatZmodemSize(actual))
	}
	return ""
}

func zmodemReceiveCompleteMessage(name, warning string) string {
	if warning != "" {
		return fmt.Sprintf("已接收 %s（警告：%s）", name, warning)
	}
	return fmt.Sprintf("已接收 %s", name)
}

func formatZmodemSize(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	if value < unit*unit {
		return fmt.Sprintf("%.1f KB", float64(value)/unit)
	}
	if value < unit*unit*unit {
		return fmt.Sprintf("%.1f MB", float64(value)/(unit*unit))
	}
	return fmt.Sprintf("%.1f GB", float64(value)/(unit*unit*unit))
}

func bytesRepeat(value byte, count int) []byte {
	result := make([]byte, count)
	for index := range result {
		result[index] = value
	}
	return result
}

func (s *terminalSession) shouldDiscardOutput() bool {
	return !s.zmodemDiscardUntil.IsZero() && time.Now().Before(s.zmodemDiscardUntil)
}

func (s *terminalSession) extendDiscardWindow() {
	s.zmodemDiscardUntil = time.Now().Add(zmodemCancelDrainWindow)
}
