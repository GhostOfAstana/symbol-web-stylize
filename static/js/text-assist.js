/* Подсказки используют textContent: ответ модели никогда не становится HTML. */
(() => {
    "use strict";
    const input = document.getElementById("text-input");
    const banner = document.getElementById("banner");
    const list = document.getElementById("suggestions") || document.getElementById("suggestions-dropdown");
    const status = document.getElementById("assist-status");
    const recommendButton = document.getElementById("recommend-banner");
    const variationsButton = document.getElementById("variations");
    if (!input || !list)
        return;
    let timer;
    let pending;
    let sequence = 0;
    if (status)
        status.setAttribute("aria-live", "polite");
    function announce(message) {
        if (status)
            status.textContent = message;
    }
    function cancelSuggestions() {
        window.clearTimeout(timer);
        sequence += 1;
        if (pending)
            pending.abort();
        pending = null;
    }
    async function request(path, signal) {
        let response;
        try {
            response = await fetch(path, {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ text: input.value }),
                signal,
            });
        }
        catch (error) {
            if (error.name === "AbortError")
                throw error;
            throw new Error("Не удалось связаться с сервером. Проверьте соединение.");
        }
        let data;
        try {
            data = await response.json();
        }
        catch {
            throw new Error("Сервер вернул непонятный ответ. Повторите запрос.");
        }
        if (!response.ok)
            throw new Error(data.error || "Не удалось получить подсказки.");
        return data;
    }
    function offer(text, chosenBanner, description) {
        const button = document.createElement("button");
        button.type = "button";
        button.className = "text-assist-option";
        button.textContent = description ? `${text} — ${description}` : text;
        button.addEventListener("click", () => {
            cancelSuggestions();
            input.value = text;
            if (banner && chosenBanner) {
                banner.value = chosenBanner;
                banner.dispatchEvent(new Event("change", { bubbles: true }));
            }
            list.replaceChildren();
            announce("Текст выбран. Можно создавать баннер.");
            input.focus();
        });
        list.appendChild(button);
    }
    input.addEventListener("input", () => {
        cancelSuggestions();
        list.replaceChildren();
        if (input.value.trim().length < 3) {
            announce("Для подсказок введите хотя бы три ASCII-символа.");
            return;
        }
        const current = sequence;
        timer = window.setTimeout(async () => {
            pending = new AbortController();
            announce("Подбираем продолжения текста…");
            try {
                const data = await request("/api/suggest", pending.signal);
                if (current !== sequence)
                    return;
                list.replaceChildren();
                data.suggestions.forEach((text) => offer(text));
                announce("Выберите подсказку или продолжайте вводить свой текст.");
            }
            catch (error) {
                if (error.name !== "AbortError" && current === sequence)
                    announce(error.message);
            }
        }, 300);
    });
    if (recommendButton)
        recommendButton.addEventListener("click", async () => {
            cancelSuggestions();
            recommendButton.disabled = true;
            announce("Выбираем подходящий шрифт…");
            try {
                const data = await request("/api/recommend-banner");
                if (banner) {
                    banner.value = data.recommended;
                    banner.dispatchEvent(new Event("change", { bubbles: true }));
                }
                announce(`${data.recommended}: ${data.reasoning}`);
            }
            catch (error) {
                announce(error.message);
            }
            finally {
                recommendButton.disabled = false;
            }
        });
    if (variationsButton)
        variationsButton.addEventListener("click", async () => {
            cancelSuggestions();
            const current = sequence;
            variationsButton.disabled = true;
            announce("Подбираем варианты оформления…");
            try {
                const data = await request("/api/variations");
                if (current !== sequence)
                    return;
                list.replaceChildren();
                data.variations.forEach((item) => offer(item.text, item.suggested_banner, item.description));
                announce("Выберите вариант: текст и шрифт заполнятся автоматически.");
            }
            catch (error) {
                if (current === sequence)
                    announce(error.message);
            }
            finally {
                variationsButton.disabled = false;
            }
        });
})();
