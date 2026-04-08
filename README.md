# chicha-astro-control

WebView desktop app for monitoring DI and controlling DO channels on Vecow-class DIO hosts.

## 1) What the app does

- Shows 8 DI channels (`DI1..DI8`) with:
  - signal state (`on` / `off`)
  - measured voltage text
  - estimated signal frequency (Hz)
- Controls 8 DO channels (`DO1..DO8`) with:
  - power state (`on` / `off`)
  - PWM duty (`0..100%`)
- Stores channel labels and output state in a JSON config file.
- Opens repository link from UI in external system browser.

Pin mapping used in UI:
- `DI1..DI8` → terminal block pins `1..8`
- `DO1..DO8` → terminal block pins `11..18`


## 2) Linux & MacOS:

```bash
go build -o /usr/local/bin/chicha-astro-control chicha-astro-control.go; chmod +x /usr/local/bin/chicha-astro-control; chicha-astro-control; 
```

## 3) Windows:

```bash
GOOS=windows GOARCH=amd64 go build -ldflags="-H windowsgui" -o chicha-astro-control.exe chicha-astro-control.go
```

## 4) Optional runtime flags

Only three flags are supported:

- `-DI` — DI path template with `%d`
- `-DO` — DO path template with `%d`
- `-config` — path to JSON settings file


## 5) If `-DI`/`-DO` are not provided, defaults depend on OS:

- Linux: `/sys/class/gpio/gpio%d/value`
- Windows: `C:\Vecow\ECX1K\di%d.value` and `C:\Vecow\ECX1K\do%d.value`
- macOS: `/tmp/astro-control/di%d.value` and `/tmp/astro-control/do%d.value`

## 6) Hardware notes

- DI and DO directions are treated as fixed runtime roles.
- Output voltage visualization uses a `0.0V..3.3V` scale derived from PWM duty.
