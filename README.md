# terskol-astro-guide

Desktop-приложение на WebView для управления DIO на Vecow ECX-1000-2G.

## Что есть
- 8 управляемых DO-портов (DO0..DO7, физические пины 11..18 20-пинового разъема).
- Для каждого порта:
  - ON / OFF
  - цветовая индикация: зеленый = включен, серый = выключен
  - поле подписи устройства + кнопка «Сохранить»
- Подписи сохраняются в `dio-labels.json`.

## Запуск

```bash
go run terskol-astro-guide.go
```

## Флаги
- `-port` HTTP порт (по умолчанию `8765`)
- `-directory` локальная директория для статики
- `-dio-value-path-template` явный путь-шаблон для DIO с `%d` (имеет максимальный приоритет)
- `-dio-linux-value-path-template` путь-шаблон DIO для Linux (по умолчанию `/sys/class/gpio/gpio%d/value`)
- `-dio-windows-value-path-template` путь-шаблон DIO для Windows (по умолчанию `C:\Vecow\ECX1K\dio%d.value`)
- `-labels-file` файл подписей (по умолчанию `dio-labels.json`)

## API
- `GET /api/state`
- `POST /api/power` body: `{ "port": 1, "power": "on" }`
- `POST /api/label` body: `{ "port": 1, "label": "Pump" }`
