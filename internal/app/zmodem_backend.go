package app

import (
	"context"
	"os"
	"path/filepath"

	"github.com/nyaterminal/nyaterminal/desktop/internal/sshclient"
	"github.com/nyaterminal/nyaterminal/desktop/internal/zmodemstore"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type backendZmodemReceive struct {
	store *App
	id    string
}

func (a *App) BeginZmodemDownload(_ context.Context, _ string, name string, size int64) (sshclient.ZmodemReceiveFile, error) {
	if name == "" {
		name = "transfer.bin"
	}
	localPath, err := runtime.SaveFileDialog(a.context(), runtime.SaveDialogOptions{
		Title: "保存 ZMODEM 文件", DefaultFilename: filepath.Base(name),
	})
	if err != nil || localPath == "" {
		return nil, err
	}
	id, err := a.zmodem.Begin(localPath, size)
	if err != nil {
		return nil, err
	}
	return backendZmodemReceive{store: a, id: id}, nil
}

func (a *App) ChooseZmodemUpload(context.Context, string) ([]sshclient.ZmodemSendFile, error) {
	paths, err := runtime.OpenMultipleFilesDialog(a.context(), runtime.OpenDialogOptions{
		Title: "选择要通过 ZMODEM 发送的文件",
	})
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	files := make([]sshclient.ZmodemSendFile, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		files = append(files, sshclient.ZmodemSendFile{
			Path: path,
			Name: filepath.Base(path),
			Size: info.Size(),
		})
	}
	return files, nil
}

func (a *App) NotifyZmodemStatus(status sshclient.ZmodemStatus) {
	if a.zmodem != nil {
		a.zmodem.Record(zmodemstore.TransferUpdate{
			SessionID:    status.SessionID,
			ConnectionID: status.ConnectionID,
			Name:         status.Name,
			Direction:    status.Direction,
			Status:       status.Status,
			BytesDone:    status.BytesDone,
			TotalBytes:   status.TotalBytes,
			Error: func() string {
				if status.Status == "failed" {
					return status.Message
				}
				return ""
			}(),
		})
	}
	runtime.EventsEmit(a.context(), "zmodem:status", status)
}

func (r backendZmodemReceive) Write(data []byte) (int, error) {
	if err := r.store.zmodem.Write(r.id, data); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (r backendZmodemReceive) Finish() error {
	return r.store.zmodem.Finish(r.id)
}

func (r backendZmodemReceive) Cancel() error {
	return r.store.zmodem.Cancel(r.id)
}
