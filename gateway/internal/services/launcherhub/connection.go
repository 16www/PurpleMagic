package launcherhub

import (
	"encoding/json"
	"net"
	"net/http"
	"regexp"
	"strings"
)

// Only the client connection target is editable here. Trust roots, asset hashes,
// channel identity and all other approved profile fields retain their values.
type gameConnection struct {
	GameHost      string `json:"gameHost"`
	BootstrapPort int    `json:"bootstrapPort"`
}

func connectionOf(p Profile) gameConnection { return gameConnection{p.GameHost, p.BootstrapPort} }
func connectionRevision(p Profile) string {
	raw, _ := json.Marshal(connectionOf(p))
	return digest(raw)
}

var hostnameLabel = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

func validGameHost(host string) bool {
	if host == "" || len(host) > 253 || strings.TrimSpace(host) != host {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if !hostnameLabel.MatchString(label) {
			return false
		}
	}
	return true
}
func (h *Hub) publishConnection(w http.ResponseWriter, r *http.Request) {
	var input gameConnection
	if err := readBody(w, r, &input); err != nil || !validGameHost(input.GameHost) || input.BootstrapPort < 1 || input.BootstrapPort > 65535 {
		failure(w, 400, "游戏地址须为 IP 或主机名，端口须为 1–65535")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	before := connectionRevision(h.state.Published.Profile)
	if r.Header.Get("If-Match") == "" {
		failure(w, 428, "请先读取当前游戏连接配置")
		return
	}
	if connectionOf(h.state.Published.Profile) == input {
		reply(w, 200, map[string]any{"ok": true, "connectionRevision": before, "sequence": h.state.Published.Sequence})
		return
	}
	if r.Header.Get("If-Match") != `"`+before+`"` {
		failure(w, 409, "游戏连接已被其他窗口修改，请重新载入")
		return
	}
	previous := h.state.Published
	h.state.Published.Profile.GameHost = input.GameHost
	h.state.Published.Profile.BootstrapPort = input.BootstrapPort
	h.state.Published.Sequence++
	if err := h.save(); err != nil {
		h.state.Published = previous
		failure(w, 500, "发布游戏连接失败")
		return
	}
	reply(w, 200, map[string]any{"ok": true, "connectionRevision": connectionRevision(h.state.Published.Profile), "sequence": h.state.Published.Sequence})
}
