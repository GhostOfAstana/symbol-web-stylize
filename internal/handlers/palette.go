package handlers

import (
	"errors"
	"net/http"

	"github.com/GhostOfAstana/symbol-web-stylize/internal/ai"
)

func (a *app) suggestPalette(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Text       string         `json:"text"`
		Preference *ai.Preference `json:"preference,omitempty"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := ai.ValidateRequest(request.Text, request.Preference); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	palettes, err := a.palettes.Suggest(r.Context(), request.Text, request.Preference)
	if err != nil {
		if errors.Is(err, ai.ErrInvalidResponse) {
			jsonError(w, http.StatusInternalServerError, "Помощник вернул неполные или некорректные палитры. Попробуйте ещё раз или выберите готовую тему.")
			return
		}
		jsonError(w, http.StatusServiceUnavailable, "Помощник по цвету временно недоступен. Попробуйте ещё раз или выберите готовую тему.")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Palettes []ai.Palette `json:"palettes"`
		Mode     string       `json:"mode"`
	}{palettes, a.palettes.Mode()})
}
