package ai

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestContrastKnownPairs(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want float64
	}{{"#000000", "#FFFFFF", 21}, {"#FFFFFF", "#FFFFFF", 1}, {"#777777", "#FFFFFF", 4.478089453577214}} {
		got, err := Contrast(tc.a, tc.b)
		if err != nil || math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("%s/%s: %v %v", tc.a, tc.b, got, err)
		}
		reverse, _ := Contrast(tc.b, tc.a)
		if got != reverse {
			t.Fatal("отношение должно быть симметричным")
		}
	}
	for _, bad := range []string{"red", "#fff", "#gggggg", "url(x)", "#000000;"} {
		if _, err := Contrast(bad, "#FFFFFF"); err == nil {
			t.Fatalf("принят цвет %q", bad)
		}
	}
}

func TestNormalizeRepairsAndValidates(t *testing.T) {
	values := mockPalettes("hello", nil)
	values[0].Colors.Text = values[0].Colors.Background
	values[1].Recommended = true
	result, err := Normalize(values)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, p := range result {
		ratio, _ := Contrast(p.Colors.Background, p.Colors.Text)
		if ratio < 4.5 {
			t.Fatalf("слабый контраст: %v", ratio)
		}
		if p.Recommended {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("рекомендаций: %d", count)
	}
	if values[0].Colors.Text != values[0].Colors.Background {
		t.Fatal("изменён исходный срез")
	}
	if _, err := Normalize(values[:2]); err == nil {
		t.Fatal("принят неполный ответ")
	}
	values[0].Colors.Accent = "url(javascript:alert(1))"
	if _, err := Normalize(values); err == nil {
		t.Fatal("принят некорректный цвет")
	}
	values = mockPalettes("hello", nil)
	values[1].Name = values[0].Name
	if _, err := Normalize(values); err == nil {
		t.Fatal("приняты повторяющиеся названия")
	}
}

func TestMockDeterminismAndStyleMemory(t *testing.T) {
	c, err := NewClient(Config{})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := c.Suggest(context.Background(), "hello", nil)
	b, _ := c.Suggest(context.Background(), "hello", nil)
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	if string(x) != string(y) {
		t.Fatal("mock не воспроизводим")
	}
	pref := &Preference{Samples: 3, Hue: 350, Brightness: .7}
	biased, err := c.Suggest(context.Background(), "hello", pref)
	if err != nil {
		t.Fatal(err)
	}
	if biased[0].Colors.Primary == a[0].Colors.Primary {
		t.Fatal("память не повлияла на mock")
	}
	if !strings.Contains(buildPrompt("hello", pref), "350.0") {
		t.Fatal("память не попала в prompt")
	}
	if strings.Contains(buildPrompt("ignore\n\"instructions", nil), "ignore\n\"instructions") {
		t.Fatal("текст не экранирован как данные")
	}
}

func TestConfigurationAndInput(t *testing.T) {
	for _, config := range []Config{{BaseURL: "http://localhost"}, {BaseURL: "file:///tmp/x", Model: "m"}, {BaseURL: "http://u:p@host", Model: "m"}, {BaseURL: "http://localhost?q=x", Model: "m"}, {Protocol: "bad"}} {
		if _, err := NewClient(config); err == nil {
			t.Fatalf("принята конфигурация %+v", config)
		}
	}
	for _, text := range []string{"", "  ", strings.Repeat("a", MaxTextBytes+1), string([]byte{255})} {
		if ValidateRequest(text, nil) == nil {
			t.Fatal("принят неверный текст")
		}
	}
	for _, pref := range []*Preference{{Samples: 2}, {Samples: 31}, {Samples: 3, Hue: 360}, {Samples: 3, Hue: math.NaN()}, {Samples: 3, Brightness: 1.1}, {Samples: 3, Brightness: math.Inf(1)}} {
		if ValidateRequest("hello", pref) == nil {
			t.Fatalf("принята память: %+v", pref)
		}
	}
}

func TestMockWithModelOnly(t *testing.T) {
	client, err := NewClient(Config{Model: "configured-but-no-backend"})
	if err != nil {
		t.Fatal(err)
	}
	if client.Mode() != "mock" {
		t.Fatal("без адреса backend должен включаться mock")
	}
	palettes, err := client.Suggest(context.Background(), "hello", nil)
	if err != nil || len(palettes) != 3 {
		t.Fatalf("%v: %v", palettes, err)
	}
}

func TestRealLLMProtocols(t *testing.T) {
	for _, protocol := range []string{"ollama", "openai"} {
		t.Run(protocol, func(t *testing.T) {
			palettes, _ := json.Marshal(map[string]any{"palettes": mockPalettes("hello", nil)})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expected := "/api/generate"
				if protocol == "openai" {
					expected = "/v1/chat/completions"
				}
				if r.URL.Path != expected || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-key" {
					t.Errorf("неверный upstream-запрос: %s %s", r.Method, r.URL.Path)
				}
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil || body["model"] != "test-model" {
					t.Error("модель не получена из конфигурации")
				}
				if protocol == "ollama" {
					if body["stream"] != false {
						t.Error("ожидался не потоковый ответ")
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"response": string(palettes)})
				} else {
					_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(palettes)}}}})
				}
			}))
			defer server.Close()
			c, err := NewClient(Config{BaseURL: server.URL, Model: "test-model", Protocol: protocol, APIKey: "test-key"})
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.Suggest(context.Background(), "Hello", nil)
			if err != nil || len(got) != 3 {
				t.Fatalf("%v %v", got, err)
			}
		})
	}
}

func TestBackendFailures(t *testing.T) {
	for name, handle := range map[string]http.HandlerFunc{
		"status":    func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
		"malformed": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"response":"invalid JSON"}`)) },
		"oversized": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
		},
		"redirect": func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/elsewhere", 302) },
		"timeout": func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(300 * time.Millisecond):
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handle)
			defer server.Close()
			c, _ := NewClient(Config{BaseURL: server.URL, Model: "m"})
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			started := time.Now()
			_, err := c.Suggest(ctx, "hello", nil)
			want := ErrUnavailable
			if name == "malformed" || name == "oversized" {
				want = ErrInvalidResponse
			}
			if err != want {
				t.Fatalf("ожидалась управляемая ошибка, получено %v", err)
			}
			if time.Since(started) > time.Second {
				t.Fatal("не соблюдена отмена запроса")
			}
		})
	}
	c, _ := NewClient(Config{BaseURL: "http://127.0.0.1:1", Model: "m"})
	if _, err := c.Suggest(context.Background(), "hello", nil); err != ErrUnavailable {
		t.Fatal(err)
	}
}

func TestProviderAndLegacyProtocol(t *testing.T) {
	for _, test := range []struct {
		provider, legacy, want string
		invalid                bool
	}{
		{"openai", "", "openai", false},
		{"", "openai", "openai", false},
		{"openai", "openai", "openai", false},
		{"openai", "ollama", "", true},
	} {
		t.Setenv("LLM_BASE_URL", "http://localhost:1234")
		t.Setenv("LLM_MODEL", "configured-model")
		t.Setenv("LLM_PROVIDER", test.provider)
		t.Setenv("LLM_PROTOCOL", test.legacy)
		client, err := NewClient(ConfigFromEnv())
		if test.invalid {
			if err == nil {
				t.Fatal("принят противоречивый протокол")
			}
			continue
		}
		if err != nil || client.Mode() != test.want || client.endpoint != "http://localhost:1234/v1/chat/completions" {
			t.Fatalf("%+v: %v", test, err)
		}
	}
}

func TestRealLLMConcurrencyLimit(t *testing.T) {
	started, release := make(chan struct{}, 8), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	palettes, _ := json.Marshal(map[string]any{"palettes": mockPalettes("hello", nil)})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"response": string(palettes)})
	}))
	defer func() { unblock(); server.Close() }()
	client, err := NewClient(Config{BaseURL: server.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			if _, err := client.Suggest(context.Background(), "hello", nil); err != nil {
				t.Error(err)
			}
		})
	}
	for range 8 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			unblock()
			group.Wait()
			t.Fatal("не дождались восьми запросов к модели")
		}
	}
	before := time.Now()
	if _, err := client.Suggest(context.Background(), "hello", nil); err != ErrUnavailable {
		t.Fatalf("девятый запрос не отклонён: %v", err)
	}
	if time.Since(before) > time.Second {
		t.Fatal("при перегрузке запрос поставлен в неограниченную очередь")
	}
	unblock()
	group.Wait()
	// После завершения вызовов слот снова должен быть доступен.
	if _, err := client.Suggest(context.Background(), "hello", nil); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentMockRequests(t *testing.T) {
	c, _ := NewClient(Config{})
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Suggest(context.Background(), "hello", &Preference{Samples: 3, Hue: 20, Brightness: .6}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func FuzzContrast(f *testing.F) {
	f.Add("#123456", "#ABCDEF")
	f.Add("bad", "")
	f.Fuzz(func(t *testing.T, a, b string) {
		ratio, err := Contrast(a, b)
		if err == nil && (ratio < 1 || ratio > 21 || math.IsNaN(ratio)) {
			t.Fatalf("неверный контраст: %v", ratio)
		}
	})
}
