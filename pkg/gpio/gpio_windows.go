//go:build windows

package gpio

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	vecowInitIsolateNonIsolated        = 0
	vecowInitDIONPN                    = 0
	vecowGPIOConfigMask         uint16 = 0xFF00
)

var vecowDLLCandidates = []string{
	"drv.dll",
	"WinRing0x64.dll",
	"OpenHardwareMonitorLib.dll",
	"Vecow.dll",
}

type windowsDriverProbeAttempt struct {
	dllPath string
	steps   []string
	outcome string
}

type windowsDriverProbeError struct {
	summary  string
	probeLog string
}

func (probeErr *windowsDriverProbeError) Error() string {
	return probeErr.summary
}

func (probeErr *windowsDriverProbeError) ProbeLog() string {
	return probeErr.probeLog
}

type windowsAdapter struct {
	dllName     string
	dll         *windows.LazyDLL
	procInitial *windows.LazyProc
	procConfig  *windows.LazyProc
	procGetGPIO *windows.LazyProc
	procSetGPIO *windows.LazyProc

	outputMask atomic.Uint32
}

func DefaultInputTemplate() string {
	return ""
}

func DefaultOutputTemplate() string {
	return ""
}

func Open(config Config) (Adapter, RuntimeMode, error) {
	_ = config

	adapter, mode, err := openWindowsAdapter()
	if err != nil {
		return nil, RuntimeMode{}, err
	}
	return adapter, mode, nil
}

func openWindowsAdapter() (*windowsAdapter, RuntimeMode, error) {
	searchOrder := windowsDLLSearchOrder()
	allProbeAttempts := make([]windowsDriverProbeAttempt, 0, len(searchOrder))
	for _, dllName := range searchOrder {
		adapter, probeAttempt, err := tryOpenWindowsAdapter(dllName)
		allProbeAttempts = append(allProbeAttempts, probeAttempt)
		if err != nil {
			continue
		}

		driverProbeLog := formatWindowsDriverProbeLog(allProbeAttempts)
		return adapter, RuntimeMode{
			ActiveDriver:   filepath.Base(dllName),
			DriverProbeLog: driverProbeLog,
		}, nil
	}

	driverProbeLog := formatWindowsDriverProbeLog(allProbeAttempts)
	return nil, RuntimeMode{}, &windowsDriverProbeError{
		summary:  "initialize Vecow GPIO failed: no suitable DLL found",
		probeLog: driverProbeLog,
	}
}

func windowsDLLSearchOrder() []string {
	driverDir := strings.TrimSpace(os.Getenv("CHICHA_GPIO_WINDOWS_DRIVER_DIR"))
	customDLL := strings.TrimSpace(os.Getenv("CHICHA_GPIO_WINDOWS_DLL"))

	searchOrder := make([]string, 0, len(vecowDLLCandidates)+2)
	if customDLL != "" {
		searchOrder = append(searchOrder, customDLL)
	}
	for _, dllName := range vecowDLLCandidates {
		if driverDir != "" {
			searchOrder = append(searchOrder, filepath.Join(driverDir, dllName))
			continue
		}
		searchOrder = append(searchOrder, dllName)
	}
	return searchOrder
}

func tryOpenWindowsAdapter(dllName string) (*windowsAdapter, windowsDriverProbeAttempt, error) {
	dllPathForLog := resolveDLLPathForLog(dllName)
	probeAttempt := windowsDriverProbeAttempt{
		dllPath: dllPathForLog,
		steps:   make([]string, 0, 10),
	}

	dll := windows.NewLazyDLL(dllName)
	if err := dll.Load(); err != nil {
		probeAttempt.steps = append(probeAttempt.steps, fmt.Sprintf("Load DLL: FAIL (%v)", err))
		probeAttempt.steps = append(probeAttempt.steps, "GPIO probe: skipped because DLL was not loaded")
		probeAttempt.outcome = "FAIL"
		return nil, probeAttempt, fmt.Errorf("load %s: %w", dllName, err)
	}
	probeAttempt.steps = append(probeAttempt.steps, "Load DLL: OK")

	adapter := &windowsAdapter{
		dllName: dllName,
		dll:     dll,
	}

	// Different Vecow driver packages expose compatible GPIO functions with different export names.
	// We resolve aliases in priority order to support both ECX1K-style and legacy Vecow-style DLLs.
	initialProc, initialProcName, initialProcErr := findProcByAlias(dll, []string{"Initial", "initial_SIO"})
	if initialProcErr != nil {
		probeAttempt.steps = append(probeAttempt.steps, fmt.Sprintf("Resolve init API: FAIL (%v)", initialProcErr))
		probeAttempt.steps = append(probeAttempt.steps, "GPIO probe: skipped because required procedures were not resolved")
		logDLLReleaseResult(adapter.dll, &probeAttempt.steps)
		probeAttempt.outcome = "FAIL"
		return nil, probeAttempt, fmt.Errorf("resolve init API in %s: %w", dllName, initialProcErr)
	}
	adapter.procInitial = initialProc
	probeAttempt.steps = append(probeAttempt.steps, fmt.Sprintf("Resolve init API: OK (%s)", initialProcName))

	getProc, getProcName, getProcErr := findProcByAlias(dll, []string{"GetGPIO", "GetDIO1", "get_GPIO1", "GetGPIO1"})
	if getProcErr != nil {
		probeAttempt.steps = append(probeAttempt.steps, fmt.Sprintf("Resolve read API: FAIL (%v)", getProcErr))
		probeAttempt.steps = append(probeAttempt.steps, "GPIO probe: skipped because required procedures were not resolved")
		logDLLReleaseResult(adapter.dll, &probeAttempt.steps)
		probeAttempt.outcome = "FAIL"
		return nil, probeAttempt, fmt.Errorf("resolve read API in %s: %w", dllName, getProcErr)
	}
	adapter.procGetGPIO = getProc
	probeAttempt.steps = append(probeAttempt.steps, fmt.Sprintf("Resolve read API: OK (%s)", getProcName))

	setProc, setProcName, setProcErr := findProcByAlias(dll, []string{"SetGPIO", "SetDIO1", "set_GPIO1", "SetGPIO1"})
	if setProcErr != nil {
		probeAttempt.steps = append(probeAttempt.steps, fmt.Sprintf("Resolve write API: FAIL (%v)", setProcErr))
		probeAttempt.steps = append(probeAttempt.steps, "GPIO probe: skipped because required procedures were not resolved")
		logDLLReleaseResult(adapter.dll, &probeAttempt.steps)
		probeAttempt.outcome = "FAIL"
		return nil, probeAttempt, fmt.Errorf("resolve write API in %s: %w", dllName, setProcErr)
	}
	adapter.procSetGPIO = setProc
	probeAttempt.steps = append(probeAttempt.steps, fmt.Sprintf("Resolve write API: OK (%s)", setProcName))

	configProc, configProcName, configProcFound := findOptionalProcByAlias(dll, []string{"SetGPIOConfig", "set_GPIO_config", "SetGPIO1Config"})
	if configProcFound {
		adapter.procConfig = configProc
		probeAttempt.steps = append(probeAttempt.steps, fmt.Sprintf("Resolve config API: OK (%s)", configProcName))
	} else {
		probeAttempt.steps = append(probeAttempt.steps, "Resolve config API: SKIP (no compatible function export)")
	}

	if err := adapter.callInitial(); err != nil {
		probeAttempt.steps = append(probeAttempt.steps, fmt.Sprintf("Initial call: FAIL (%v)", err))
		probeAttempt.steps = append(probeAttempt.steps, "GPIO probe: skipped because Initial failed")
		logDLLReleaseResult(adapter.dll, &probeAttempt.steps)
		probeAttempt.outcome = "FAIL"
		return nil, probeAttempt, err
	}
	probeAttempt.steps = append(probeAttempt.steps, "Initial call: OK")
	if adapter.procConfig != nil {
		if err := adapter.callSetGPIOConfig(vecowGPIOConfigMask); err != nil {
			probeAttempt.steps = append(probeAttempt.steps, fmt.Sprintf("GPIO config call: FAIL (%v)", err))
			probeAttempt.steps = append(probeAttempt.steps, "GPIO probe: skipped because GPIO configuration failed")
			logDLLReleaseResult(adapter.dll, &probeAttempt.steps)
			probeAttempt.outcome = "FAIL"
			return nil, probeAttempt, err
		}
		probeAttempt.steps = append(probeAttempt.steps, "GPIO config call: OK")
	}
	if err := adapter.callSetGPIO(0); err != nil {
		probeAttempt.steps = append(probeAttempt.steps, fmt.Sprintf("SetGPIO call: FAIL (%v)", err))
		probeAttempt.steps = append(probeAttempt.steps, "GPIO probe: skipped because GPIO write failed")
		logDLLReleaseResult(adapter.dll, &probeAttempt.steps)
		probeAttempt.outcome = "FAIL"
		return nil, probeAttempt, err
	}
	probeAttempt.steps = append(probeAttempt.steps, "SetGPIO call: OK")

	var gpioState uint16
	if err := adapter.callGetGPIO(&gpioState); err != nil {
		probeAttempt.steps = append(probeAttempt.steps, fmt.Sprintf("GPIO probe: FAIL (%v)", err))
		logDLLReleaseResult(adapter.dll, &probeAttempt.steps)
		probeAttempt.outcome = "FAIL"
		return nil, probeAttempt, fmt.Errorf("GPIO ports unavailable in %s: %w", dllName, err)
	}
	probeAttempt.steps = append(probeAttempt.steps, fmt.Sprintf("GPIO probe: OK (state=0x%04X)", gpioState))
	probeAttempt.steps = append(probeAttempt.steps, "Unload DLL: skipped (DLL is active)")
	probeAttempt.outcome = "SUCCESS"

	adapter.outputMask.Store(0)
	return adapter, probeAttempt, nil
}

func resolveDLLPathForLog(dllName string) string {
	if filepath.IsAbs(dllName) {
		return dllName
	}

	if strings.ContainsRune(dllName, os.PathSeparator) {
		absolutePath, err := filepath.Abs(dllName)
		if err != nil {
			return dllName
		}
		return absolutePath
	}

	return fmt.Sprintf("PATH lookup: %s", dllName)
}

func findProcByAlias(dll *windows.LazyDLL, aliases []string) (*windows.LazyProc, string, error) {
	var firstErr error
	for _, aliasName := range aliases {
		proc := dll.NewProc(aliasName)
		if err := proc.Find(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		return proc, aliasName, nil
	}

	if firstErr == nil {
		firstErr = fmt.Errorf("no aliases were provided")
	}
	return nil, "", firstErr
}

func findOptionalProcByAlias(dll *windows.LazyDLL, aliases []string) (*windows.LazyProc, string, bool) {
	for _, aliasName := range aliases {
		proc := dll.NewProc(aliasName)
		if err := proc.Find(); err != nil {
			continue
		}
		return proc, aliasName, true
	}
	return nil, "", false
}

func logDLLReleaseResult(dll *windows.LazyDLL, probeEvents *[]string) {
	if err := releaseLazyDLL(dll); err != nil {
		*probeEvents = append(*probeEvents, fmt.Sprintf("Unload DLL: FAIL (%v)", err))
		return
	}
	*probeEvents = append(*probeEvents, "Unload DLL: OK")
}

func formatWindowsDriverProbeLog(allProbeAttempts []windowsDriverProbeAttempt) string {
	if len(allProbeAttempts) == 0 {
		return "Windows DLL probe log is empty."
	}

	formattedLines := make([]string, 0, len(allProbeAttempts)*8+4)
	formattedLines = append(formattedLines, "Windows DLL probe report")
	formattedLines = append(formattedLines, "========================")
	for probeIndex, probeAttempt := range allProbeAttempts {
		formattedLines = append(formattedLines, "")
		formattedLines = append(formattedLines, fmt.Sprintf("DLL #%d", probeIndex+1))
		formattedLines = append(formattedLines, fmt.Sprintf("Path: %s", probeAttempt.dllPath))
		for _, step := range probeAttempt.steps {
			formattedLines = append(formattedLines, fmt.Sprintf("  - %s", step))
		}
		formattedLines = append(formattedLines, fmt.Sprintf("Result: %s", probeAttempt.outcome))
	}
	return strings.Join(formattedLines, "\n")
}

func (adapter *windowsAdapter) ReadInput(channel int) (bool, error) {
	if channel < 1 || channel > InputCount {
		return false, fmt.Errorf("invalid input channel %d", channel)
	}

	var state uint16
	if err := adapter.callGetGPIO(&state); err != nil {
		return false, err
	}
	bitMask := uint16(1 << (channel - 1))
	return state&bitMask != 0, nil
}

func (adapter *windowsAdapter) WriteOutput(channel int, high bool) error {
	if channel < 1 || channel > OutputCount {
		return fmt.Errorf("invalid output channel %d", channel)
	}

	bitMask := uint32(1 << uint(channel+7))
	for {
		currentMask := adapter.outputMask.Load()
		nextMask := currentMask
		if high {
			nextMask |= bitMask
		} else {
			nextMask &^= bitMask
		}

		if !adapter.outputMask.CompareAndSwap(currentMask, nextMask) {
			continue
		}

		if err := adapter.callSetGPIO(uint16(nextMask)); err != nil {
			_ = adapter.outputMask.CompareAndSwap(nextMask, currentMask)
			return err
		}
		return nil
	}
}

func (adapter *windowsAdapter) Close() error {
	if adapter.dll == nil {
		return nil
	}
	if err := releaseLazyDLL(adapter.dll); err != nil {
		return fmt.Errorf("release %s: %w", adapter.dllName, err)
	}
	adapter.dll = nil
	return nil
}

func releaseLazyDLL(dll *windows.LazyDLL) error {
	if dll == nil {
		return nil
	}

	handle := dll.Handle()
	if handle == 0 {
		return nil
	}

	return windows.FreeLibrary(windows.Handle(handle))
}

func (adapter *windowsAdapter) callInitial() error {
	result, _, _ := adapter.procInitial.Call(vecowInitIsolateNonIsolated, vecowInitDIONPN)
	if result != 0 {
		return fmt.Errorf("Initial returned %d", result)
	}
	return nil
}

func (adapter *windowsAdapter) callSetGPIOConfig(mask uint16) error {
	result, _, _ := adapter.procConfig.Call(uintptr(mask))
	if result != 0 {
		return fmt.Errorf("SetGPIOConfig returned %d", result)
	}
	return nil
}

func (adapter *windowsAdapter) callSetGPIO(mask uint16) error {
	result, _, _ := adapter.procSetGPIO.Call(uintptr(mask))
	if result != 0 {
		return fmt.Errorf("SetGPIO returned %d", result)
	}
	return nil
}

func (adapter *windowsAdapter) callGetGPIO(state *uint16) error {
	result, _, _ := adapter.procGetGPIO.Call(uintptr(unsafe.Pointer(state)))
	if result != 0 {
		return fmt.Errorf("GetGPIO returned %d", result)
	}
	return nil
}
