package sftpclient

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nyaterminal/nyaterminal/desktop/internal/sshclient"
	"github.com/pkg/sftp"
)

type Service struct {
	ssh       *sshclient.Manager
	mu        sync.Mutex
	grants    map[string]localGrant
	transfers map[string]*transferJob
	slots     chan struct{}
	wg        sync.WaitGroup
}

func (s *Service) Close() {
	s.mu.Lock()
	jobs := make([]*transferJob, 0, len(s.transfers))
	for _, job := range s.transfers {
		jobs = append(jobs, job)
	}
	s.mu.Unlock()
	for _, job := range jobs {
		_ = s.CancelTransfer(job.value.ID)
	}
	s.wg.Wait()
}

type localGrant struct {
	Root      string
	ExpiresAt time.Time
}

type LocalLocation struct {
	Token string  `json:"token"`
	Path  string  `json:"path"`
	Items []Entry `json:"items"`
}

type Entry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	IsDir   bool      `json:"isDir"`
	ModTime time.Time `json:"modTime"`
}

func New(sshManager *sshclient.Manager) *Service {
	return &Service{
		ssh: sshManager, grants: make(map[string]localGrant),
		transfers: make(map[string]*transferJob), slots: make(chan struct{}, 3),
	}
}

func (s *Service) GrantLocalDirectory(root string) (LocalLocation, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return LocalLocation{}, err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return LocalLocation{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return LocalLocation{}, errors.New("selected local path is not a directory")
	}
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return LocalLocation{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	s.mu.Lock()
	now := time.Now()
	for existing, grant := range s.grants {
		if now.After(grant.ExpiresAt) {
			delete(s.grants, existing)
		}
	}
	s.grants[token] = localGrant{Root: resolved, ExpiresAt: now.Add(8 * time.Hour)}
	s.mu.Unlock()
	items, err := s.ListLocal(token, ".")
	if err != nil {
		return LocalLocation{}, err
	}
	return LocalLocation{Token: token, Path: resolved, Items: items}, nil
}

func (s *Service) ListLocal(token, relativePath string) ([]Entry, error) {
	target, err := s.resolveGrantedPath(token, relativePath)
	if err != nil {
		return nil, err
	}
	items, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	result := make([]Entry, 0, len(items))
	for _, item := range items {
		info, err := item.Info()
		if err != nil {
			return nil, err
		}
		itemRelative := filepath.ToSlash(filepath.Join(relativePath, item.Name()))
		result = append(result, Entry{
			Name: item.Name(), Path: itemRelative, Size: info.Size(),
			Mode: info.Mode().String(), IsDir: info.IsDir(), ModTime: info.ModTime(),
		})
	}
	return result, nil
}

func (s *Service) UploadGranted(
	ctx context.Context,
	connectionID, token, localRelativePath, remotePath string,
) error {
	localPath, err := s.resolveGrantedPath(token, localRelativePath)
	if err != nil {
		return err
	}
	return s.Upload(ctx, connectionID, localPath, remotePath)
}

func (s *Service) DownloadGranted(
	ctx context.Context,
	connectionID, remotePath, token, localRelativePath string,
) error {
	localPath, err := s.resolveGrantedPath(token, localRelativePath)
	if err != nil {
		return err
	}
	return s.Download(ctx, connectionID, remotePath, localPath)
}

func (s *Service) ListRemote(ctx context.Context, connectionID, remotePath string) ([]Entry, error) {
	client, sftpClient, err := s.connect(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	defer sftpClient.Close()
	remotePath, err = cleanRemote(remotePath)
	if err != nil {
		return nil, err
	}
	items, err := sftpClient.ReadDir(remotePath)
	if err != nil {
		return nil, err
	}
	result := make([]Entry, 0, len(items))
	for _, item := range items {
		result = append(result, Entry{
			Name: item.Name(), Path: path.Join(remotePath, item.Name()),
			Size: item.Size(), Mode: item.Mode().String(), IsDir: item.IsDir(),
			ModTime: item.ModTime(),
		})
	}
	return result, nil
}

func (s *Service) CreateRemoteDirectory(
	ctx context.Context, connectionID, remotePath string,
) error {
	remotePath, err := cleanRemote(remotePath)
	if err != nil {
		return err
	}
	client, sftpClient, err := s.connect(ctx, connectionID)
	if err != nil {
		return err
	}
	defer client.Close()
	defer sftpClient.Close()
	return sftpClient.Mkdir(remotePath)
}

func (s *Service) RenameRemote(
	ctx context.Context, connectionID, oldPath, newPath string,
) error {
	oldPath, err := cleanRemote(oldPath)
	if err != nil {
		return err
	}
	newPath, err = cleanRemote(newPath)
	if err != nil {
		return err
	}
	client, sftpClient, err := s.connect(ctx, connectionID)
	if err != nil {
		return err
	}
	defer client.Close()
	defer sftpClient.Close()
	return sftpClient.Rename(oldPath, newPath)
}

func (s *Service) DeleteRemote(
	ctx context.Context, connectionID, remotePath string, directory bool,
) error {
	remotePath, err := cleanRemote(remotePath)
	if err != nil {
		return err
	}
	if remotePath == "." || remotePath == "/" {
		return errors.New("refusing to delete remote root")
	}
	client, sftpClient, err := s.connect(ctx, connectionID)
	if err != nil {
		return err
	}
	defer client.Close()
	defer sftpClient.Close()
	if directory {
		var entries []string
		walker := sftpClient.Walk(remotePath)
		for walker.Step() {
			if walker.Err() != nil {
				return walker.Err()
			}
			entries = append(entries, walker.Path())
		}
		for index := len(entries) - 1; index >= 0; index-- {
			info, err := sftpClient.Lstat(entries[index])
			if err != nil {
				return err
			}
			if info.IsDir() {
				if err := sftpClient.RemoveDirectory(entries[index]); err != nil {
					return err
				}
			} else if err := sftpClient.Remove(entries[index]); err != nil {
				return err
			}
		}
		return nil
	}
	return sftpClient.Remove(remotePath)
}

func (s *Service) Upload(ctx context.Context, connectionID, localPath, remotePath string) error {
	localPath, err := validateLocalFile(localPath)
	if err != nil {
		return err
	}
	remotePath, err = cleanRemote(remotePath)
	if err != nil {
		return err
	}
	source, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer source.Close()
	client, sftpClient, err := s.connect(ctx, connectionID)
	if err != nil {
		return err
	}
	defer client.Close()
	defer sftpClient.Close()
	target, err := sftpClient.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	defer target.Close()
	_, err = copyContext(ctx, target, source)
	return err
}

func (s *Service) Download(ctx context.Context, connectionID, remotePath, localPath string) error {
	remotePath, err := cleanRemote(remotePath)
	if err != nil {
		return err
	}
	localPath, err = validateLocalDestination(localPath)
	if err != nil {
		return err
	}
	client, sftpClient, err := s.connect(ctx, connectionID)
	if err != nil {
		return err
	}
	defer client.Close()
	defer sftpClient.Close()
	source, err := sftpClient.Open(remotePath)
	if err != nil {
		return err
	}
	defer source.Close()
	tempPath := localPath + ".nyapart"
	target, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := copyContext(ctx, target, source)
	closeErr := target.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tempPath, localPath)
}

func (s *Service) connect(ctx context.Context, connectionID string) (io.Closer, *sftp.Client, error) {
	if borrowed := s.ssh.BorrowConnection(connectionID); borrowed != nil {
		sftpClient, err := sftp.NewClient(borrowed, sftp.MaxPacket(1<<15))
		if err == nil {
			return nopCloser{}, sftpClient, nil
		}
	}
	client, err := s.ssh.DialConnection(ctx, connectionID, nil)
	if err != nil {
		return nil, nil, err
	}
	sftpClient, err := sftp.NewClient(client, sftp.MaxPacket(1<<15))
	if err != nil {
		client.Close()
		return nil, nil, err
	}
	return client, sftpClient, nil
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

func (s *Service) resolveGrantedPath(token, relativePath string) (string, error) {
	s.mu.Lock()
	grant, ok := s.grants[token]
	if ok && time.Now().After(grant.ExpiresAt) {
		delete(s.grants, token)
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		return "", errors.New("local directory permission has expired")
	}
	if filepath.IsAbs(relativePath) || strings.ContainsRune(relativePath, '\x00') {
		return "", errors.New("invalid local path")
	}
	candidate := filepath.Join(grant.Root, filepath.Clean(relativePath))
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	checkedPath := absolute
	if _, err := os.Lstat(absolute); errors.Is(err, os.ErrNotExist) {
		checkedPath = filepath.Dir(absolute)
	} else if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(checkedPath)
	if err != nil {
		return "", err
	}
	if checkedPath != absolute {
		absolute = filepath.Join(resolved, filepath.Base(absolute))
	} else {
		absolute = resolved
	}
	relative, err := filepath.Rel(grant.Root, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("local path escapes the granted directory")
	}
	return absolute, nil
}

func cleanRemote(value string) (string, error) {
	if strings.ContainsRune(value, '\x00') {
		return "", errors.New("invalid remote path")
	}
	if value == "" {
		return ".", nil
	}
	cleaned := path.Clean(value)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("remote path traversal is not allowed")
	}
	return cleaned, nil
}

func validateLocalFile(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("local source must be a regular file")
	}
	return absolute, nil
}

func validateLocalDestination(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(absolute)
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return "", errors.New("destination directory does not exist")
	}
	return absolute, nil
}

func copyContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
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
