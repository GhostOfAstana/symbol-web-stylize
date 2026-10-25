// Package ascii превращает печатный ASCII-текст в баннер высотой восемь строк.
package ascii

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

const (
	// MaxInputBytes ограничивает размер пользовательского текста до обработки.
	MaxInputBytes = 4096
	// MaxOutputBytes не позволяет одному запросу занять неограниченную память.
	MaxOutputBytes = 1 << 20
	fontHeight     = 8
	fontCharacters = 95
	maxFontBytes   = 1 << 20
	maxGlyphWidth  = 128
)

var (
	ErrUnknownBanner        = errors.New("неизвестный шрифт: выберите standard, shadow или thinkertoy")
	ErrInputTooLong         = errors.New("текст превышает допустимые 4096 байт")
	ErrUnsupportedCharacter = errors.New("допустимы печатные ASCII-символы и переводы строк")
	ErrOutputTooLarge       = errors.New("готовый баннер превышает допустимый размер 1 МиБ")
)

type glyph [fontHeight]string
type font [fontCharacters]glyph

// Generator хранит проверенные шрифты; после создания их можно безопасно читать
// одновременно из нескольких HTTP-запросов.
type Generator struct {
	fonts map[string]font
}

// New загружает все три шрифта из файловой системы. Ошибка повреждённого ресурса
// обнаруживается при запуске сервера, до приёма пользовательских запросов.
func New(fsys fs.FS) (*Generator, error) {
	if fsys == nil {
		return nil, errors.New("не задана файловая система со шрифтами")
	}
	g := &Generator{fonts: make(map[string]font, 3)}
	for _, name := range []string{"standard", "shadow", "thinkertoy"} {
		file, err := fsys.Open(name + ".txt")
		if err != nil {
			return nil, fmt.Errorf("загрузка шрифта %s: %w", name, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxFontBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, fmt.Errorf("чтение шрифта %s: %w", name, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("закрытие шрифта %s: %w", name, closeErr)
		}
		if len(data) > maxFontBytes {
			return nil, fmt.Errorf("шрифт %s превышает допустимый размер", name)
		}
		parsed, err := parseFont(string(data))
		if err != nil {
			return nil, fmt.Errorf("повреждён шрифт %s: %w", name, err)
		}
		g.fonts[name] = parsed
	}
	return g, nil
}

func parseFont(data string) (font, error) {
	var result font
	data = strings.ReplaceAll(data, "\r\n", "\n")
	// Завершающий LF — разделитель строки, а не дополнительный элемент шрифта.
	lines := strings.Split(strings.TrimSuffix(data, "\n"), "\n")
	if len(lines) != fontCharacters*(fontHeight+1) {
		return result, fmt.Errorf("ожидалось 855 строк, получено %d", len(lines))
	}
	for character := range result {
		start := character * (fontHeight + 1)
		if lines[start] != "" {
			return result, fmt.Errorf("нет пустого разделителя перед символом %q", rune(character+32))
		}
		width := len(lines[start+1])
		if width == 0 || width > maxGlyphWidth {
			return result, fmt.Errorf("недопустимая ширина символа %q", rune(character+32))
		}
		for row := range fontHeight {
			line := lines[start+1+row]
			if len(line) != width {
				return result, fmt.Errorf("строки символа %q имеют разную ширину", rune(character+32))
			}
			for i := range line {
				if line[i] < 32 || line[i] > 126 {
					return result, fmt.Errorf("непечатный символ внутри шрифта")
				}
			}
			result[character][row] = line
		}
	}
	return result, nil
}

// Generate поддерживает обычные переводы строк LF, Windows CRLF и буквальную
// последовательность \n. Пробелы справа сохраняются: они выравнивают баннер.
func (g *Generator) Generate(text, banner string) (string, error) {
	if g == nil {
		return "", errors.New("генератор не инициализирован")
	}
	selected, ok := g.fonts[banner]
	if !ok {
		return "", ErrUnknownBanner
	}
	if len(text) > MaxInputBytes {
		return "", ErrInputTooLong
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, `\n`, "\n")
	for i := range text {
		if text[i] != '\n' && (text[i] < 32 || text[i] > 126) {
			return "", ErrUnsupportedCharacter
		}
	}
	if text == "" {
		return "", nil
	}
	lines := strings.Split(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	// Сначала считаем размер. Если результат слишком велик, не выделяем память
	// под него и не возвращаем частично сформированный баннер.
	outputSize := 0
	for _, line := range lines {
		if line == "" {
			outputSize++
		} else {
			outputSize += fontHeight
			for i := range line {
				outputSize += len(selected[line[i]-32][0]) * fontHeight
			}
		}
		if outputSize > MaxOutputBytes {
			return "", ErrOutputTooLarge
		}
	}
	var output strings.Builder
	output.Grow(outputSize)
	for _, line := range lines {
		if line == "" {
			output.WriteByte('\n')
			continue
		}
		for row := range fontHeight {
			for i := range line {
				output.WriteString(selected[line[i]-32][row])
			}
			output.WriteByte('\n')
		}
	}
	return output.String(), nil
}
