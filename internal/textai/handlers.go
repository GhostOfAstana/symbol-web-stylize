package textai

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"
)

// Register добавляет только API-маршруты и не меняет главную страницу проекта.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/suggest", s.handle("suggest"))
	mux.HandleFunc("/api/variations", s.handle("variations"))
	mux.HandleFunc("/api/recommend-banner", s.handle("recommend"))
}

func (s *Service) handle(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			jsonError(w, http.StatusMethodNotAllowed, "Для этой операции нужен POST-запрос.")
			return
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			jsonError(w, http.StatusBadRequest, "Отправьте JSON с полем text.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		var input struct {
			Text string `json:"text"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			jsonError(w, http.StatusBadRequest, "Некорректный или слишком большой JSON-запрос.")
			return
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			jsonError(w, http.StatusBadRequest, "В запросе должен быть один JSON-объект.")
			return
		}
		text := strings.TrimSpace(input.Text)
		if len(text) == 0 || len(text) > 4096 || (kind != "recommend" && len(text) < 3) || !printableInput(text) {
			jsonError(w, http.StatusBadRequest, "Введите печатный ASCII-текст до 4096 байт; для подсказок нужно не менее трёх символов.")
			return
		}
		if kind == "recommend" {
			_ = json.NewEncoder(w).Encode(recommend(text))
			return
		}
		key := kind + "\x00" + text
		if s.endpoint == "" {
			w.Header().Set("X-AI-Mode", "mock")
		} else {
			w.Header().Set("X-AI-Mode", "live")
		}
		if data, ok := s.cached(key); ok {
			w.Header().Set("X-Suggestion-Cache", "hit")
			_, _ = w.Write(data)
			return
		}
		var result any
		if s.endpoint == "" {
			w.Header().Set("X-AI-Mode", "mock")
			if kind == "suggest" {
				result = map[string]any{"suggestions": mockSuggestions(text)}
			} else {
				result = map[string]any{"variations": mockVariations(text)}
			}
		} else {
			w.Header().Set("X-AI-Mode", "live")
			prompt := suggestionPrompt(text)
			if kind == "variations" {
				prompt = variationPrompt(text)
			}
			raw, err := s.completion(r.Context(), prompt)
			if err != nil {
				log.Printf("Ошибка сервиса текстовых подсказок: %v", err)
				jsonError(w, http.StatusServiceUnavailable, "Сервис подсказок временно недоступен. Основной генератор продолжает работать.")
				return
			}
			if kind == "suggest" {
				items, err := parseSuggestions(raw)
				if err != nil {
					log.Printf("Ошибка проверки текстовых подсказок: %v", err)
					jsonError(w, http.StatusInternalServerError, "Модель вернула некорректные подсказки. Повторите запрос.")
					return
				}
				result = map[string]any{"suggestions": items}
			} else {
				items, err := parseVariations(raw)
				if err != nil {
					log.Printf("Ошибка проверки вариантов текста: %v", err)
					jsonError(w, http.StatusInternalServerError, "Модель вернула некорректные варианты. Повторите запрос.")
					return
				}
				result = map[string]any{"variations": items}
			}
		}
		data, err := json.Marshal(result)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "Не удалось подготовить ответ.")
			return
		}
		s.remember(key, data)
		w.Header().Set("X-Suggestion-Cache", "miss")
		_, _ = w.Write(data)
	}
}

func printableInput(text string) bool {
	for i := range text {
		if (text[i] < 32 || text[i] > 126) && text[i] != '\n' && text[i] != '\r' {
			return false
		}
	}
	return true
}

func jsonError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
