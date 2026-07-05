package sftpclient

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Transfer struct {
	ID           string    `json:"id"`
	ConnectionID string    `json:"connectionId"`
	SessionID    string    `json:"sessionId,omitempty"`
	Mode         string    `json:"mode"`
	Name         string    `json:"name"`
	Direction    string    `json:"direction"`
	Status       string    `json:"status"`
	BytesDone    int64     `json:"bytesDone"`
	TotalBytes   int64     `json:"totalBytes"`
	Error        string    `json:"error,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type transferSpec struct {
	connectionID string
	direction    string
	localPath    string
	remotePath   string
	overwrite    bool
}

type transferJob struct {
	mu     sync.Mutex
	value  Transfer
	spec   transferSpec
	cancel context.CancelFunc
}

func (s *Service) StartUpload(
	connectionID, localPath, remotePath string, overwrite bool,
) (Transfer, error) {
	localPath, err := validateLocalFile(localPath)
	if err != nil {
		return Transfer{}, err
	}
	remotePath, err = cleanRemote(remotePath)
	if err != nil {
		return Transfer{}, err
	}
	info, err := os.Stat(localPath)
	if err != nil {
		return Transfer{}, err
	}
	return s.startTransfer(transferSpec{
		connectionID: connectionID, direction: "upload",
		localPath: localPath, remotePath: remotePath, overwrite: overwrite,
	}, path.Base(remotePath), info.Size()), nil
}

func (s *Service) StartDownload(
	connectionID, remotePath, localPath string, overwrite bool,
) (Transfer, error) {
	remotePath, err := cleanRemote(remotePath)
	if err != nil {
		return Transfer{}, err
	}
	localPath, err = validateLocalDestination(localPath)
	if err != nil {
		return Transfer{}, err
	}
	return s.startTransfer(transferSpec{
		connectionID: connectionID, direction: "download",
		localPath: localPath, remotePath: remotePath, overwrite: overwrite,
	}, path.Base(remotePath), 0), nil
}

func (s *Service) StartUploadGranted(
	connectionID, token, localRelativePath, remotePath string, overwrite bool,
) (Transfer, error) {
	localPath, err := s.resolveGrantedPath(token, localRelativePath)
	if err != nil {
		return Transfer{}, err
	}
	return s.StartUpload(connectionID, localPath, remotePath, overwrite)
}

func (s *Service) StartDownloadGranted(
	connectionID, remotePath, token, localRelativePath string, overwrite bool,
) (Transfer, error) {
	localPath, err := s.resolveGrantedPath(token, localRelativePath)
	if err != nil {
		return Transfer{}, err
	}
	return s.StartDownload(connectionID, remotePath, localPath, overwrite)
}

func (s *Service) startTransfer(spec transferSpec, name string, total int64) Transfer {
	now := time.Now().UTC()
	job := &transferJob{
		spec: spec,
		value: Transfer{
			ID: uuid.NewString(), ConnectionID: spec.connectionID, Name: name,
			Mode: "sftp", Direction: spec.direction, Status: "queued", TotalBytes: total,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	s.mu.Lock()
	s.transfers[job.value.ID] = job
	s.mu.Unlock()
	s.launchTransfer(job)
	return job.snapshot()
}

func (s *Service) launchTransfer(job *transferJob) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runTransfer(job)
	}()
}

func (s *Service) ListTransfers() []Transfer {
	s.mu.Lock()
	jobs := make([]*transferJob, 0, len(s.transfers))
	for _, job := range s.transfers {
		jobs = append(jobs, job)
	}
	s.mu.Unlock()
	result := make([]Transfer, 0, len(jobs))
	for _, job := range jobs {
		result = append(result, job.snapshot())
	}
	return result
}

func (s *Service) PauseTransfer(id string) error {
	job, err := s.transfer(id)
	if err != nil {
		return err
	}
	job.mu.Lock()
	switch job.value.Status {
	case "queued", "running":
		job.value.Status = "paused"
		job.value.UpdatedAt = time.Now().UTC()
		if job.cancel != nil {
			job.cancel()
		}
	default:
		job.mu.Unlock()
		return errors.New("transfer cannot be paused")
	}
	job.mu.Unlock()
	return nil
}

func (s *Service) ResumeTransfer(id string) error {
	job, err := s.transfer(id)
	if err != nil {
		return err
	}
	job.mu.Lock()
	if job.value.Status != "paused" && job.value.Status != "failed" {
		job.mu.Unlock()
		return errors.New("transfer cannot be resumed")
	}
	job.value.Status = "queued"
	job.value.Error = ""
	job.value.UpdatedAt = time.Now().UTC()
	job.mu.Unlock()
	s.launchTransfer(job)
	return nil
}

func (s *Service) CancelTransfer(id string) error {
	job, err := s.transfer(id)
	if err != nil {
		return err
	}
	job.mu.Lock()
	switch job.value.Status {
	case "completed", "cancelled":
		job.mu.Unlock()
		return nil
	default:
		job.value.Status = "cancelled"
		job.value.UpdatedAt = time.Now().UTC()
		if job.cancel != nil {
			job.cancel()
		}
	}
	job.mu.Unlock()
	return nil
}

func (s *Service) transfer(id string) (*transferJob, error) {
	s.mu.Lock()
	job := s.transfers[id]
	s.mu.Unlock()
	if job == nil {
		return nil, errors.New("transfer not found")
	}
	return job, nil
}

func (s *Service) runTransfer(job *transferJob) {
	s.slots <- struct{}{}
	defer func() { <-s.slots }()

	job.mu.Lock()
	if job.value.Status != "queued" {
		job.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job.cancel = cancel
	job.value.Status = "running"
	job.value.UpdatedAt = time.Now().UTC()
	job.mu.Unlock()

	var err error
	if job.spec.direction == "upload" {
		err = s.runUpload(ctx, job)
	} else {
		err = s.runDownload(ctx, job)
	}
	cancel()
	job.mu.Lock()
	job.cancel = nil
	cancelled := job.value.Status == "cancelled"
	switch {
	case job.value.Status == "paused" || job.value.Status == "cancelled":
	case err != nil:
		job.value.Status = "failed"
		job.value.Error = err.Error()
	default:
		job.value.Status = "completed"
		job.value.BytesDone = job.value.TotalBytes
	}
	job.value.UpdatedAt = time.Now().UTC()
	job.mu.Unlock()
	if cancelled {
		s.cleanupTransfer(job.spec)
	}
}

func (s *Service) cleanupTransfer(spec transferSpec) {
	if spec.direction == "download" {
		_ = os.Remove(spec.localPath + ".nyapart")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, remote, err := s.connect(ctx, spec.connectionID)
	if err != nil {
		return
	}
	defer client.Close()
	defer remote.Close()
	_ = remote.Remove(spec.remotePath + ".nyapart")
}

func (s *Service) runUpload(ctx context.Context, job *transferJob) error {
	// #nosec G304 -- Transfer upload reads an explicit local file path selected by the desktop user.
	source, err := os.Open(job.spec.localPath)
	if err != nil {
		return err
	}
	defer source.Close()
	client, remote, err := s.connect(ctx, job.spec.connectionID)
	if err != nil {
		return err
	}
	defer client.Close()
	defer remote.Close()
	tempPath := job.spec.remotePath + ".nyapart"
	var offset int64
	if info, statErr := remote.Stat(tempPath); statErr == nil {
		offset = info.Size()
		if offset > job.value.TotalBytes {
			offset = 0
		}
	}
	if !job.spec.overwrite {
		if _, statErr := remote.Stat(job.spec.remotePath); statErr == nil {
			return errors.New("remote destination already exists")
		}
	}
	target, err := remote.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE)
	if err != nil {
		return err
	}
	if offset == 0 {
		if err := target.Truncate(0); err != nil {
			_ = target.Close()
			return err
		}
	}
	if _, err := source.Seek(offset, io.SeekStart); err != nil {
		_ = target.Close()
		return err
	}
	if _, err := target.Seek(offset, io.SeekStart); err != nil {
		_ = target.Close()
		return err
	}
	job.setProgress(offset, job.value.TotalBytes)
	if _, err := copyTransfer(ctx, target, source, job); err != nil {
		_ = target.Close()
		return err
	}
	if err := target.Close(); err != nil {
		return err
	}
	if job.spec.overwrite {
		_ = remote.Remove(job.spec.remotePath)
	}
	return remote.Rename(tempPath, job.spec.remotePath)
}

func (s *Service) runDownload(ctx context.Context, job *transferJob) error {
	client, remote, err := s.connect(ctx, job.spec.connectionID)
	if err != nil {
		return err
	}
	defer client.Close()
	defer remote.Close()
	source, err := remote.Open(job.spec.remotePath)
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	if !job.spec.overwrite {
		if _, statErr := os.Stat(job.spec.localPath); statErr == nil {
			return errors.New("local destination already exists")
		}
	}
	tempPath := job.spec.localPath + ".nyapart"
	// #nosec G304 -- Transfer download writes to an explicit local destination selected by the desktop user.
	target, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	offset, err := target.Seek(0, io.SeekEnd)
	if err != nil {
		_ = target.Close()
		return err
	}
	if offset > info.Size() {
		if err := target.Truncate(0); err != nil {
			_ = target.Close()
			return err
		}
		offset = 0
	}
	if _, err := source.Seek(offset, io.SeekStart); err != nil {
		_ = target.Close()
		return err
	}
	job.setProgress(offset, info.Size())
	if _, err := copyTransfer(ctx, target, source, job); err != nil {
		_ = target.Close()
		return err
	}
	if err := target.Sync(); err != nil {
		_ = target.Close()
		return err
	}
	if err := target.Close(); err != nil {
		return err
	}
	if job.spec.overwrite {
		_ = os.Remove(job.spec.localPath)
	}
	return os.Rename(tempPath, job.spec.localPath)
}

func copyTransfer(
	ctx context.Context, destination io.Writer, source io.Reader, job *transferJob,
) (int64, error) {
	buffer := make([]byte, 128*1024)
	var written int64
	for {
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		default:
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			output, writeErr := destination.Write(buffer[:count])
			written += int64(output)
			job.addProgress(int64(output))
			if writeErr != nil {
				return written, writeErr
			}
			if output != count {
				return written, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}

func (job *transferJob) snapshot() Transfer {
	job.mu.Lock()
	defer job.mu.Unlock()
	return job.value
}

func (job *transferJob) setProgress(done, total int64) {
	job.mu.Lock()
	job.value.BytesDone = done
	job.value.TotalBytes = total
	job.value.UpdatedAt = time.Now().UTC()
	job.mu.Unlock()
}

func (job *transferJob) addProgress(value int64) {
	job.mu.Lock()
	job.value.BytesDone += value
	job.value.UpdatedAt = time.Now().UTC()
	job.mu.Unlock()
}
