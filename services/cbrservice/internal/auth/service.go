// Package auth хранит пароль плеера (bcrypt) и выданный токен в auth.json.
package auth

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// data — содержимое auth.json.
type data struct {
	PasswordHash string    `json:"password_hash"`
	Token        string    `json:"token"`
	LastAuthTime time.Time `json:"last_auth_time"`
}

// Service — пароль и текущий токен. Безопасен для конкурентного использования.
type Service struct {
	path string

	mu   sync.RWMutex
	data data
}

// NewService создаёт сервис, хранящий данные в path. Данные не загружаются — см. Load.
func NewService(path string) *Service {
	return &Service{path: path}
}

// Path — путь к auth.json.
func (s *Service) Path() string { return s.path }

// Load читает auth.json.
func (s *Service) Load() error {
	content, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var d data
	if err := json.Unmarshal(content, &d); err != nil {
		return fmt.Errorf("parse %s: %w", s.path, err)
	}

	s.mu.Lock()
	s.data = d
	s.mu.Unlock()
	return nil
}

// save записывает данные; вызывающий держит s.mu.
func (s *Service) save() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o600)
}

// SetPassword сохраняет хэш нового пароля и сбрасывает выданный токен.
func (s *Service) SetPassword(password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = data{PasswordHash: string(hash)}
	return s.save()
}

// CheckPassword сравнивает пароль с сохранённым хэшем.
func (s *Service) CheckPassword(password string) bool {
	s.mu.RLock()
	hash := s.data.PasswordHash
	s.mu.RUnlock()
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// IssueToken выдаёт новый токен (предыдущий перестаёт действовать) и сохраняет его.
func (s *Service) IssueToken() (string, error) {
	token := uuid.NewString()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Token = token
	s.data.LastAuthTime = time.Now()
	if err := s.save(); err != nil {
		return "", fmt.Errorf("save token: %w", err)
	}
	return token, nil
}

// ValidToken проверяет токен. Пустой токен никогда не валиден.
func (s *Service) ValidToken(token string) bool {
	s.mu.RLock()
	current := s.data.Token
	s.mu.RUnlock()

	if token == "" || current == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(current)) == 1
}
