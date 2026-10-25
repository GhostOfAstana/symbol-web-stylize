package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	assets "github.com/GhostOfAstana/symbol-web-stylize"
	"github.com/GhostOfAstana/symbol-web-stylize/internal/ai"
)

func testApp(t *testing.T) http.Handler {
	t.Helper()
	c, _ := ai.NewClient(ai.Config{})
	h, err := New(assets.FS, c)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func request(h http.Handler, method, path, body, contentType string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestPagesStaticAndMethods(t *testing.T) {
	h := testApp(t)
	for _, tc := range []struct {
		method, path string
		want         int
	}{{"GET", "/", 200}, {"GET", "/healthz", 200}, {"GET", "/static/css/main.css", 200}, {"GET", "/static/js/palette.js", 200}, {"GET", "/static/", 404}, {"GET", "/missing", 404}, {"GET", "/symbol-art", 405}, {"GET", "/api/suggest-palette", 405}} {
		w := request(h, tc.method, tc.path, "", "")
		if w.Code != tc.want {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("нет защитного заголовка")
		}
	}
	w := request(h, "GET", "/", "", "")
	if strings.Contains(w.Body.String(), " style=") || strings.Contains(w.Body.String(), "<style") {
		t.Fatal("CSS встроен в HTML")
	}
	for _, id := range []string{"text-input", "banner", "suggest-palette", "palette-status", "saved-palettes", "dark-toggle"} {
		if !strings.Contains(w.Body.String(), `id="`+id+`"`) {
			t.Fatalf("нет элемента %s", id)
		}
	}
}

func TestGenerationAndHostileForms(t *testing.T) {
	h := testApp(t)
	for _, banner := range []string{"standard", "shadow", "thinkertoy"} {
		body := url.Values{"text": {"hello"}, "banner": {banner}}.Encode()
		w := request(h, "POST", "/symbol-art", body, "application/x-www-form-urlencoded")
		if w.Code != 200 || !strings.Contains(w.Body.String(), `id="art-output"`) {
			t.Fatalf("шрифт %s: %d %s", banner, w.Code, w.Body.String())
		}
	}
	for _, tc := range []url.Values{{"text": {""}, "banner": {"standard"}}, {"text": {"Привет"}, "banner": {"standard"}}, {"text": {"hello"}, "banner": {"../../etc/passwd"}}, {"text": {strings.Repeat("a", 4097)}, "banner": {"standard"}}} {
		w := request(h, "POST", "/symbol-art", tc.Encode(), "application/x-www-form-urlencoded")
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	evil := `</textarea><script>alert(1)</script>`
	w := request(h, "POST", "/symbol-art", url.Values{"text": {evil}, "banner": {"standard"}}.Encode(), "application/x-www-form-urlencoded")
	if w.Code != 200 || strings.Contains(w.Body.String(), evil) {
		t.Fatal("неэкранированный HTML")
	}
	w = request(h, "POST", "/symbol-art", "text="+strings.Repeat("a", 70<<10), "application/x-www-form-urlencoded")
	if w.Code != 400 {
		t.Fatal("не ограничено тело формы")
	}
}

func TestPaletteEndpointValidation(t *testing.T) {
	h := testApp(t)
	w := request(h, "POST", "/api/suggest-palette", `{"text":"Happy Birthday!"}`, "application/json; charset=utf-8")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		Palettes []ai.Palette `json:"palettes"`
		Mode     string       `json:"mode"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Palettes) != 3 || result.Mode != "mock" {
		t.Fatal(w.Body.String())
	}
	for _, p := range result.Palettes {
		ratio, _ := ai.Contrast(p.Colors.Background, p.Colors.Text)
		if ratio < 4.5 {
			t.Fatal(ratio)
		}
	}
	for _, body := range []string{`{}`, `null`, `{"text":""}`, `{"text":7}`, `{"text":"hello","extra":1}`, `{"text":"hello"} {}`, `{"text":"hello","preference":{"samples":2,"hue":1,"brightness":0.3}}`, strings.Repeat("x", 17000)} {
		w := request(h, "POST", "/api/suggest-palette", body, "application/json")
		if w.Code != 400 {
			t.Fatalf("%s: %d", body[:min(len(body), 80)], w.Code)
		}
	}
	w = request(h, "POST", "/api/suggest-palette", `{"text":"hello"}`, "text/plain")
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	memory := request(h, "POST", "/api/suggest-palette", `{"text":"Happy Birthday!","preference":{"samples":3,"hue":80,"brightness":0.6}}`, "application/json")
	if memory.Code != 200 || bytes.Equal(memory.Body.Bytes(), w.Body.Bytes()) {
		t.Fatal("память не применяется")
	}
}

func TestPaletteMockWithModelButNoBackend(t *testing.T) {
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_MODEL", "configured-but-no-backend")
	t.Setenv("LLM_PROVIDER", "")
	t.Setenv("LLM_PROTOCOL", "")
	client, err := ai.NewClient(ai.ConfigFromEnv())
	if err != nil {
		t.Fatal(err)
	}
	application, err := New(assets.FS, client)
	if err != nil {
		t.Fatal(err)
	}
	response := request(application, "POST", "/api/suggest-palette", `{"text":"hello"}`, "application/json")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"mode":"mock"`) {
		t.Fatalf("%d: %s", response.Code, response.Body.String())
	}
}

func TestPaletteUnavailable(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer upstream.Close()
	c, _ := ai.NewClient(ai.Config{BaseURL: upstream.URL, Model: "m"})
	h, err := New(assets.FS, c)
	if err != nil {
		t.Fatal(err)
	}
	w := request(h, "POST", "/api/suggest-palette", `{"text":"hello"}`, "application/json")
	if w.Code != 503 || !strings.Contains(w.Body.String(), "временно недоступен") {
		t.Fatal(w.Code, w.Body.String())
	}
	if request(h, "GET", "/healthz", "", "").Code != 200 {
		t.Fatal("сервер остановился после сбоя upstream")
	}
}

func TestInvalidPaletteResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"response":"{\"palettes\":[]}"}`))
	}))
	defer upstream.Close()
	client, _ := ai.NewClient(ai.Config{BaseURL: upstream.URL, Model: "m"})
	application, err := New(assets.FS, client)
	if err != nil {
		t.Fatal(err)
	}
	response := request(application, "POST", "/api/suggest-palette", `{"text":"hello"}`, "application/json")
	if response.Code != 500 {
		t.Fatalf("%d: %s", response.Code, response.Body.String())
	}
	if request(application, "GET", "/healthz", "", "").Code != 200 {
		t.Fatal("сервер остановился после некорректного ответа модели")
	}
}

func TestConcurrentHTTPStress(t *testing.T) {
	h := testApp(t)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := request(h, "POST", "/api/suggest-palette", `{"text":"Hello"}`, "application/json")
			if w.Code != 200 {
				t.Error(w.Code)
			}
			w = request(h, "POST", "/symbol-art", "text=Hello&banner=standard", "application/x-www-form-urlencoded")
			if w.Code != 200 {
				t.Error(w.Code)
			}
		}()
	}
	wg.Wait()
}
