"use strict";
const test = require("node:test"), assert = require("node:assert/strict");
const { contrast, readable, validatePalette, preference, hsl } = require("./palette.js");
test("контраст и цвет подписи кнопки", () => { assert.equal(contrast("#000000", "#FFFFFF"), 21); for (const color of ["#000000", "#FFFFFF", "#777777", "#FEFA00", "#205C86"]) {
    assert.ok(contrast(color, readable(color)) >= 4.5);
} });
test("проверка сохранённых цветов и исправление текста", () => { const p = { name: "Тест", colors: { background: "#FFFFFF", primary: "#FFFFFF", secondary: "#FFFFFF", text: "#FFFFFF", accent: "#FFFFFF" } }; const safe = validatePalette(p); assert.equal(safe.colors.text, "#000000"); assert.equal(p.colors.text, "#FFFFFF"); for (const bad of [null, [], {}, "x", { name: "x", colors: { background: "url(x)" } }])
    assert.equal(validatePalette(bad), null); });
test("память начинается после третьего выбора", () => { assert.equal(preference(["#FF0000", "#FF0000"]), null); const p = preference(["#FF0000", "#FF0000", "#FF0000"]); assert.equal(p.samples, 3); assert.equal(p.hue, 0); assert.equal(p.brightness, .5); });
test("круговое среднее и ограничение истории", () => { const p = preference(["#FF0000", "#FF0011", "#FF1100"]); assert.ok(p.hue < 5 || p.hue > 355); assert.equal(preference(Array(50).fill("#FF0000")).samples, 30); assert.equal(preference(["bad", null, "#00FF00"]), null); });
test("основные HSL-тона", () => { assert.equal(hsl("#00FF00").hue, 120); assert.equal(hsl("#0000FF").hue, 240); });
