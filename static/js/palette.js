"use strict";
// Все сохранённые данные считаются внешним вводом и проходят проверку повторно.
(() => {
    const KEYS = ["background", "primary", "secondary", "text", "accent"];
    const HEX = /^#[0-9a-f]{6}$/i;
    const light = { name: "Тёплая бумага", colors: { background: "#FFF8ED", primary: "#963F1C", secondary: "#E9D7BC", text: "#28231F", accent: "#B25D36" }, reasoning: "Спокойная светлая тема.", mood: "Тепло" };
    const dark = { name: "Ночной терминал", colors: { background: "#14221F", primary: "#8ED8B1", secondary: "#294A40", text: "#E0F4E9", accent: "#75BAA1" }, reasoning: "Высокий контраст для вечерней работы.", mood: "Сосредоточенно" };
    function luminance(hex) { const c = [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16) / 255).map(v => v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4); return c[0] * .2126 + c[1] * .7152 + c[2] * .0722; }
    function contrast(a, b) { if (!HEX.test(a) || !HEX.test(b))
        return 0; const x = luminance(a), y = luminance(b); return (Math.max(x, y) + .05) / (Math.min(x, y) + .05); }
    function readable(color) { return contrast(color, "#000000") >= contrast(color, "#FFFFFF") ? "#000000" : "#FFFFFF"; }
    function validatePalette(value) {
        if (!value || typeof value.name !== "string" || !value.name.trim() || value.name.length > 160 || !value.colors || !KEYS.every(k => typeof value.colors[k] === "string" && HEX.test(value.colors[k])))
            return null;
        const colors = Object.fromEntries(KEYS.map(k => [k, value.colors[k]]));
        if (contrast(colors.background, colors.text) < 4.5)
            colors.text = readable(colors.background);
        return { name: value.name, colors, reasoning: typeof value.reasoning === "string" ? value.reasoning.slice(0, 1600) : "Сохранённая палитра.", mood: typeof value.mood === "string" ? value.mood.slice(0, 240) : "Ваш выбор", recommended: value.recommended === true };
    }
    function hsl(hex) {
        const [r, g, b] = [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16) / 255), max = Math.max(r, g, b), min = Math.min(r, g, b), delta = max - min;
        let hue = 0;
        if (delta) {
            if (max === r)
                hue = ((g - b) / delta) % 6;
            else if (max === g)
                hue = (b - r) / delta + 2;
            else
                hue = (r - g) / delta + 4;
            hue *= 60;
            if (hue < 0)
                hue += 360;
        }
        return { hue, brightness: (max + min) / 2 };
    }
    function preference(history) {
        const valid = Array.isArray(history) ? history.filter(v => typeof v === "string" && HEX.test(v)).slice(-30) : [];
        if (valid.length < 3)
            return null;
        const values = valid.map(hsl);
        let x = 0, y = 0, total = 0;
        for (const v of values) {
            x += Math.cos(v.hue * Math.PI / 180);
            y += Math.sin(v.hue * Math.PI / 180);
            total += v.brightness;
        }
        // Круговое среднее не превращает близкие красные 359° и 1° в голубой 180°.
        let hue = (Math.atan2(y, x) * 180 / Math.PI + 360) % 360;
        if (Math.hypot(x, y) < 1e-8)
            hue = values[values.length - 1].hue;
        return { samples: valid.length, hue, brightness: total / valid.length };
    }
    if (typeof module !== "undefined" && module.exports)
        module.exports = { contrast, readable, validatePalette, preference, hsl };
    if (typeof document === "undefined")
        return;
    const status = document.querySelector("#palette-status"), options = document.querySelector("#palette-options"), savedContainer = document.querySelector("#saved-palettes"), memory = document.querySelector("#memory-status");
    if (!status || !options || !savedContainer || !memory)
        return;
    let storageDenied = false;
    function read(key, fallback) { try {
        const raw = localStorage.getItem(key);
        if (!raw || raw.length > 128000)
            return fallback;
        return JSON.parse(raw);
    }
    catch {
        storageDenied = true;
        return fallback;
    } }
    function write(key, value) { try {
        localStorage.setItem(key, JSON.stringify(value));
        return true;
    }
    catch {
        storageDenied = true;
        return false;
    } }
    function remove(key) { try {
        localStorage.removeItem(key);
    }
    catch {
        storageDenied = true;
    } }
    function message(text, kind = "success") { status.hidden = false; status.className = `notice ${kind}`; status.textContent = text; }
    let history = read("symbol.history", []);
    if (!Array.isArray(history))
        history = [];
    history = history.filter(v => typeof v === "string" && HEX.test(v)).slice(-30);
    let saved = read("symbol.saved", []);
    if (!Array.isArray(saved))
        saved = [];
    saved = saved.map(validatePalette).filter(Boolean).slice(-12);
    let current = validatePalette(read("symbol.theme", null)) || light;
    function updateMemory() { const pref = preference(history); memory.textContent = pref ? `Память стиля: ${pref.samples} выбора; средний тон ${Math.round(pref.hue)}°, светлота ${Math.round(pref.brightness * 100)}%. Следующий запрос учтёт их.` : `Память стиля: ${history.length} из 3 выборов. После третьего помощник учтёт средний тон и светлоту.`; }
    function apply(palette, { record = true, announce = true } = {}) {
        const safe = validatePalette(palette);
        if (!safe) {
            message("Эту палитру применить нельзя: проверьте цвета.", "error");
            return;
        }
        current = safe;
        const variables = { background: "--bg-color", primary: "--primary-color", secondary: "--secondary-color", text: "--text-color", accent: "--accent-color" };
        for (const key of KEYS)
            document.documentElement.style.setProperty(variables[key], safe.colors[key]);
        document.documentElement.style.setProperty("--on-primary", readable(safe.colors.primary));
        const isDark = luminance(safe.colors.background) < .18;
        document.documentElement.style.colorScheme = isDark ? "dark" : "light";
        const toggle = document.querySelector("#dark-toggle");
        toggle.setAttribute("aria-pressed", String(isDark));
        toggle.textContent = isDark ? "Светлая тема" : "Тёмная тема";
        write("symbol.theme", safe);
        if (record) {
            history.push(safe.colors.primary);
            history = history.slice(-30);
            write("symbol.history", history);
        }
        updateMemory();
        if (announce)
            message(storageDenied ? "Тема применена. Браузер запретил сохранение; после перезагрузки выбор может сброситься." : `Применена палитра «${safe.name}». Контраст текста ${contrast(safe.colors.background, safe.colors.text).toFixed(2)}:1.`);
    }
    function renderSaved() {
        savedContainer.replaceChildren();
        if (!saved.length) {
            const p = document.createElement("p");
            p.textContent = "Здесь появятся сохранённые палитры. Можно хранить до 12 вариантов.";
            savedContainer.append(p);
            return;
        }
        for (const palette of saved) {
            const button = document.createElement("button");
            button.type = "button";
            button.className = "quiet";
            button.textContent = palette.name;
            button.setAttribute("aria-label", `Применить сохранённую палитру ${palette.name}`);
            button.addEventListener("click", () => apply(palette));
            savedContainer.append(button);
        }
    }
    function save(palette) {
        const safe = validatePalette(palette);
        if (!safe)
            return;
        const signature = JSON.stringify(safe.colors);
        saved = saved.filter(p => JSON.stringify(p.colors) !== signature);
        saved.push(safe);
        saved = saved.slice(-12);
        write("symbol.saved", saved);
        renderSaved();
        message(storageDenied ? "Палитра добавлена на эту страницу; браузер запретил постоянное хранение." : `Палитра «${safe.name}» сохранена.`);
    }
    function showPalettes(palettes) {
        options.replaceChildren();
        for (const palette of palettes) {
            const card = document.createElement("article");
            card.className = "palette-card";
            const label = document.createElement("span");
            label.className = "palette-label";
            label.textContent = palette.recommended ? "РЕКОМЕНДУЕМ" : "ЕЩЁ ОДНА ИДЕЯ";
            const title = document.createElement("h3");
            title.textContent = palette.name;
            const swatches = document.createElement("div");
            swatches.className = "swatches";
            swatches.setAttribute("aria-label", "Цвета палитры");
            for (const key of KEYS) {
                const swatch = document.createElement("span");
                swatch.className = "swatch";
                swatch.style.backgroundColor = palette.colors[key];
                swatch.title = palette.colors[key];
                swatch.setAttribute("aria-label", palette.colors[key]);
                swatches.append(swatch);
            }
            const reasoning = document.createElement("p");
            reasoning.textContent = palette.reasoning;
            const mood = document.createElement("p");
            mood.textContent = `Настроение: ${palette.mood}`;
            const button = document.createElement("button");
            button.type = "button";
            button.className = "quiet";
            button.textContent = "Применить";
            button.setAttribute("aria-label", `Применить палитру ${palette.name}`);
            button.addEventListener("click", () => apply(palette));
            card.append(label, title, swatches, reasoning, mood, button);
            options.append(card);
        }
    }
    const suggest = document.querySelector("#suggest-palette");
    suggest.addEventListener("click", async () => {
        const text = document.querySelector("#text-input").value;
        if (!text.trim()) {
            message("Введите текст, чтобы подобрать настроение.", "error");
            return;
        }
        suggest.disabled = true;
        suggest.setAttribute("aria-busy", "true");
        message("Подбираем три палитры…", "loading");
        const controller = new AbortController(), timer = setTimeout(() => controller.abort(), 12000);
        try {
            const body = { text }, pref = preference(history);
            if (pref)
                body.preference = pref;
            const response = await fetch("/api/suggest-palette", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body), signal: controller.signal });
            const data = await response.json();
            if (!response.ok)
                throw new Error(typeof data.error === "string" ? data.error : "Помощник временно недоступен.");
            if (!Array.isArray(data.palettes) || data.palettes.length !== 3)
                throw new Error("Сервер вернул неполный набор палитр.");
            const palettes = data.palettes.map(validatePalette);
            if (palettes.some(v => !v))
                throw new Error("Сервер вернул некорректные цвета.");
            if (document.querySelector("#text-input").value !== text) {
                message("Текст изменился. Запросите палитры ещё раз.");
                return;
            }
            showPalettes(palettes);
            message(data.mode === "mock" ? "Готовы три палитры из демонстрационного набора. Их можно применять и сохранять." : "Готовы три палитры от LLM. Контраст проверен сервером.");
        }
        catch (error) {
            message(error.name === "AbortError" ? "Помощник отвечает слишком долго. Выберите готовую тему или повторите запрос." : error.message || "Не удалось связаться с помощником.", "error");
        }
        finally {
            clearTimeout(timer);
            suggest.disabled = false;
            suggest.removeAttribute("aria-busy");
        }
    });
    document.querySelector("#preset-light").addEventListener("click", () => apply(light));
    document.querySelector("#preset-dark").addEventListener("click", () => apply(dark));
    document.querySelector("#dark-toggle").addEventListener("click", () => apply(luminance(current.colors.background) < .18 ? light : dark));
    document.querySelector("#save-theme").addEventListener("click", () => save(current));
    document.querySelector("#reset-theme").addEventListener("click", () => { remove("symbol.theme"); apply(light, { record: false }); });
    document.querySelector("#clear-memory").addEventListener("click", () => { history = []; remove("symbol.history"); updateMemory(); message("Память предпочтений очищена."); });
    document.querySelector("#clear-saved").addEventListener("click", () => { saved = []; remove("symbol.saved"); renderSaved(); message("Сохранённые палитры удалены."); });
    document.querySelector("#custom-palette-form").addEventListener("submit", event => {
        event.preventDefault();
        const form = event.currentTarget, colors = Object.fromEntries(KEYS.map(key => [key, form.elements.namedItem(key).value]));
        const palette = { name: document.querySelector("#custom-name").value.trim(), colors, reasoning: "Ваша собственная палитра. Контраст текста автоматически проверен.", mood: "Ваш выбор" };
        if (!validatePalette(palette)) {
            message("Введите название и пять корректных цветов.", "error");
            return;
        }
        apply(palette);
        save(current);
    });
    apply(current, { record: false, announce: false });
    renderSaved();
    updateMemory();
})();
