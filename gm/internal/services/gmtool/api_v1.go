package gmtool

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Cec1c/DNF90GM/internal/modules/dnf/repository"
)

type v1RequestIDKey struct{}
type v1Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}
type v1Page struct {
	Limit      int    `json:"limit"`
	NextCursor string `json:"nextCursor,omitempty"`
}
type v1Envelope struct {
	Data       any      `json:"data,omitempty"`
	Page       *v1Page  `json:"page,omitempty"`
	ObservedAt string   `json:"observedAt"`
	RequestID  string   `json:"requestId"`
	Error      *v1Error `json:"error,omitempty"`
}

func v1Reply(w http.ResponseWriter, r *http.Request, status int, out v1Envelope) {
	out.RequestID, _ = r.Context().Value(v1RequestIDKey{}).(string)
	out.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(out)
}
func v1Fail(w http.ResponseWriter, r *http.Request, status int, code, message string, retry bool) {
	v1Reply(w, r, status, v1Envelope{Error: &v1Error{Code: code, Message: message, Retryable: retry}})
}

func (s *Server) v1Routes() http.Handler {
	mux := http.NewServeMux()
	writes := http.NewServeMux()
	s.v1OperationRoutes(mux, writes)
	mux.HandleFunc("GET /api/v1/capabilities", s.v1Capabilities)
	mux.HandleFunc("GET /api/v1/accounts", s.v1Accounts)
	mux.HandleFunc("GET /api/v1/accounts/{accountId}", s.v1Account)
	mux.HandleFunc("GET /api/v1/accounts/{accountId}/characters", s.v1AccountCharacters)
	mux.HandleFunc("GET /api/v1/characters", s.v1Characters)
	mux.HandleFunc("GET /api/v1/characters/{characterId}", s.v1Character)
	mux.HandleFunc("GET /api/v1/characters/{characterId}/inventory", s.v1Inventory)
	mux.HandleFunc("GET /api/v1/characters/{characterId}/inventory/{container}/{slot}", s.v1InventoryDetail)
	mux.HandleFunc("GET /api/v1/characters/{characterId}/mailbox", s.v1Mailbox)
	mux.HandleFunc("GET /api/v1/characters/{characterId}/mailbox/{mailId}", s.v1MailboxDetail)
	mux.HandleFunc("GET /api/v1/characters/{characterId}/quests", s.v1CharacterQuests)
	mux.HandleFunc("GET /api/v1/quests", s.v1Quests)
	mux.HandleFunc("GET /api/v1/quests/{questId}", s.v1QuestDetail)
	mux.HandleFunc("GET /api/v1/items", s.v1Items)
	mux.HandleFunc("GET /api/v1/items/{itemId}", s.v1ItemDetail)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { v1Fail(w, r, 404, "not_found", "接口不存在", false) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			v1Fail(w, r, 503, "unavailable", "服务暂不可用", true)
			return
		}
		id := hex.EncodeToString(raw[:])
		r = r.WithContext(context.WithValue(r.Context(), v1RequestIDKey{}, id))
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !s.authenticated(r) {
			v1Fail(w, r, 401, "authentication_required", "请先登录管理工作台", false)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if _, pattern := writes.Handler(r); pattern != "" && r.Method == http.MethodPost {
				if !s.v1CheckWrite(w, r) {
					return
				}
				ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
				defer cancel()
				writes.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			v1Fail(w, r, 405, "method_not_allowed", "此接口不支持该请求方法", false)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

type v1Query struct {
	Q      string
	Limit  int
	Cursor string
	Values url.Values
}

func v1ParseQuery(r *http.Request, extra ...string) (v1Query, error) {
	q := v1Query{Limit: 20}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return q, err
	}
	allowed := map[string]bool{"q": true, "limit": true, "cursor": true}
	for _, key := range extra {
		allowed[key] = true
	}
	for key, values := range values {
		if !allowed[key] || len(values) != 1 {
			return q, errors.New("unknown or duplicate query field")
		}
	}
	q.Values = values
	q.Q = strings.TrimSpace(values.Get("q"))
	q.Cursor = values.Get("cursor")
	if len(q.Q) > 128 || !utf8.ValidString(q.Q) || strings.ContainsRune(q.Q, 0) || len(q.Cursor) > 2048 {
		return q, errors.New("query too large")
	}
	if raw, ok := values["limit"]; ok {
		q.Limit, err = strconv.Atoi(raw[0])
		if err != nil || q.Limit < 1 || q.Limit > 100 {
			return q, errors.New("limit must be 1..100")
		}
	}
	return q, nil
}

type v1Cursor struct {
	Version  int    `json:"v"`
	Resource string `json:"r"`
	Filter   string `json:"f"`
	After    string `json:"a"`
}

func (s *Server) v1CursorToken(resource, filter, after string) string {
	stamp := sha256.Sum256([]byte(filter))
	raw, _ := json.Marshal(v1Cursor{1, resource, hex.EncodeToString(stamp[:]), after})
	key := sha256.Sum256([]byte("gm-v1-cursor\x00" + s.cfg.Password))
	mac := hmac.New(sha256.New, key[:])
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Server) v1CursorAfter(token, resource, filter string) (string, error) {
	if token == "" {
		return "", nil
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || len(token) > 2048 {
		return "", errors.New("invalid cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	key := sha256.Sum256([]byte("gm-v1-cursor\x00" + s.cfg.Password))
	mac := hmac.New(sha256.New, key[:])
	mac.Write(raw)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return "", errors.New("invalid cursor signature")
	}
	var c v1Cursor
	stamp := sha256.Sum256([]byte(filter))
	if json.Unmarshal(raw, &c) != nil || c.Version != 1 || c.Resource != resource || c.Filter != hex.EncodeToString(stamp[:]) || len(c.After) > 128 {
		return "", errors.New("cursor does not match query")
	}
	return c.After, nil
}
func v1Filter(q v1Query, extra string) string {
	return q.Q + "\x00" + strconv.Itoa(q.Limit) + "\x00" + extra
}
func v1ValidID(id string) bool {
	return id != "" && id == strings.TrimSpace(id) && len(id) <= 128 && utf8.ValidString(id) && !strings.ContainsAny(id, "\x00/\\")
}

func (s *Server) v1SummaryReader() (repository.CharacterSummaryReader, error) {
	if s.deps.Repos == nil {
		return nil, errRepositoriesUnavailable
	}
	g, available := s.deps.Repos()
	if !available {
		return nil, errRepositoriesUnavailable
	}
	reader, ok := g.Character.(repository.CharacterSummaryReader)
	if !ok {
		return nil, errors.New("summary reader unavailable")
	}
	return reader, nil
}
func (s *Server) v1Unavailable(w http.ResponseWriter, r *http.Request, err error) {
	if s.cfg.Logger != nil {
		s.cfg.Logger("gm-v1-read-unavailable", "request_id", r.Context().Value(v1RequestIDKey{}), "cause", err.Error())
	}
	v1Fail(w, r, 503, "dependency_unavailable", "数据暂时无法读取，请稍后重试", true)
}
func (s *Server) v1Capabilities(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		v1Fail(w, r, 400, "invalid_query", "此接口不接受查询参数", false)
		return
	}
	_, err := s.v1SummaryReader()
	items := s.items != nil && s.items.isReady() && s.items.failure() == nil
	var group repository.Group
	available := false
	if s.deps.Repos != nil {
		group, available = s.deps.Repos()
	}
	_, transactional := group.MailboxAssets.(repository.AccountMailboxCharacterUnitOfWork)
	writable := s.deps.Benefits != nil && available && transactional
	features := map[string]bool{"accounts.read": s.db != nil && err == nil, "characters.read": err == nil, "items.read": items, "mail.write": writable, "benefits.write": writable && items, "tasks.read": s.deps.Benefits != nil, "bans.write": false, "dungeonRuns.read": false}
	_, resources := group.Character.(repository.CharacterResourceReader)
	features["inventory.read"] = available && resources && items
	features["mailbox.read"] = available && resources
	_, questErr := s.questReadIndex(r.Context())
	features["quests.read"] = available && resources && questErr == nil
	v1Reply(w, r, 200, v1Envelope{Data: map[string]any{"apiVersion": "1", "features": features, "csrfToken": s.v1CSRF(r), "benefitScopes": []string{"account", "character"}, "legacyBenefitsScopes": []string{"account", "character"}, "itemIndexReady": items, "maxPageSize": 100, "mailBatchSize": 10}})
}
