"use strict";
// Нативная отправка формы работает и без JavaScript; скрипт добавляет обратную связь.
(() => {
    const form = document.querySelector("#art-form"), button = document.querySelector("#generate-button");
    if (form && button) {
        form.addEventListener("submit", () => { button.disabled = true; button.textContent = "Создаём баннер…"; button.setAttribute("aria-busy", "true"); });
        window.addEventListener("pageshow", () => { button.disabled = false; button.textContent = "Создать баннер ↗"; button.removeAttribute("aria-busy"); });
    }
    const copy = document.querySelector("#copy-art");
    if (copy) {
        copy.addEventListener("click", async () => { const status = document.querySelector("#copy-status"); try {
            if (!navigator.clipboard)
                throw new Error();
            await navigator.clipboard.writeText(document.querySelector("#art-output").textContent);
            status.textContent = "Баннер скопирован.";
        }
        catch {
            status.textContent = "Браузер не разрешил копирование. Выделите баннер и скопируйте вручную.";
        } });
    }
})();
