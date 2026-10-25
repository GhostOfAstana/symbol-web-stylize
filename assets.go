// Package assets включает ресурсы в исполняемый файл, чтобы запуск не зависел
// от текущей папки и от наличия исходников рядом с сервером.
package assets

import "embed"

// FS содержит проверенные шрифты, HTML-шаблоны и статические файлы сайта.
//
//go:embed standard.txt shadow.txt thinkertoy.txt templates static
var FS embed.FS
