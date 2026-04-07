# terskol-astro-guide

Desktop-приложение на WebView для управления DIO/DO портами.

## Что есть
- 20 портов: 10 входящих `DI` и 10 исходящих `DO`.
- Для каждого порта:
  - компактная строка с одной toggle-кнопкой ON/OFF (жирная надпись состояния)
  - подсветка всей строки: зеленый = включено, красный = выключено
  - редактирование названия через кнопку-карандаш `✏️` и сохранение через `💾`
- Состояние и названия сохраняются в SQLite (`dio-state.sqlite` по умолчанию).

## Запуск

```bash
go run terskol-astro-guide.go
```

## Флаги
- `-port` HTTP порт (по умолчанию `8765`)
- `-directory` локальная директория для статики
- `-dio-value-path-template` путь-шаблон для файла DIO (по умолчанию `/sys/class/gpio/gpio%d/value`)
- `-db-file` файл SQLite БД (по умолчанию `dio-state.sqlite`)

## API
- `GET /api/state`
- `POST /api/power` body: `{ "direction": "DO", "port": 1, "power": "on" }`
- `POST /api/label` body: `{ "direction": "DI", "port": 1, "label": "Door sensor" }`
