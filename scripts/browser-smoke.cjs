"use strict";
// Дополнительная проверка использует уже установленный Playwright и браузер.
const assert = require("node:assert/strict"), path = require("node:path");
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || process.argv[4] || "playwright");
const { contrast } = require("../static/js/palette.js");
(async () => {
    const executable = process.env.BROWSER_EXECUTABLE || process.argv[5];
    const browser = await chromium.launch({ headless: true, ...(executable ? { executablePath: executable } : {}) });
    const failures = [];
    try {
        const page = await browser.newPage();
        page.on("pageerror", error => failures.push(error.message));
        const url = process.argv[2] || "http://localhost:8080";
        await page.goto(url, { waitUntil: "networkidle" });
        for (const width of [375, 768, 1920]) {
            await page.setViewportSize({ width, height: 1000 });
            assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `переполнение страницы при ${width}`);
            if (process.argv[3])
                await page.screenshot({ path: path.join(process.argv[3], `stylize-${width}.png`), fullPage: true });
        }
        await page.setViewportSize({ width: 1280, height: 1000 });
        await page.locator("#text-input").fill("Hello");
        await page.locator("#banner").selectOption("standard");
        await Promise.all([page.waitForURL("**/symbol-art"), page.locator("#generate-button").click()]);
        assert.ok((await page.locator("#art-output").textContent()).length > 30);
        await page.locator("#suggest-palette").click();
        await page.locator(".palette-card").nth(2).waitFor();
        assert.equal(await page.locator(".palette-card").count(), 3);
        for (let i = 0; i < 3; i++)
            await page.locator(".palette-card button").nth(i).click();
        assert.match(await page.locator("#memory-status").textContent(), /средний тон/);
        const theme = await page.evaluate(() => localStorage.getItem("symbol.theme"));
        const preferenceRequest = page.waitForRequest(r => r.url().endsWith("/api/suggest-palette") && r.method() === "POST");
        await page.locator("#suggest-palette").click();
        assert.equal((await preferenceRequest).postDataJSON().preference.samples, 3);
        await page.waitForFunction(() => !document.querySelector("#suggest-palette").disabled);
        await page.locator("#save-theme").click();
        await page.goto(url, { waitUntil: "networkidle" });
        assert.equal(await page.evaluate(() => localStorage.getItem("symbol.theme")), theme);
        const colors = await page.evaluate(() => { const s = getComputedStyle(document.documentElement); return { background: s.getPropertyValue("--bg-color").trim(), text: s.getPropertyValue("--text-color").trim(), primary: s.getPropertyValue("--primary-color").trim(), onPrimary: s.getPropertyValue("--on-primary").trim() }; });
        assert.ok(contrast(colors.background, colors.text) >= 4.5);
        assert.ok(contrast(colors.primary, colors.onPrimary) >= 4.5);
        assert.equal(await page.locator("#saved-palettes button").count(), 1);
        await page.locator("#dark-toggle").click();
        assert.equal(await page.locator("#dark-toggle").getAttribute("aria-pressed"), "false");
        await page.locator("#preset-dark").click();
        assert.equal(await page.locator("#dark-toggle").getAttribute("aria-pressed"), "true");
        await page.locator("#reset-theme").click();
        await page.locator("#clear-memory").click();
        assert.match(await page.locator("#memory-status").textContent(), /0 из 3/);
        await page.locator(".custom-theme summary").click();
        await page.locator("#custom-name").fill("Мой контраст");
        await page.locator('input[name="background"]').fill("#ffffff");
        await page.locator('input[name="text"]').fill("#ffffff");
        await page.locator('input[name="primary"]').fill("#ffffff");
        await page.locator("#custom-palette-form button").click();
        assert.equal(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--text-color").trim()), "#000000");
        assert.equal(await page.locator("#saved-palettes button").count(), 2);
        // Произвольные значения хранилища не должны ломать загрузку.
        await page.evaluate(() => { localStorage.setItem("symbol.theme", "{"); localStorage.setItem("symbol.saved", JSON.stringify([{ "name": "bad", "colors": {} }])); localStorage.setItem("symbol.history", JSON.stringify([null, "url(x)"])); });
        await page.reload({ waitUntil: "networkidle" });
        assert.equal(await page.locator("#saved-palettes button").count(), 0);
        await page.locator("#text-input").focus();
        await page.keyboard.press("Tab");
        assert.equal(await page.evaluate(() => document.activeElement.id), "banner");
        assert.ok(await page.evaluate(() => getComputedStyle(document.activeElement).outlineStyle !== "none"));
        // Запоздавший ответ предыдущему тексту не должен предлагаться для нового.
        const canned = await (await page.request.post(url + "/api/suggest-palette", { data: { text: "Old" } })).json();
        let release, intercepted;
        const gate = new Promise(resolve => { release = resolve; }), started = new Promise(resolve => { intercepted = resolve; });
        await page.route("**/api/suggest-palette", async (route) => { intercepted(); await gate; await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(canned) }); });
        await page.locator("#text-input").fill("Old");
        await page.locator("#suggest-palette").click();
        await started;
        await page.locator("#text-input").fill("New");
        release();
        await page.waitForFunction(() => !document.querySelector("#suggest-palette").disabled);
        assert.equal(await page.locator(".palette-card").count(), 0);
        assert.match(await page.locator("#palette-status").textContent(), /Текст изменился/);
        await page.unroute("**/api/suggest-palette");
        await page.route("**/api/suggest-palette", route => route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "Помощник временно недоступен." }) }));
        await page.locator("#suggest-palette").click();
        await page.waitForFunction(() => !document.querySelector("#suggest-palette").disabled);
        assert.match(await page.locator("#palette-status").textContent(), /временно недоступен/);
        assert.deepEqual(failures, []);
        console.log(JSON.stringify({ result: "ok", viewports: [375, 768, 1920], checks: ["форма", "палитры", "контраст", "память", "localStorage", "темы", "фокус", "ошибки JavaScript"] }));
    }
    finally {
        await browser.close();
    }
})().catch(error => { console.error(error); process.exitCode = 1; });
