package ai

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Colors struct {
	Background string `json:"background"`
	Primary    string `json:"primary"`
	Secondary  string `json:"secondary"`
	Text       string `json:"text"`
	Accent     string `json:"accent"`
}

type Palette struct {
	Name        string `json:"name"`
	Colors      Colors `json:"colors"`
	Reasoning   string `json:"reasoning"`
	Mood        string `json:"mood"`
	Recommended bool   `json:"recommended"`
}

// Preference — среднее по выбору пользователя, а не скрытый профиль.
type Preference struct {
	Samples    int     `json:"samples"`
	Hue        float64 `json:"hue"`
	Brightness float64 `json:"brightness"`
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func ValidateRequest(text string, preference *Preference) error {
	if !utf8.ValidString(text) || strings.TrimSpace(text) == "" || len(text) > MaxTextBytes {
		return errors.New("введите непустой текст длиной до 4096 байт")
	}
	if preference != nil && (preference.Samples < 3 || preference.Samples > 30 || math.IsNaN(preference.Hue) || math.IsInf(preference.Hue, 0) || preference.Hue < 0 || preference.Hue >= 360 || math.IsNaN(preference.Brightness) || math.IsInf(preference.Brightness, 0) || preference.Brightness < 0 || preference.Brightness > 1) {
		return errors.New("некорректная память цветовых предпочтений")
	}
	return nil
}

// Contrast считает относительную яркость sRGB, затем отношение WCAG.
func Contrast(a, b string) (float64, error) {
	x, err := luminance(a)
	if err != nil {
		return 0, err
	}
	y, err := luminance(b)
	if err != nil {
		return 0, err
	}
	if x < y {
		x, y = y, x
	}
	return (x + 0.05) / (y + 0.05), nil
}

func luminance(color string) (float64, error) {
	if !hexColor.MatchString(color) {
		return 0, errors.New("цвет должен иметь формат #RRGGBB")
	}
	var channels [3]float64
	for i := range channels {
		value, _ := strconv.ParseUint(color[1+i*2:3+i*2], 16, 8)
		v := float64(value) / 255
		if v <= 0.04045 {
			channels[i] = v / 12.92
		} else {
			channels[i] = math.Pow((v+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2], nil
}

// Normalize проверяет даже mock-ответы и исправляет текст при слабом контрасте.
func Normalize(palettes []Palette) ([]Palette, error) {
	if len(palettes) != 3 {
		return nil, errors.New("ожидались ровно три палитры")
	}
	result := make([]Palette, 3)
	recommended := -1
	names := map[string]bool{}
	for i, p := range palettes {
		if strings.TrimSpace(p.Name) == "" || len(p.Name) > 160 || strings.TrimSpace(p.Reasoning) == "" || len(p.Reasoning) > 1600 || strings.TrimSpace(p.Mood) == "" || len(p.Mood) > 240 || !utf8.ValidString(p.Name+p.Reasoning+p.Mood) {
			return nil, errors.New("неполное описание палитры")
		}
		if names[p.Name] {
			return nil, errors.New("названия палитр должны различаться")
		}
		names[p.Name] = true
		for _, color := range []string{p.Colors.Background, p.Colors.Primary, p.Colors.Secondary, p.Colors.Text, p.Colors.Accent} {
			if !hexColor.MatchString(color) {
				return nil, errors.New("некорректный цвет")
			}
		}
		ratio, _ := Contrast(p.Colors.Background, p.Colors.Text)
		if ratio < 4.5 {
			black, _ := Contrast(p.Colors.Background, "#000000")
			white, _ := Contrast(p.Colors.Background, "#FFFFFF")
			p.Colors.Text = "#000000"
			if white > black {
				p.Colors.Text = "#FFFFFF"
			}
			p.Reasoning += " Цвет текста скорректирован для контраста WCAG AA."
		}
		if p.Recommended && recommended < 0 {
			recommended = i
		}
		p.Recommended = false
		result[i] = p
	}
	if recommended < 0 {
		recommended = 0
	}
	result[recommended].Recommended = true
	return result, nil
}

func mockPalettes(text string, preference *Preference) []Palette {
	palettes := []Palette{
		{Name: "Тёплая бумага", Colors: Colors{"#FFF8ED", "#963F1C", "#E9D7BC", "#28231F", "#B25D36"}, Reasoning: "Тёплый фон и спокойный акцент помогают читать длинные баннеры.", Mood: "Тепло и собранно", Recommended: true},
		{Name: "Северный свет", Colors: Colors{"#EEF5FA", "#205C86", "#C1D7E7", "#1C3040", "#3C7890"}, Reasoning: "Прохладная гамма подчёркивает геометрию символьного рисунка.", Mood: "Свежо и спокойно"},
		{Name: "Ночной терминал", Colors: Colors{"#14221F", "#8ED8B1", "#294A40", "#E0F4E9", "#75BAA1"}, Reasoning: "Тёмный фон удобен для вечерней работы и сохраняет высокий контраст.", Mood: "Тихо и сосредоточенно"},
	}
	// Контекст меняет рекомендуемую палитру, но повторный запрос воспроизводим.
	lower := strings.ToLower(text)
	if strings.Contains(lower, "night") || strings.Contains(lower, "ноч") {
		palettes[0].Recommended = false
		palettes[2].Recommended = true
	}
	if preference != nil {
		for i := range palettes {
			hue := math.Mod(preference.Hue+float64(i)*24, 360)
			lightness := math.Max(0.24, math.Min(0.72, preference.Brightness))
			palettes[i].Colors.Primary = hslHex(hue, 0.58, lightness)
			palettes[i].Reasoning += fmt.Sprintf(" Учтена память: %d выбора, средний тон %.0f°, светлота %.0f%%.", preference.Samples, preference.Hue, preference.Brightness*100)
		}
	}
	return palettes
}

func hslHex(h, s, l float64) string {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return fmt.Sprintf("#%02X%02X%02X", int(math.Round((r+m)*255)), int(math.Round((g+m)*255)), int(math.Round((b+m)*255)))
}
