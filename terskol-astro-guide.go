package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	_ "terskol-astro-guide/pkg/sqlitecli"

	"github.com/webview/webview"
)

// =============================
// Embedded static assets.
// =============================

//go:embed static/*
var staticFiles embed.FS

var (
	portFlag                 = flag.Int("port", 8765, "web server port")
	directoryFlag            = flag.String("directory", ".", "directory to serve files from")
	dioValuePathTemplateFlag = flag.String("dio-value-path-template", "/sys/class/gpio/gpio%d/value", "DIO value file path template")
	databaseFileFlag         = flag.String("db-file", "dio-state.sqlite", "path to sqlite database file")
)

const portsPerDirection = 10

// =============================
// Domain model.
// =============================

type portState struct {
	Port      int    `json:"port"`
	Direction string `json:"direction"`
	Power     string `json:"power"`
	Label     string `json:"label"`
}

type appState struct {
	Ports []portState `json:"ports"`
}

type setPowerRequest struct {
	Port      int    `json:"port"`
	Direction string `json:"direction"`
	Power     string `json:"power"`
}

type setLabelRequest struct {
	Port      int    `json:"port"`
	Direction string `json:"direction"`
	Label     string `json:"label"`
}

type stateCommand struct {
	kind      string
	port      int
	direction string
	power     string
	label     string
	reply     chan stateReply
}

type stateReply struct {
	state appState
	err   error
}

// =============================
// Main entry point.
// =============================

func main() {
	flag.Parse()

	database, err := sql.Open("sqlitecli", *databaseFileFlag)
	if err != nil {
		log.Fatalf("startup: database open failed: %v", err)
	}
	defer database.Close()

	if err := prepareDatabase(database); err != nil {
		log.Fatalf("startup: database init failed: %v", err)
	}

	stateCommands := make(chan stateCommand)
	go runStateOwner(stateCommands, database, *dioValuePathTemplateFlag)

	http.HandleFunc("/api/state", handleGetState(stateCommands))
	http.HandleFunc("/api/power", handleSetPower(stateCommands))
	http.HandleFunc("/api/label", handleSetLabel(stateCommands))
	http.HandleFunc("/", handleRequest)

	address := fmt.Sprintf(":%d", *portFlag)
	log.Printf("startup: starting HTTP server on http://localhost%s", address)
	go func() {
		if err := http.ListenAndServe(address, nil); err != nil {
			log.Printf("shutdown: HTTP server stopped: %v", err)
		}
	}()

	window := webview.New(false)
	defer window.Destroy()
	window.SetTitle("DIO/DO Control · ECX-1000-2G")
	window.SetSize(980, 760, webview.HintNone)
	window.Navigate("http://localhost" + address)

	log.Printf("webview: window started")
	window.Run()
	log.Printf("shutdown: webview stopped")
}

// =============================
// State owner goroutine.
// =============================

func runStateOwner(stateCommands <-chan stateCommand, database *sql.DB, dioValuePathTemplate string) {
	state, err := loadStateFromDatabase(database)
	if err != nil {
		log.Printf("state: database read failed, fallback to defaults: %v", err)
		state = buildDefaultState()
	}
	log.Printf("state: owner started with %d ports", len(state.Ports))

	for command := range stateCommands {
		switch command.kind {
		case "get":
			command.reply <- stateReply{state: cloneState(state)}
		case "set_power":
			resultState, err := applyPower(state, command.port, command.direction, command.power, dioValuePathTemplate)
			if err != nil {
				command.reply <- stateReply{state: cloneState(state), err: err}
				continue
			}
			singlePortState, found := findPort(resultState, command.port, command.direction)
			if err := savePortToDatabase(database, singlePortState, found); err != nil {
				command.reply <- stateReply{state: cloneState(state), err: err}
				continue
			}
			state = resultState
			command.reply <- stateReply{state: cloneState(state)}
		case "set_label":
			resultState, err := applyLabel(state, command.port, command.direction, command.label)
			if err != nil {
				command.reply <- stateReply{state: cloneState(state), err: err}
				continue
			}
			singlePortState, found := findPort(resultState, command.port, command.direction)
			if err := savePortToDatabase(database, singlePortState, found); err != nil {
				command.reply <- stateReply{state: cloneState(state), err: err}
				continue
			}
			state = resultState
			command.reply <- stateReply{state: cloneState(state)}
		default:
			command.reply <- stateReply{state: cloneState(state), err: errors.New("unknown command")}
		}
	}
}

// =============================
// Database subsystem.
// =============================

func prepareDatabase(database *sql.DB) error {
	if _, err := database.Exec(`
		CREATE TABLE IF NOT EXISTS ports (
			port INTEGER NOT NULL,
			direction TEXT NOT NULL,
			power TEXT NOT NULL,
			label TEXT NOT NULL,
			PRIMARY KEY (port, direction)
		)
	`); err != nil {
		return err
	}

	for _, direction := range []string{"DI", "DO"} {
		for portNumber := 1; portNumber <= portsPerDirection; portNumber++ {
			defaultLabel := fmt.Sprintf("%s %d", direction, portNumber)
			if _, err := database.Exec(`INSERT OR IGNORE INTO ports(port, direction, power, label) VALUES (?, ?, 'off', ?)`, portNumber, direction, defaultLabel); err != nil {
				return err
			}
		}
	}
	return nil
}

func loadStateFromDatabase(database *sql.DB) (appState, error) {
	rows, err := database.Query(`SELECT port, direction, power, label FROM ports ORDER BY direction, port`)
	if err != nil {
		return appState{}, err
	}
	defer rows.Close()

	ports := make([]portState, 0, portsPerDirection*2)
	for rows.Next() {
		var singlePortState portState
		if err := rows.Scan(&singlePortState.Port, &singlePortState.Direction, &singlePortState.Power, &singlePortState.Label); err != nil {
			return appState{}, err
		}
		ports = append(ports, singlePortState)
	}
	if err := rows.Err(); err != nil {
		return appState{}, err
	}

	return appState{Ports: ports}, nil
}

func savePortToDatabase(database *sql.DB, singlePortState portState, found bool) error {
	if !found {
		return errors.New("port not found")
	}
	_, err := database.Exec(`UPDATE ports SET power = ?, label = ? WHERE port = ? AND direction = ?`, singlePortState.Power, singlePortState.Label, singlePortState.Port, singlePortState.Direction)
	return err
}

// =============================
// State transition subsystem.
// =============================

func buildDefaultState() appState {
	ports := make([]portState, 0, portsPerDirection*2)
	for _, direction := range []string{"DI", "DO"} {
		for portNumber := 1; portNumber <= portsPerDirection; portNumber++ {
			ports = append(ports, portState{Port: portNumber, Direction: direction, Power: "off", Label: fmt.Sprintf("%s %d", direction, portNumber)})
		}
	}
	return appState{Ports: ports}
}

func applyPower(state appState, port int, direction string, nextPower string, dioValuePathTemplate string) (appState, error) {
	if port < 1 || port > portsPerDirection {
		return state, errors.New("invalid port")
	}
	if direction != "DI" && direction != "DO" {
		return state, errors.New("direction must be DI or DO")
	}
	if nextPower != "on" && nextPower != "off" {
		return state, errors.New("power must be on or off")
	}

	if err := writeDIOPower(port, direction, nextPower, dioValuePathTemplate); err != nil {
		return state, err
	}

	nextState := cloneState(state)
	index, found := findPortIndex(nextState, port, direction)
	if !found {
		return state, errors.New("port not found")
	}
	nextState.Ports[index].Power = nextPower
	log.Printf("dio: direction=%s port=%d power=%s", direction, port, nextPower)
	return nextState, nil
}

func applyLabel(state appState, port int, direction string, nextLabel string) (appState, error) {
	if port < 1 || port > portsPerDirection {
		return state, errors.New("invalid port")
	}
	if direction != "DI" && direction != "DO" {
		return state, errors.New("direction must be DI or DO")
	}

	sanitizedLabel := strings.TrimSpace(nextLabel)
	if sanitizedLabel == "" {
		return state, errors.New("label is required")
	}

	nextState := cloneState(state)
	index, found := findPortIndex(nextState, port, direction)
	if !found {
		return state, errors.New("port not found")
	}
	nextState.Ports[index].Label = sanitizedLabel
	log.Printf("dio: direction=%s port=%d label=%s", direction, port, sanitizedLabel)
	return nextState, nil
}

func findPort(state appState, port int, direction string) (portState, bool) {
	index, found := findPortIndex(state, port, direction)
	if !found {
		return portState{}, false
	}
	return state.Ports[index], true
}

func findPortIndex(state appState, port int, direction string) (int, bool) {
	for index, singlePortState := range state.Ports {
		if singlePortState.Port == port && singlePortState.Direction == direction {
			return index, true
		}
	}
	return -1, false
}

func cloneState(source appState) appState {
	copiedPorts := make([]portState, len(source.Ports))
	copy(copiedPorts, source.Ports)
	return appState{Ports: copiedPorts}
}

func writeDIOPower(port int, direction string, nextPower string, dioValuePathTemplate string) error {
	if direction == "DI" {
		log.Printf("dio: DI port=%d is virtual state only", port)
		return nil
	}
	if runtime.GOOS != "linux" {
		log.Printf("dio: non-linux runtime, skip physical write for port=%d", port)
		return nil
	}

	nextValue := "0"
	if nextPower == "on" {
		nextValue = "1"
	}
	path := fmt.Sprintf(dioValuePathTemplate, port)
	return os.WriteFile(path, []byte(nextValue), 0o644)
}

// =============================
// HTTP handlers.
// =============================

func handleGetState(stateCommands chan<- stateCommand) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		log.Printf("http: %s %s", request.Method, request.URL.Path)
		reply := make(chan stateReply, 1)
		stateCommands <- stateCommand{kind: "get", reply: reply}
		result := <-reply
		if result.err != nil {
			http.Error(writer, result.err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(writer, result.state)
	}
}

func handleSetPower(stateCommands chan<- stateCommand) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		log.Printf("http: %s %s", request.Method, request.URL.Path)
		if request.Method != http.MethodPost {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var apiRequest setPowerRequest
		if err := json.NewDecoder(request.Body).Decode(&apiRequest); err != nil {
			http.Error(writer, "invalid json", http.StatusBadRequest)
			return
		}

		reply := make(chan stateReply, 1)
		stateCommands <- stateCommand{kind: "set_power", port: apiRequest.Port, direction: strings.ToUpper(apiRequest.Direction), power: apiRequest.Power, reply: reply}
		result := <-reply
		if result.err != nil {
			http.Error(writer, result.err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(writer, result.state)
	}
}

func handleSetLabel(stateCommands chan<- stateCommand) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		log.Printf("http: %s %s", request.Method, request.URL.Path)
		if request.Method != http.MethodPost {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var apiRequest setLabelRequest
		if err := json.NewDecoder(request.Body).Decode(&apiRequest); err != nil {
			http.Error(writer, "invalid json", http.StatusBadRequest)
			return
		}

		reply := make(chan stateReply, 1)
		stateCommands <- stateCommand{kind: "set_label", port: apiRequest.Port, direction: strings.ToUpper(apiRequest.Direction), label: apiRequest.Label, reply: reply}
		result := <-reply
		if result.err != nil {
			http.Error(writer, result.err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(writer, result.state)
	}
}

func writeJSON(writer http.ResponseWriter, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(payload)
}

// =============================
// Static file serving.
// =============================

func handleRequest(writer http.ResponseWriter, request *http.Request) {
	requestedFile := strings.TrimPrefix(request.URL.Path, "/")
	if requestedFile == "" {
		requestedFile = "index.html"
	}

	fullPathToFile := filepath.Join(*directoryFlag, requestedFile)
	if fileExists(fullPathToFile) {
		http.ServeFile(writer, request, fullPathToFile)
		return
	}

	if fileExistsInStatic(requestedFile) {
		fileData, err := staticFiles.ReadFile(filepath.Join("static", requestedFile))
		if err != nil {
			http.Error(writer, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", getContentType(requestedFile))
		_, _ = writer.Write(fileData)
		return
	}

	http.NotFound(writer, request)
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return err == nil && !info.IsDir()
}

func fileExistsInStatic(filename string) bool {
	_, err := staticFiles.ReadFile(filepath.Join("static", filename))
	return err == nil
}

func getContentType(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".html":
		return "text/html"
	case ".js":
		return "application/javascript"
	case ".css":
		return "text/css"
	default:
		return "application/octet-stream"
	}
}
