# terskol-astro-guide

Минимальное desktop-приложение на Go + WebView.

## Что делает
- Поднимает локальный HTTP сервер.
- Открывает `webview` окно.
- Показывает одну кнопку управления состоянием `on/off`.

## Запуск на macOS

```bash
go run .
```

Если окно не открывается, обычно причина в отсутствии системных библиотек WebKit/CGO в окружении сборки.

Проверка:

```bash
go env CGO_ENABLED
go run .
```

Для обычного macOS окружения `CGO_ENABLED` должен быть `1`.

## Структура
- `terskol-astro-guide.go` — вся бизнес-логика приложения в одном файле.
- `static/index.html` — UI, встроенный через `go:embed`.
- `third_party/webview_go/` — локальная зависимость webview.
