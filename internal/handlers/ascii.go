// Package handlers связывает HTTP, генератор баннеров и помощника по оформлению.
package handlers

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"github.com/GhostOfAstana/symbol-web-stylize/internal/ai"
	"github.com/GhostOfAstana/symbol-web-stylize/internal/ascii"
	"github.com/GhostOfAstana/symbol-web-stylize/internal/textai"
)

type app struct {
	generator *ascii.Generator
	templates *template.Template
	palettes  *ai.Client
}

type page struct {
	Title  string
	Text   string
	Banner string
	Art    string
	Error  string
	Mode   string
}

func New(fsys fs.FS, palettes *ai.Client) (http.Handler, error) {
	if palettes == nil {
		return nil, fmt.Errorf("клиент палитр не задан")
	}
	generator, err := ascii.New(fsys)
	if err != nil {
		return nil, err
	}
	templates, err := template.ParseFS(fsys, "templates/*.html")
	if err != nil {
		return nil, err
	}
	static, err := fs.Sub(fsys, "static")
	if err != nil {
		return nil, err
	}
	a := &app{generator: generator, templates: templates, palettes: palettes}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.index)
	mux.HandleFunc("POST /symbol-art", a.generate)
	mux.HandleFunc("POST /api/suggest-palette", a.suggestPalette)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	files := http.StripPrefix("/static/", http.FileServer(http.FS(static)))
	mux.HandleFunc("GET /static/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/static/")
		file, err := static.Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	})
	assistant, err := textai.NewFromEnv()
	if err != nil {
		return nil, err
	}
	assistant.Register(mux)
	return protect(mux), nil
}

func (a *app) index(w http.ResponseWriter, r *http.Request) {
	a.render(w, http.StatusOK, "index.html", page{Title: "Студия символьных баннеров", Text: "Hello", Banner: "standard", Mode: a.palettes.Mode()})
}

func (a *app) generate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		a.failure(w, http.StatusBadRequest, "Форма слишком большая или повреждена.")
		return
	}
	text, banner := r.PostForm.Get("text"), r.PostForm.Get("banner")
	if strings.TrimSpace(text) == "" {
		a.failure(w, http.StatusBadRequest, "Введите текст для баннера.")
		return
	}
	art, err := a.generator.Generate(text, banner)
	if err != nil {
		a.failure(w, http.StatusBadRequest, "Проверьте текст и шрифт. Разрешены печатные ASCII-символы и переносы строк, до 4096 байт.")
		return
	}
	a.render(w, http.StatusOK, "result.html", page{Title: "Ваш баннер готов", Text: text, Banner: banner, Art: art, Mode: a.palettes.Mode()})
}

func (a *app) failure(w http.ResponseWriter, status int, message string) {
	a.render(w, status, "error.html", page{Title: "Не удалось выполнить запрос", Error: message, Mode: a.palettes.Mode()})
}

func (a *app) render(w http.ResponseWriter, status int, name string, data page) {
	var buf bytes.Buffer
	if err := a.templates.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "Не удалось показать страницу.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		defer func() {
			if recover() != nil {
				log.Print("обработчик запроса завершился с внутренней ошибкой")
				http.Error(w, "Внутренняя ошибка сервера.", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
