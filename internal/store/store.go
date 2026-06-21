package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nyaterminal/nyaterminal/desktop/internal/model"
	"github.com/nyaterminal/nyaterminal/desktop/internal/vault"
)

const (
	TypeGroup          = "group"
	TypeConnection     = "connection"
	TypeCredential     = "credential"
	TypeTag            = "tag"
	TypeSettings       = "settings"
	TypeHistory        = "command_history"
	TypeHostTrust      = "host_trust"
	TypeAccountSession = "account_session"
	TypeSyncProfile    = "sync_profile"
	TypeSyncState      = "sync_state"
	TypePairing        = "sync_pairing"
	TypeDeletion       = "sync_deletion"
)

type Store struct {
	vault *vault.Vault
}

type HostTrust struct {
	ID          string    `json:"id"`
	HostPort    string    `json:"hostPort"`
	Algorithm   string    `json:"algorithm"`
	Fingerprint string    `json:"fingerprint"`
	PublicKey   []byte    `json:"publicKey"`
	AcceptedAt  time.Time `json:"acceptedAt"`
}

type Deletion struct {
	ID         string    `json:"id"`
	EntityID   string    `json:"entityId"`
	EntityType string    `json:"entityType"`
	DeletedAt  time.Time `json:"deletedAt"`
}

func New(v *vault.Vault) *Store {
	return &Store{vault: v}
}

func (s *Store) ListGroups(ctx context.Context) ([]model.Group, error) {
	values, err := s.vault.List(ctx, TypeGroup, func() any { return &model.Group{} })
	return valuesAs[model.Group](values), err
}

func (s *Store) PutGroup(ctx context.Context, value model.Group) (model.Group, error) {
	now := time.Now().UTC()
	if value.ID == "" {
		value.ID = uuid.NewString()
		value.CreatedAt = now
	}
	value.Name = strings.TrimSpace(value.Name)
	if value.Name == "" || len(value.Name) > 128 || value.ParentID == value.ID {
		return model.Group{}, errors.New("invalid group")
	}
	if value.ParentID != "" {
		groups, err := s.ListGroups(ctx)
		if err != nil {
			return model.Group{}, err
		}
		if introducesCycle(groups, value.ID, value.ParentID) {
			return model.Group{}, errors.New("group hierarchy contains a cycle")
		}
	}
	value.UpdatedAt = now
	return value, s.vault.Put(ctx, TypeGroup, value.ID, value)
}

func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	groups, err := s.ListGroups(ctx)
	if err != nil {
		return err
	}
	for _, group := range groups {
		if group.ParentID == id {
			return errors.New("group contains child groups")
		}
	}
	connections, err := s.ListConnections(ctx)
	if err != nil {
		return err
	}
	for _, connection := range connections {
		if connection.GroupID == id {
			return errors.New("group contains connections")
		}
	}
	return s.Delete(ctx, id)
}

func (s *Store) ListTags(ctx context.Context) ([]model.Tag, error) {
	values, err := s.vault.List(ctx, TypeTag, func() any { return &model.Tag{} })
	return valuesAs[model.Tag](values), err
}

func (s *Store) PutTag(ctx context.Context, value model.Tag) (model.Tag, error) {
	now := time.Now().UTC()
	if value.ID == "" {
		value.ID = uuid.NewString()
		value.CreatedAt = now
	}
	value.Name = strings.TrimSpace(value.Name)
	if value.Name == "" || len(value.Name) > 64 {
		return model.Tag{}, errors.New("invalid tag")
	}
	if !regexp.MustCompile(`^#[0-9a-fA-F]{6}$`).MatchString(value.Color) {
		return model.Tag{}, errors.New("tag color must use #RRGGBB")
	}
	value.UpdatedAt = now
	return value, s.vault.Put(ctx, TypeTag, value.ID, value)
}

func (s *Store) DeleteTag(ctx context.Context, id string) error {
	connections, err := s.ListConnections(ctx)
	if err != nil {
		return err
	}
	for _, connection := range connections {
		next := connection.Tags[:0]
		for _, tagID := range connection.Tags {
			if tagID != id {
				next = append(next, tagID)
			}
		}
		if len(next) != len(connection.Tags) {
			connection.Tags = append([]string(nil), next...)
			if _, err := s.PutConnection(ctx, connection); err != nil {
				return err
			}
		}
	}
	return s.Delete(ctx, id)
}

func (s *Store) ListConnections(ctx context.Context) ([]model.Connection, error) {
	values, err := s.vault.List(ctx, TypeConnection, func() any { return &model.Connection{} })
	result := valuesAs[model.Connection](values)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].SortOrder != result[j].SortOrder {
			return result[i].SortOrder < result[j].SortOrder
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result, err
}

func (s *Store) GetConnection(ctx context.Context, id string) (model.Connection, error) {
	var value model.Connection
	err := s.vault.Get(ctx, TypeConnection, id, &value)
	return value, err
}

func (s *Store) PutConnection(ctx context.Context, value model.Connection) (model.Connection, error) {
	now := time.Now().UTC()
	if value.ID == "" {
		value.ID = uuid.NewString()
		value.CreatedAt = now
		connections, err := s.ListConnections(ctx)
		if err != nil {
			return model.Connection{}, err
		}
		maxOrder := -1
		for _, existing := range connections {
			if existing.GroupID == value.GroupID && existing.SortOrder > maxOrder {
				maxOrder = existing.SortOrder
			}
		}
		value.SortOrder = maxOrder + 1
	}
	value.Name = strings.TrimSpace(value.Name)
	value.Remark = strings.TrimSpace(value.Remark)
	value.Host = strings.TrimSpace(value.Host)
	value.Username = strings.TrimSpace(value.Username)
	if value.Name == "" || value.Host == "" || value.Username == "" ||
		value.Port < 1 || value.Port > 65535 {
		return model.Connection{}, errors.New("invalid connection")
	}
	if len(value.Remark) > 2000 {
		return model.Connection{}, errors.New("remark is too long")
	}
	if value.Encoding == "" {
		value.Encoding = "utf-8"
	}
	if value.KeepAliveSeconds < 0 || value.KeepAliveSeconds > 3600 {
		return model.Connection{}, errors.New("invalid keepalive interval")
	}
	if value.ConnectTimeoutSec <= 0 {
		value.ConnectTimeoutSec = 15
	}
	value.UpdatedAt = now
	return value, s.vault.Put(ctx, TypeConnection, value.ID, value)
}

func (s *Store) DeleteConnection(ctx context.Context, id string) error {
	history, err := s.vault.List(ctx, TypeHistory, func() any { return &model.CommandHistory{} })
	if err != nil {
		return err
	}
	for _, value := range valuesAs[model.CommandHistory](history) {
		if value.ConnectionID == id {
			if err := s.Delete(ctx, value.ID); err != nil {
				return err
			}
		}
	}
	return s.Delete(ctx, id)
}

func (s *Store) GetCredential(ctx context.Context, id string) (model.Credential, error) {
	var value model.Credential
	err := s.vault.Get(ctx, TypeCredential, id, &value)
	return value, err
}

func (s *Store) ListCredentials(ctx context.Context) ([]model.Credential, error) {
	values, err := s.vault.List(ctx, TypeCredential, func() any { return &model.Credential{} })
	return valuesAs[model.Credential](values), err
}

func (s *Store) PutCredential(ctx context.Context, value model.Credential) (model.Credential, error) {
	now := time.Now().UTC()
	if value.ID == "" {
		value.ID = uuid.NewString()
		value.CreatedAt = now
	}
	value.Name = strings.TrimSpace(value.Name)
	if value.Name == "" {
		return model.Credential{}, errors.New("credential name is required")
	}
	switch value.Type {
	case "password":
		if value.Password == "" {
			return model.Credential{}, errors.New("password is required")
		}
	case "private_key":
		if value.PrivateKeyPEM == "" {
			return model.Credential{}, errors.New("private key is required")
		}
	case "agent", "interactive":
	default:
		return model.Credential{}, errors.New("unsupported credential type")
	}
	value.UpdatedAt = now
	return value, s.vault.Put(ctx, TypeCredential, value.ID, value)
}

func (s *Store) Delete(ctx context.Context, id string) error {
	recordType, err := s.vault.RecordType(ctx, id)
	if err != nil {
		return err
	}
	if !synchronizableType(recordType) {
		return s.vault.Delete(ctx, id)
	}
	deletion := Deletion{
		ID: "deletion:" + id, EntityID: id, EntityType: recordType,
		DeletedAt: time.Now().UTC(),
	}
	return s.vault.DeleteAndPut(ctx, id, TypeDeletion, deletion.ID, deletion)
}

func (s *Store) ListDeletions(ctx context.Context) ([]Deletion, error) {
	values, err := s.vault.List(ctx, TypeDeletion, func() any { return &Deletion{} })
	return valuesAs[Deletion](values), err
}

func (s *Store) RemoveDeletion(ctx context.Context, id string) error {
	return s.vault.Delete(ctx, id)
}

func (s *Store) Settings(ctx context.Context) (model.Settings, error) {
	var value model.Settings
	err := s.vault.Get(ctx, TypeSettings, "settings", &value)
	if errors.Is(err, sql.ErrNoRows) {
		value = model.DefaultSettings()
		err = s.vault.Put(ctx, TypeSettings, "settings", value)
	} else if err == nil {
		value = model.NormalizeSettings(value)
	}
	return value, err
}

func (s *Store) PutSettings(ctx context.Context, value model.Settings) error {
	value = model.NormalizeSettings(value)
	if value.FontSize < 9 || value.FontSize > 40 ||
		value.LockAfterMinutes < 0 || value.LockAfterMinutes > 24*60 {
		return errors.New("invalid settings")
	}
	for _, rule := range value.SensitiveCommandRules {
		if len(rule) > 512 {
			return errors.New("sensitive command rule is too long")
		}
		if _, err := regexp.Compile(rule); err != nil {
			return errors.New("invalid sensitive command rule")
		}
	}
	return s.vault.Put(ctx, TypeSettings, "settings", value)
}

func (s *Store) AddCommand(ctx context.Context, connectionID, command string, private bool) error {
	connection, err := s.GetConnection(ctx, connectionID)
	if err != nil {
		return err
	}
	if !connection.CommandHistory {
		return nil
	}
	if strings.HasPrefix(command, " ") {
		return nil
	}
	command = strings.TrimSpace(command)
	if private || command == "" || len(command) > 16*1024 {
		return nil
	}
	settings, err := s.Settings(ctx)
	if err != nil {
		return err
	}
	for _, rule := range settings.SensitiveCommandRules {
		matcher, err := regexp.Compile(rule)
		if err == nil && matcher.MatchString(command) {
			return nil
		}
	}
	now := time.Now().UTC()
	if err := s.upsertCommandHistory(ctx, commandHistoryID(connectionID, command), connectionID, command, now); err != nil {
		return err
	}
	return s.upsertCommandHistory(ctx, commandHistoryID("", command), "", command, now)
}

func (s *Store) SuggestCommands(ctx context.Context, connectionID, prefix string, limit int) ([]model.CommandHistory, error) {
	connection, err := s.GetConnection(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	if !connection.CommandHistory {
		return nil, nil
	}
	values, err := s.vault.List(ctx, TypeHistory, func() any { return &model.CommandHistory{} })
	if err != nil {
		return nil, err
	}
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	resultByCommand := make(map[string]model.CommandHistory)
	for _, value := range valuesAs[model.CommandHistory](values) {
		if (value.ConnectionID == connectionID || value.ConnectionID == "") &&
			strings.HasPrefix(strings.ToLower(value.Command), prefix) {
			existing, ok := resultByCommand[value.Command]
			if !ok || betterHistoryCandidate(value, existing, connectionID) {
				resultByCommand[value.Command] = value
			}
		}
	}
	result := make([]model.CommandHistory, 0, len(resultByCommand))
	for _, value := range resultByCommand {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].UseCount != result[j].UseCount {
			return result[i].UseCount > result[j].UseCount
		}
		return result[i].LastUsedAt.After(result[j].LastUsedAt)
	})
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *Store) upsertCommandHistory(
	ctx context.Context, id, connectionID, command string, now time.Time,
) error {
	var history model.CommandHistory
	err := s.vault.Get(ctx, TypeHistory, id, &history)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if history.ID == "" {
		history = model.CommandHistory{
			ID: id, ConnectionID: connectionID, Command: command,
		}
	}
	history.UseCount++
	history.LastUsedAt = now
	return s.vault.Put(ctx, TypeHistory, id, history)
}

func commandHistoryID(connectionID, command string) string {
	return connectionID + ":" + uuid.NewSHA1(uuid.NameSpaceURL, []byte(command)).String()
}

func betterHistoryCandidate(candidate, existing model.CommandHistory, connectionID string) bool {
	if candidate.UseCount != existing.UseCount {
		return candidate.UseCount > existing.UseCount
	}
	if candidate.LastUsedAt.Equal(existing.LastUsedAt) {
		if candidate.ConnectionID == connectionID && existing.ConnectionID == "" {
			return true
		}
		return false
	}
	return candidate.LastUsedAt.After(existing.LastUsedAt)
}

func synchronizableType(recordType string) bool {
	switch recordType {
	case TypeGroup, TypeConnection, TypeCredential, TypeTag, TypeHistory:
		return true
	default:
		return false
	}
}

func (s *Store) GetHostTrust(ctx context.Context, hostPort string) (HostTrust, error) {
	var trust HostTrust
	err := s.vault.Get(ctx, TypeHostTrust, hostTrustID(hostPort), &trust)
	return trust, err
}

func (s *Store) PutHostTrust(ctx context.Context, trust HostTrust) error {
	trust.ID = hostTrustID(trust.HostPort)
	trust.AcceptedAt = time.Now().UTC()
	return s.vault.Put(ctx, TypeHostTrust, trust.ID, trust)
}

func hostTrustID(hostPort string) string {
	return "host:" + uuid.NewSHA1(uuid.NameSpaceDNS, []byte(strings.ToLower(hostPort))).String()
}

func introducesCycle(groups []model.Group, id, parentID string) bool {
	if id == "" {
		return false
	}
	parents := make(map[string]string, len(groups))
	for _, group := range groups {
		parents[group.ID] = group.ParentID
	}
	parents[id] = parentID
	seen := map[string]bool{id: true}
	for current := parentID; current != ""; current = parents[current] {
		if seen[current] {
			return true
		}
		seen[current] = true
	}
	return false
}

func valuesAs[T any](values []any) []T {
	result := make([]T, 0, len(values))
	for _, value := range values {
		result = append(result, *(value.(*T)))
	}
	return result
}
