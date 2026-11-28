package conversation

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
)

const textBindingDirectory = "native-text-bindings"

func (s *Store) textBindingPath(id string) (string, error) {
	if _, err := hex.DecodeString(id); len(id) != 64 || err != nil {
		return "", status_error.ChatLogRecordInvalid
	}
	return filepath.Join(s.root, textBindingDirectory, id+".json"), nil
}

// ReadTextBinding returns the last retained private binding without creating a public Chat.
func (s *Store) ReadTextBinding(ctx context.Context, id string) (*agent.NativeBinding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.textBindingPath(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, status_error.ChatLogError
	}
	var binding agent.NativeBinding
	if json.Unmarshal(data, &binding) != nil || !validTextBinding(binding) {
		return nil, status_error.ChatLogCorrupted
	}
	return &binding, nil
}

// WriteTextBinding publishes only the current native address and compatibility evidence.
func (s *Store) WriteTextBinding(ctx context.Context, id string, binding agent.NativeBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.textBindingPath(id)
	if err != nil {
		return err
	}
	if !validTextBinding(binding) {
		return status_error.ChatLogRecordInvalid
	}
	data, err := json.Marshal(binding)
	if err != nil {
		return status_error.ChatLogRecordInvalid
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return status_error.ChatLogError
	}
	file, err := os.CreateTemp(dir, ".binding-*")
	if err != nil {
		return status_error.ChatLogError
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return status_error.ChatLogError
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if os.Rename(file.Name(), path) != nil || syncDirectory(dir) != nil || syncDirectory(s.root) != nil {
		return status_error.ChatLogError
	}
	return nil
}

func validTextBinding(binding agent.NativeBinding) bool {
	return !binding.Cleared && binding.Handoff == nil && json.Valid(binding.Configuration) &&
		binding.Session.Provider != "" && binding.Session.Subject != "" && binding.Session.SessionID != ""
}
