package gmtool

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"time"

	"dnf90.local/gateway/internal/platform/filex"
)

const rememberedSessionTTL = 30 * 24 * time.Hour

type rememberedSessionFile struct {
	Version  int                  `json:"version"`
	Identity string               `json:"identity"`
	Sessions map[string]time.Time `json:"sessions"`
}

func sessionDigest(token string) string {
	hash := sha256Sum(token)
	return hex.EncodeToString(hash[:])
}

func (s *Server) sessionIdentity() string {
	if s.cfg.LoginKeySHA256 != "" {
		return sessionDigest("key:" + s.cfg.LoginKeySHA256)
	}
	return sessionDigest("password:" + s.cfg.Password)
}

func (s *Server) loadRememberedSessions() error {
	s.remembered = make(map[string]time.Time)
	if s.cfg.SessionStorePath == "" {
		return nil
	}
	raw, err := os.ReadFile(s.cfg.SessionStorePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("remembered session store unavailable")
	}
	var saved rememberedSessionFile
	if len(raw) > 1<<20 || json.Unmarshal(raw, &saved) != nil || saved.Version != 1 {
		return errors.New("remembered session store invalid")
	}
	// Rotating the operator token invalidates prior remembered sessions.
	if saved.Identity != s.sessionIdentity() {
		return nil
	}
	for key, expires := range saved.Sessions {
		if len(key) == 64 && expires.After(time.Now()) {
			s.remembered[key] = expires
		}
	}
	return nil
}

// Caller holds s.mu. Only one authentication service owns this file.
func (s *Server) saveRememberedSessions() error {
	if s.cfg.SessionStorePath == "" {
		return errors.New("remembered session store is not configured")
	}
	raw, err := json.Marshal(rememberedSessionFile{1, s.sessionIdentity(), s.remembered})
	if err != nil {
		return err
	}
	return filex.AtomicWriteFile(s.cfg.SessionStorePath, raw, 0o600)
}

func (s *Server) startRememberedSession() (string, time.Time, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	expires := time.Now().UTC().Add(rememberedSessionTTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.remembered == nil {
		s.remembered = make(map[string]time.Time)
	}
	for key, until := range s.remembered {
		if !until.After(time.Now()) {
			delete(s.remembered, key)
		}
	}
	if len(s.remembered) >= 1024 {
		return "", time.Time{}, errors.New("too many remembered sessions")
	}
	key := sessionDigest(token)
	s.remembered[key] = expires
	if err := s.saveRememberedSessions(); err != nil {
		delete(s.remembered, key)
		return "", time.Time{}, err
	}
	return token, expires, nil
}
