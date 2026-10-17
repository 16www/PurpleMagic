package launcherhub

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"
)

func newsRevision(news []News) string {
	if news == nil {
		news = []News{}
	}
	raw, _ := json.Marshal(news)
	return digest(raw)
}

// The existing whole-list endpoint also accepts a content revision. Keeping the
// revision specific to news allows independent signed client releases to proceed.
func (h *Hub) publishNews(w http.ResponseWriter, r *http.Request) {
	var news []News
	if err := readBody(w, r, &news); err != nil || news == nil || len(news) > 30 {
		failure(w, 400, "公告格式或数量无效，最多 30 条")
		return
	}
	for _, n := range news {
		if strings.TrimSpace(n.Title) == "" || len(n.Title) > 160 || len(n.Body) > 12000 || len(n.Date) > 32 ||
			!utf8.ValidString(n.Title+n.Body+n.Date) || strings.ContainsRune(n.Title+n.Body+n.Date, 0) ||
			(n.Category != "综合" && n.Category != "更新" && n.Category != "指南") {
			failure(w, 400, "公告标题、长度或分类无效")
			return
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	before, next := newsRevision(h.state.Published.News), newsRevision(news)
	// A lost response can be replayed without advancing the signed sequence.
	if before == next {
		reply(w, 200, map[string]any{"ok": true, "newsRevision": before})
		return
	}
	if expected := r.Header.Get("If-Match"); expected != "" && expected != `"`+before+`"` {
		failure(w, 409, "公告已被其他窗口修改，请重新载入后编辑")
		return
	}
	previous := h.state.Published
	h.state.Published.News = news
	h.state.Published.Sequence++
	if err := h.save(); err != nil {
		h.state.Published = previous
		failure(w, 500, "发布公告失败")
		return
	}
	reply(w, 200, map[string]any{"ok": true, "newsRevision": next})
}
