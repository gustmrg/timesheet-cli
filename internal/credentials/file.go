package credentials

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

type fileData struct {
	Version int               `json:"version"`
	Origins map[string]Record `json:"origins"`
}

// FileStore stores credentials in a local file protected by operating-system
// file permissions. It is intended for explicitly configured headless systems.
type FileStore struct {
	path string
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (s *FileStore) Get(origin string) (Record, error) {
	account, err := NormalizeOrigin(origin)
	if err != nil {
		return Record{}, err
	}
	data, err := s.read()
	if err != nil {
		return Record{}, err
	}
	record, ok := data.Origins[account]
	if !ok {
		return Record{}, &Error{Kind: KindNotFound, Message: "no saved credentials"}
	}
	if record.Version != recordVersion || record.Username == "" || record.Password == "" {
		return Record{}, &Error{Kind: KindCorrupt, Message: "saved credentials use an unsupported or invalid format"}
	}
	return record, nil
}

func (s *FileStore) Set(origin string, value Record) error {
	account, err := NormalizeOrigin(origin)
	if err != nil {
		return err
	}
	if value.Username == "" || value.Password == "" {
		return &Error{Kind: KindStore, Message: "credentials must include a username and password"}
	}
	data, err := s.read()
	if IsKind(err, KindNotFound) {
		data = fileData{Version: recordVersion, Origins: make(map[string]Record)}
	} else if err != nil {
		return err
	}
	value.Version = recordVersion
	data.Origins[account] = value
	return s.write(data)
}

func (s *FileStore) Delete(origin string) error {
	account, err := NormalizeOrigin(origin)
	if err != nil {
		return err
	}
	data, err := s.read()
	if IsKind(err, KindNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, ok := data.Origins[account]; !ok {
		return nil
	}
	delete(data.Origins, account)
	if len(data.Origins) == 0 {
		if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return &Error{Kind: KindStore, Message: "remove credentials file"}
		}
		return nil
	}
	return s.write(data)
}

func (s *FileStore) read() (fileData, error) {
	body, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return fileData{}, &Error{Kind: KindNotFound, Message: "no saved credentials"}
	}
	if err != nil {
		return fileData{}, &Error{Kind: KindStore, Message: "read credentials file"}
	}
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(s.path)
		if statErr != nil {
			return fileData{}, &Error{Kind: KindStore, Message: "inspect credentials file"}
		}
		if info.Mode().Perm()&0o077 != 0 {
			return fileData{}, &Error{Kind: KindStore, Message: "credentials file permissions are too open; restrict the file to mode 0600"}
		}
	}
	var data fileData
	if err := json.Unmarshal(body, &data); err != nil || data.Version != recordVersion || data.Origins == nil {
		return fileData{}, &Error{Kind: KindCorrupt, Message: "saved credentials file is corrupt or unsupported"}
	}
	return data, nil
}

func (s *FileStore) write(data fileData) error {
	directory := filepath.Dir(s.path)
	info, err := os.Stat(directory)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return &Error{Kind: KindStore, Message: "create credentials directory"}
		}
		info, err = os.Stat(directory)
	}
	if err != nil || !info.IsDir() {
		return &Error{Kind: KindStore, Message: "inspect credentials directory"}
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return &Error{Kind: KindStore, Message: "credentials directory permissions are too open; restrict the directory to mode 0700"}
	}
	temporary, err := os.CreateTemp(directory, ".credentials-*.tmp")
	if err != nil {
		return &Error{Kind: KindStore, Message: "create temporary credentials file"}
	}
	name := temporary.Name()
	defer os.Remove(name)
	_ = temporary.Chmod(0o600)
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(data)
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		if runtime.GOOS == "windows" {
			_ = os.Remove(s.path)
		}
		err = os.Rename(name, s.path)
	}
	if err != nil {
		return &Error{Kind: KindStore, Message: "save credentials file"}
	}
	_ = os.Chmod(s.path, 0o600)
	return nil
}
