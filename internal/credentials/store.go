package credentials

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"

	keyring "github.com/zalando/go-keyring"
)

const (
	serviceName   = "timesheet-cli"
	recordVersion = 1
)

type Kind string

const (
	KindNotFound    Kind = "not_found"
	KindUnsupported Kind = "unsupported"
	KindUnavailable Kind = "unavailable"
	KindLocked      Kind = "locked"
	KindCorrupt     Kind = "corrupt"
	KindStore       Kind = "store"
)

type Error struct {
	Kind    Kind
	Message string
}

func (e *Error) Error() string { return e.Message }

func IsKind(err error, kind Kind) bool {
	var credentialErr *Error
	return errors.As(err, &credentialErr) && credentialErr.Kind == kind
}

type Record struct {
	Version  int    `json:"version"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type Store interface {
	Get(origin string) (Record, error)
	Set(origin string, value Record) error
	Delete(origin string) error
}

type backend interface {
	Get(service, account string) (string, error)
	Set(service, account, secret string) error
	Delete(service, account string) error
}

type systemBackend struct{}

func (systemBackend) Get(service, account string) (string, error) {
	return keyring.Get(service, account)
}
func (systemBackend) Set(service, account, secret string) error {
	return keyring.Set(service, account, secret)
}
func (systemBackend) Delete(service, account string) error {
	return keyring.Delete(service, account)
}

type KeyringStore struct {
	backend backend
}

func NewKeyringStore() *KeyringStore {
	return &KeyringStore{backend: systemBackend{}}
}

func newKeyringStore(backend backend) *KeyringStore {
	return &KeyringStore{backend: backend}
}

func (s *KeyringStore) Get(origin string) (Record, error) {
	account, err := NormalizeOrigin(origin)
	if err != nil {
		return Record{}, err
	}
	secret, err := s.backend.Get(serviceName, account)
	if err != nil {
		return Record{}, classifyBackend(err, "read credentials from the operating system vault")
	}
	var record Record
	if err := json.Unmarshal([]byte(secret), &record); err != nil {
		return Record{}, &Error{Kind: KindCorrupt, Message: "saved credentials are corrupt"}
	}
	if record.Version != recordVersion || record.Username == "" || record.Password == "" {
		return Record{}, &Error{Kind: KindCorrupt, Message: "saved credentials use an unsupported or invalid format"}
	}
	return record, nil
}

func (s *KeyringStore) Set(origin string, value Record) error {
	account, err := NormalizeOrigin(origin)
	if err != nil {
		return err
	}
	value.Version = recordVersion
	if value.Username == "" || value.Password == "" {
		return &Error{Kind: KindStore, Message: "credentials must include a username and password"}
	}
	secret, err := json.Marshal(value)
	if err != nil {
		return &Error{Kind: KindStore, Message: "encode credentials for the operating system vault"}
	}
	if err := s.backend.Set(serviceName, account, string(secret)); err != nil {
		return classifyBackend(err, "save credentials in the operating system vault")
	}
	return nil
}

func (s *KeyringStore) Delete(origin string) error {
	account, err := NormalizeOrigin(origin)
	if err != nil {
		return err
	}
	if err := s.backend.Delete(serviceName, account); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return classifyBackend(err, "delete credentials from the operating system vault")
	}
	return nil
}

func NormalizeOrigin(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return "", &Error{Kind: KindStore, Message: "invalid timesheet server URL for credential storage"}
	}
	scheme := strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	host := hostname
	if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	}
	return (&url.URL{Scheme: scheme, Host: host}).String(), nil
}

func classifyBackend(err error, operation string) error {
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return &Error{Kind: KindNotFound, Message: "no saved credentials"}
	case errors.Is(err, keyring.ErrUnsupportedPlatform):
		return &Error{Kind: KindUnsupported, Message: "the operating system credential vault is not supported"}
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "locked"), strings.Contains(message, "islocked"):
		return &Error{Kind: KindLocked, Message: "the operating system credential vault is locked"}
	case strings.Contains(message, "dbus"),
		strings.Contains(message, "secret service"),
		strings.Contains(message, "not available"),
		strings.Contains(message, "no such file"),
		strings.Contains(message, "cannot autolaunch"):
		return &Error{Kind: KindUnavailable, Message: "the operating system credential vault is unavailable"}
	default:
		return &Error{Kind: KindStore, Message: operation}
	}
}
