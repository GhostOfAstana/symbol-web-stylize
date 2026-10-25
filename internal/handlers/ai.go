package handlers

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
)

// decodeJSON ограничивает вход до разбора и запрещает лишние документы и поля.
func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		jsonError(w, http.StatusBadRequest, "Ожидается Content-Type: application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(destination) != nil {
		jsonError(w, http.StatusBadRequest, "Некорректный JSON или слишком большой запрос.")
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		jsonError(w, http.StatusBadRequest, "Ожидается ровно один JSON-документ.")
		return false
	}
	return true
}

func jsonError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
