// Package textai реализует подсказки текста и объяснимый выбор шрифта.
package textai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	requestTimeout   = 10 * time.Second
	maxResponseBytes = 64 << 10
	cacheLifetime    = 5 * time.Minute
	maxCacheEntries  = 256
)

type cacheEntry struct {
	data    []byte
	expires time.Time
}

// Service можно совместно использовать во всех запросах сервера.
type Service struct {
	endpoint string
	model    string
	provider string
	apiKey   string
	client   *http.Client
	mu       sync.Mutex
	cache    map[string]cacheEntry
	active   chan struct{}
}

// NewFromEnv выбирает mock при пустом LLM_BASE_URL. Имена моделей и адреса
// читаются из окружения; сетевой режим никогда не включается неявно.
func NewFromEnv() (*Service, error) {
	s := &Service{
		model:    strings.TrimSpace(os.Getenv("LLM_MODEL")),
		provider: strings.ToLower(strings.TrimSpace(os.Getenv("LLM_PROVIDER"))),
		apiKey:   os.Getenv("LLM_API_KEY"),
		client:   &http.Client{Timeout: requestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		cache:    make(map[string]cacheEntry),
		active:   make(chan struct{}, 8),
	}
	// Старое имя переменной поддерживается явно; противоречащие друг другу
	// настройки лучше обнаружить при запуске, чем отправить запрос не туда.
	legacyProvider := strings.ToLower(strings.TrimSpace(os.Getenv("LLM_PROTOCOL")))
	if s.provider != "" && legacyProvider != "" && s.provider != legacyProvider {
		return nil, errors.New("LLM_PROVIDER и LLM_PROTOCOL задают разные протоколы")
	}
	if s.provider == "" {
		s.provider = legacyProvider
	}
	base := strings.TrimSpace(os.Getenv("LLM_BASE_URL"))
	if base == "" {
		return s, nil
	}
	if s.model == "" {
		return nil, errors.New("при LLM_BASE_URL необходимо задать LLM_MODEL")
	}
	if s.provider == "" {
		s.provider = "ollama"
	}
	if s.provider != "ollama" && s.provider != "openai" {
		return nil, errors.New("LLM_PROVIDER должен быть ollama или openai")
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("LLM_BASE_URL должен быть HTTP(S)-адресом без пароля, запроса и фрагмента")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if s.provider == "ollama" {
		if strings.HasSuffix(u.Path, "/api") {
			u.Path += "/generate"
		} else {
			u.Path += "/api/generate"
		}
	} else if strings.HasSuffix(u.Path, "/v1") {
		u.Path += "/chat/completions"
	} else {
		u.Path += "/v1/chat/completions"
	}
	s.endpoint = u.String()
	return s, nil
}

func (s *Service) completion(ctx context.Context, prompt string) (string, error) {
	select {
	case s.active <- struct{}{}:
		defer func() { <-s.active }()
	default:
		return "", errors.New("слишком много одновременных запросов к модели")
	}
	var payload any = map[string]any{"model": s.model, "prompt": prompt, "stream": false, "options": map[string]any{"temperature": 0.5, "num_predict": 500}}
	if s.provider == "openai" {
		payload = map[string]any{"model": s.model, "messages": []map[string]string{{"role": "user", "content": prompt}}, "temperature": 0.5, "max_tokens": 500}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+s.apiKey)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("модель недоступна: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("модель вернула HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxResponseBytes {
		return "", errors.New("ответ модели превышает допустимый размер")
	}
	if s.provider == "openai" {
		var result struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(data, &result); err != nil || len(result.Choices) == 0 || result.Choices[0].Message.Content == "" {
			return "", errors.New("модель вернула некорректный ответ")
		}
		return result.Choices[0].Message.Content, nil
	}
	var result struct {
		Response string `json:"response"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal(data, &result); err != nil || result.Response == "" || result.Error != "" {
		return "", errors.New("модель вернула некорректный ответ")
	}
	return result.Response, nil
}

func (s *Service) cached(key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[key]
	if !ok || !time.Now().Before(entry.expires) {
		delete(s.cache, key)
		return nil, false
	}
	return entry.data, true
}

func (s *Service) remember(key string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for key, entry := range s.cache {
		if !now.Before(entry.expires) {
			delete(s.cache, key)
		}
	}
	if len(s.cache) >= maxCacheEntries {
		oldestKey := ""
		oldestTime := now.Add(cacheLifetime + time.Second)
		for key, entry := range s.cache {
			if entry.expires.Before(oldestTime) {
				oldestKey, oldestTime = key, entry.expires
			}
		}
		delete(s.cache, oldestKey)
	}
	s.cache[key] = cacheEntry{data: data, expires: now.Add(cacheLifetime)}
}
