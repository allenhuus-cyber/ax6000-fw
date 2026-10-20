package api

import (
	"encoding/json"
	"net/http"
	"time"
)

func nowUnix() int64 { return time.Now().Unix() }

// writeJSON 输出 JSON 响应。
func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ok 成功响应。
func ok(w http.ResponseWriter, v interface{}) { writeJSON(w, http.StatusOK, v) }

// fail 错误响应，统一结构 {"error": "...", "code": <http code>}。
func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]interface{}{"error": msg, "code": code})
}

// decodeJSON 解析请求体，出错时返回 false 并写回 400。
func decodeJSON(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if r.Body == nil {
		fail(w, http.StatusBadRequest, "empty body")
		return false
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "bad json: "+err.Error())
		return false
	}
	return true
}
