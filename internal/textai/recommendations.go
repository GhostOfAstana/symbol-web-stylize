package textai

import "strings"

type Alternative struct {
	Banner string  `json:"banner"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

type Recommendation struct {
	Recommended  string        `json:"recommended"`
	Reasoning    string        `json:"reasoning"`
	Alternatives []Alternative `json:"alternatives"`
}

// recommend использует явно заданные правила. Регистрация, модель и сеть для
// этого выбора не нужны; одинаковый текст всегда даёт одинаковый ответ.
func recommend(text string) Recommendation {
	letters, upper := 0, 0
	for _, char := range text {
		if char >= 'A' && char <= 'Z' {
			letters++
			upper++
		} else if char >= 'a' && char <= 'z' {
			letters++
		}
	}
	chosen, reason := "thinkertoy", "Короткий текст со смешанным регистром подходит для декоративного шрифта."
	if (letters > 0 && letters == upper) || len(strings.TrimSpace(text)) <= 6 {
		chosen, reason = "shadow", "Короткий текст или заглавные буквы хорошо выделяются объёмным шрифтом."
	} else if len(text) > 12 {
		chosen, reason = "standard", "Длинный текст удобнее читать в простом шрифте."
	}
	alternatives := make([]Alternative, 0, 2)
	for _, name := range []string{"standard", "shadow", "thinkertoy"} {
		if name != chosen {
			alternatives = append(alternatives, Alternative{Banner: name, Score: 0.7 - float64(len(alternatives))*0.2, Reason: "Допустимый вариант: итог зависит от желаемого оформления."})
		}
	}
	return Recommendation{Recommended: chosen, Reasoning: reason, Alternatives: alternatives}
}
