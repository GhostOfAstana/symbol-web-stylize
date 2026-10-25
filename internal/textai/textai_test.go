package textai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func mockService(t *testing.T) *Service {
	t.Helper()
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_MODEL", "")
	t.Setenv("LLM_PROVIDER", "")
	t.Setenv("LLM_PROTOCOL", "")
	s, err := NewFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func call(t testing.TB, s *Service, route, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	s.Register(mux)
	r := httptest.NewRequest(method, route, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestMockAndCache(t *testing.T) {
	s := mockService(t)
	for _, route := range []string{"/api/suggest", "/api/variations", "/api/recommend-banner"} {
		first := call(t, s, route, "POST", `{"text":"hello"}`)
		second := call(t, s, route, "POST", `{"text":"hello"}`)
		if first.Code != 200 || first.Body.String() != second.Body.String() {
			t.Fatalf("%s: %d %s", route, first.Code, first.Body.String())
		}
		if route != "/api/recommend-banner" && second.Header().Get("X-Suggestion-Cache") != "hit" {
			t.Fatal("second response not cached")
		}
	}
	data := call(t, s, "/api/suggest", "POST", `{"text":"Happy Birth"}`)
	var suggestions struct {
		Suggestions []string `json:"suggestions"`
	}
	if err := json.Unmarshal(data.Body.Bytes(), &suggestions); err != nil {
		t.Fatal(err)
	}
	if len(suggestions.Suggestions) != 3 {
		t.Fatal(suggestions)
	}
	for _, text := range suggestions.Suggestions {
		if !validArtText(text) {
			t.Fatal(text)
		}
	}
}

func TestRecommendationRules(t *testing.T) {
	for _, tc := range []struct{ text, want string }{{"WELCOME", "shadow"}, {"hello", "shadow"}, {"Hello team", "thinkertoy"}, {"A longer mixed text", "standard"}, {"123456789", "thinkertoy"}} {
		got := recommend(tc.text)
		if got.Recommended != tc.want || got.Reasoning == "" || len(got.Alternatives) != 2 {
			t.Fatalf("%q: %+v", tc.text, got)
		}
	}
}

func TestRequestValidation(t *testing.T) {
	s := mockService(t)
	for _, tc := range []struct {
		method, body string
		status       int
	}{
		{"GET", `{"text":"hello"}`, 405},
		{"POST", `{`, 400},
		{"POST", `null`, 400},
		{"POST", `{"text":""}`, 400},
		{"POST", `{"text":"ab"}`, 400},
		{"POST", `{"text":"Привет"}`, 400},
		{"POST", `{"text":"hello","extra":true}`, 400},
		{"POST", `{"text":"hello"} {}`, 400},
		{"POST", `{"text":"` + strings.Repeat("a", 4097) + `"}`, 400},
		{"POST", `{"text":"` + strings.Repeat("a", 17000) + `"}`, 400},
	} {
		got := call(t, s, "/api/suggest", tc.method, tc.body)
		if got.Code != tc.status {
			t.Fatalf("%s %s: got=%d want=%d", tc.method, tc.body[:min(len(tc.body), 80)], got.Code, tc.status)
		}
	}
	r := httptest.NewRequest("POST", "/api/suggest", strings.NewReader(`{"text":"hello"}`))
	w := httptest.NewRecorder()
	s.handle("suggest")(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestConfiguration(t *testing.T) {
	for _, tc := range []struct {
		base, model, provider string
		ok                    bool
	}{
		{"", "", "", true}, {"http://localhost:11434", "configured-model", "", true},
		{"http://localhost:8000/v1", "configured-model", "openai", true},
		{"file:///etc/passwd", "model", "", false}, {"http://user:pass@localhost", "model", "", false},
		{"http://localhost?secret=yes", "model", "", false}, {"http://localhost", "", "", false},
		{"http://localhost", "model", "other", false},
	} {
		t.Setenv("LLM_BASE_URL", tc.base)
		t.Setenv("LLM_MODEL", tc.model)
		t.Setenv("LLM_PROVIDER", tc.provider)
		t.Setenv("LLM_PROTOCOL", "")
		_, err := NewFromEnv()
		if (err == nil) != tc.ok {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
}

func TestLegacyProtocolConfiguration(t *testing.T) {
	t.Setenv("LLM_BASE_URL", "http://localhost:8000")
	t.Setenv("LLM_MODEL", "configured-model")
	for _, tc := range []struct {
		provider, protocol string
		ok                 bool
	}{
		{"", "openai", true}, {"openai", "OPENAI", true}, {"ollama", "openai", false}, {"openai", "ollama", false},
	} {
		t.Setenv("LLM_PROVIDER", tc.provider)
		t.Setenv("LLM_PROTOCOL", tc.protocol)
		s, err := NewFromEnv()
		if (err == nil) != tc.ok {
			t.Fatalf("%+v: %v", tc, err)
		}
		if tc.ok && (s.provider != "openai" || !strings.HasSuffix(s.endpoint, "/v1/chat/completions")) {
			t.Fatal("не применён старый протокол", s.provider, s.endpoint)
		}
	}
}

func TestLiveBackends(t *testing.T) {
	for _, provider := range []string{"ollama", "openai"} {
		t.Run(provider, func(t *testing.T) {
			var requests int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("missing auth")
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if payload["model"] != "configured-model" {
					t.Error("model not from config")
				}
				answer := `["Hello!","Hello World","~ Hello ~"]`
				if provider == "openai" {
					if r.URL.Path != "/v1/chat/completions" {
						t.Error(r.URL.Path)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": answer}}}})
				} else {
					if r.URL.Path != "/api/generate" {
						t.Error(r.URL.Path)
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"response": answer})
				}
			}))
			defer server.Close()
			t.Setenv("LLM_BASE_URL", server.URL)
			t.Setenv("LLM_MODEL", "configured-model")
			t.Setenv("LLM_PROVIDER", provider)
			t.Setenv("LLM_PROTOCOL", "")
			t.Setenv("LLM_API_KEY", "test-key")
			s, err := NewFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if got := call(t, s, "/api/suggest", "POST", `{"text":"Hello"}`); got.Code != 200 {
					t.Fatal(got.Code, got.Body.String())
				}
			}
			if requests != 1 {
				t.Fatal("cache missed", requests)
			}
		})
	}
}

func TestOllamaBaseURLPaths(t *testing.T) {
	for _, suffix := range []string{"", "/", "/api", "/api/"} {
		t.Run("base"+suffix, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/generate" {
					t.Errorf("неправильный путь Ollama: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"response": `["One","Two","Three"]`})
			}))
			defer server.Close()
			t.Setenv("LLM_BASE_URL", server.URL+suffix)
			t.Setenv("LLM_MODEL", "configured-model")
			t.Setenv("LLM_PROVIDER", "ollama")
			t.Setenv("LLM_PROTOCOL", "")
			s, err := NewFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			got := call(t, s, "/api/suggest", "POST", `{"text":"hello"}`)
			if got.Code != http.StatusOK {
				t.Fatal(got.Code, got.Body.String())
			}
		})
	}
}

func TestBackendFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body                string
		backendStatus, wantStatus int
		delay                     time.Duration
	}{
		{"unavailable", "", 503, 503, 0}, {"invalid transport JSON", "{", 200, 503, 0},
		{"invalid completions", `{"response":"[\"ok\"]"}`, 200, 500, 0},
		{"oversize", strings.Repeat("x", maxResponseBytes+1), 200, 503, 0},
		{"timeout", `{"response":"[]"}`, 200, 503, 50 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(tc.delay)
				w.WriteHeader(tc.backendStatus)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			t.Setenv("LLM_BASE_URL", server.URL)
			t.Setenv("LLM_MODEL", "model")
			t.Setenv("LLM_PROVIDER", "ollama")
			t.Setenv("LLM_PROTOCOL", "")
			s, err := NewFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			if tc.delay > 0 {
				s.client.Timeout = 5 * time.Millisecond
			}
			got := call(t, s, "/api/suggest", "POST", `{"text":"hello"}`)
			if got.Code != tc.wantStatus {
				t.Fatal(got.Code, got.Body.String())
			}
			if len(s.cache) != 0 {
				t.Fatal("error cached")
			}
		})
	}
}

func TestParsers(t *testing.T) {
	if _, err := parseSuggestions("One\nTwo\nThree"); err != nil {
		t.Fatal(err)
	}
	if _, err := parseSuggestions("```json\n[\"One\",\"Two\",\"Three\"]\n```"); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`[]`, `["same","same","same"]`, `["ok","ok2","Привет"]`, `["one","two","` + strings.Repeat("x", 50) + `"]`} {
		if _, err := parseSuggestions(raw); err == nil {
			t.Fatal("bad suggestions accepted", raw)
		}
	}
	valid, _ := json.Marshal(mockVariations("hello"))
	if _, err := parseVariations(string(valid)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"{", "[]", strings.Replace(string(valid), "standard", "invalid", 1), strings.Replace(string(valid), `"text":"hello."`, `"text":""`, 1)} {
		if _, err := parseVariations(raw); err == nil {
			t.Fatal("bad variations accepted", raw)
		}
	}
}

func TestCacheBoundsAndConcurrency(t *testing.T) {
	s := mockService(t)
	s.cache["expired"] = cacheEntry{data: []byte("old"), expires: time.Now().Add(-time.Second)}
	if _, ok := s.cached("expired"); ok {
		t.Fatal("expired entry returned")
	}
	for i := range maxCacheEntries + 20 {
		s.remember(strings.Repeat("x", i+1), []byte("data"))
	}
	if len(s.cache) > maxCacheEntries {
		t.Fatal(len(s.cache))
	}
	var group sync.WaitGroup
	for range 64 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 10 {
				got := call(t, s, "/api/suggest", "POST", `{"text":"hello"}`)
				if got.Code != 200 {
					t.Errorf("status=%d", got.Code)
				}
			}
		}()
	}
	group.Wait()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.endpoint = "http://127.0.0.1:1"
	if _, err := s.completion(ctx, "hello"); err == nil {
		t.Fatal("cancelled call succeeded")
	}
}
