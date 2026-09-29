package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

var (
	modShell32              = syscall.NewLazyDLL("shell32.dll")
	procShellNotifyIconW     = modShell32.NewProc("Shell_NotifyIconW")

	procRegisterClassExW    = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW      = modUser32.NewProc("CreateWindowExW")
	procDestroyWindow        = modUser32.NewProc("DestroyWindow")
	procDefWindowProcW       = modUser32.NewProc("DefWindowProcW")
	procCreatePopupMenu      = modUser32.NewProc("CreatePopupMenu")
	procAppendMenuW          = modUser32.NewProc("AppendMenuW")
	procDestroyMenu          = modUser32.NewProc("DestroyMenu")
	procTrackPopupMenu       = modUser32.NewProc("TrackPopupMenu")
	procSetForegroundWindow  = modUser32.NewProc("SetForegroundWindow")
	procGetCursorPos         = modUser32.NewProc("GetCursorPos")
	procPostMessageW         = modUser32.NewProc("PostMessageW")
	procPostQuitMessage      = modUser32.NewProc("PostQuitMessage")
	procLoadIconW            = modUser32.NewProc("LoadIconW")
	procLoadImageW           = modUser32.NewProc("LoadImageW")
	procRegisterWindowMessageW = modUser32.NewProc("RegisterWindowMessageW")
	procGetModuleHandleW     = modKernel32.NewProc("GetModuleHandleW")
)

var (
	wmTaskbarCreated uint32
)

const (
	NIM_ADD    = 0x00000000
	NIM_MODIFY = 0x00000001
	NIM_DELETE = 0x00000002

	NIF_MESSAGE = 0x00000001
	NIF_ICON    = 0x00000002
	NIF_TIP     = 0x00000004

	WM_USER         = 0x0400
	WM_TRAYICON     = WM_USER + 100
	WM_LBUTTONUP    = 0x0202
	WM_LBUTTONDBLCLK = 0x0203
	WM_RBUTTONUP    = 0x0205
	WM_CONTEXTMENU  = 0x007B
	WM_COMMAND      = 0x0111
	WM_DESTROY      = 0x0002
	WM_NULL         = 0x0000

	MF_STRING    = 0x00000000
	MF_GRAYED    = 0x00000001
	MF_DISABLED  = 0x00000002
	MF_SEPARATOR = 0x00000800

	TPM_RIGHTBUTTON = 0x0002

	IDI_APPLICATION = 32512
	IMAGE_ICON      = 1
	LR_LOADFROMFILE = 0x00000010
	LR_DEFAULTSIZE  = 0x00000040

	ID_TRAY_HEADER   = 2000
	ID_OPEN_CONSOLE  = 2001
	ID_TOGGLE_ENABLE = 2002
	ID_OPEN_SETTINGS = 2003
	ID_EXIT_APP      = 2004
)

type POINT struct {
	X int32
	Y int32
}

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

type SystemTray struct {
	mu           sync.Mutex
	hWnd         uintptr
	nid          NOTIFYICONDATAW
	isAdded      bool
	consoleHwnd  uintptr
	onSettings   func()
	onExit       func()
}

var GlobalTray = &SystemTray{}

func trayWndProc(hWnd uintptr, msg uint32, wParam uintptr, lParam uintptr) uintptr {
	if wmTaskbarCreated != 0 && msg == wmTaskbarCreated {
		GlobalTray.mu.Lock()
		procShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&GlobalTray.nid)))
		GlobalTray.isAdded = true
		GlobalTray.mu.Unlock()
		return 0
	}

	switch msg {
	case WM_TRAYICON:
		switch lParam {
		case WM_LBUTTONUP, WM_LBUTTONDBLCLK:
			// Click or double click: Open Console
			GlobalTray.ShowConsole()
			return 0

		case WM_RBUTTONUP, WM_CONTEXTMENU:
			// Right click: Show context menu
			GlobalTray.showContextMenu(hWnd)
			return 0
		}

	case WM_COMMAND:
		cmdID := uint32(wParam & 0xFFFF)
		switch cmdID {
		case ID_OPEN_CONSOLE:
			GlobalTray.ShowConsole()

		case ID_TOGGLE_ENABLE:
			cfg := UpdateConfig(func(c *Config) {
				c.Enabled = !c.Enabled
			})
			GlobalTray.UpdateTooltip(cfg)
			fmt.Printf("\n[Tray] Sounds %s\n> ", map[bool]string{true: "Enabled", false: "Disabled"}[cfg.Enabled])

		case ID_OPEN_SETTINGS:
			GlobalTray.ShowConsole()
			if GlobalTray.onSettings != nil {
				GlobalTray.onSettings()
			}

		case ID_EXIT_APP:
			GlobalTray.Remove()
			if GlobalTray.onExit != nil {
				GlobalTray.onExit()
			}
			procPostQuitMessage.Call(0)
		}
		return 0

	case WM_DESTROY:
		GlobalTray.Remove()
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hWnd, uintptr(msg), wParam, lParam)
	return ret
}

// InitSystemTray initializes and displays the system tray icon
func (st *SystemTray) Init(consoleHwnd uintptr, onSettings func(), onExit func()) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	st.consoleHwnd = consoleHwnd
	st.onSettings = onSettings
	st.onExit = onExit

	hInst, _, _ := procGetModuleHandleW.Call(0)

	className, _ := syscall.UTF16PtrFromString("MechChillTrayClass")
	wndClass := WNDCLASSEXW{
		CbSize:      uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		LpfnWndProc: syscall.NewCallback(trayWndProc),
		HInstance:   hInst,
		LpszClassName: className,
	}

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wndClass)))

	windowName, _ := syscall.UTF16PtrFromString("MechChillTrayWindow")
	hWnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0, 0, 0, 0, 0,
		0, // HWND_MESSAGE
		0,
		hInst,
		0,
	)
	if hWnd == 0 {
		return fmt.Errorf("failed to create tray message window: %w", err)
	}

	st.hWnd = hWnd

	// Load Icon
	hIcon := loadAppIcon()

	st.nid = NOTIFYICONDATAW{
		CbSize:           uint32(unsafe.Sizeof(NOTIFYICONDATAW{})),
		HWnd:             hWnd,
		UID:              1,
		UFlags:           NIF_MESSAGE | NIF_ICON | NIF_TIP,
		UCallbackMessage: WM_TRAYICON,
		HIcon:            hIcon,
	}

	// Register "TaskbarCreated" message for Explorer restarts
	tbMsg, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(tbMsg)))
	wmTaskbarCreated = uint32(r)

	cfg := GetConfig()
	st.setTooltipLocked(formatTooltip(cfg))

	ret, _, _ := procShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&st.nid)))
	if ret != 0 {
		st.isAdded = true
	}

	return nil
}

func loadAppIcon() uintptr {
	// Try loading icon.ico in same folder if present
	exePath, err := os.Executable()
	if err == nil {
		icoPath := filepath.Join(filepath.Dir(exePath), "icon.ico")
		if _, err := os.Stat(icoPath); err == nil {
			icoUtf16, _ := syscall.UTF16PtrFromString(icoPath)
			hIco, _, _ := procLoadImageW.Call(
				0,
				uintptr(unsafe.Pointer(icoUtf16)),
				IMAGE_ICON,
				0, 0,
				LR_LOADFROMFILE|LR_DEFAULTSIZE,
			)
			if hIco != 0 {
				return hIco
			}
		}
	}

	// Fallback to standard Windows application icon
	hIco, _, _ := procLoadIconW.Call(0, uintptr(IDI_APPLICATION))
	return hIco
}

func (st *SystemTray) showContextMenu(hWnd uintptr) {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	headerText, _ := syscall.UTF16PtrFromString("MechChill")
	procAppendMenuW.Call(hMenu, MF_STRING|MF_DISABLED|MF_GRAYED, ID_TRAY_HEADER, uintptr(unsafe.Pointer(headerText)))

	procAppendMenuW.Call(hMenu, MF_SEPARATOR, 0, 0)

	openText, _ := syscall.UTF16PtrFromString("Open Console")
	procAppendMenuW.Call(hMenu, MF_STRING, ID_OPEN_CONSOLE, uintptr(unsafe.Pointer(openText)))

	cfg := GetConfig()
	var toggleStr string
	if cfg.Enabled {
		toggleStr = "Disable Sounds"
	} else {
		toggleStr = "Enable Sounds"
	}
	toggleText, _ := syscall.UTF16PtrFromString(toggleStr)
	procAppendMenuW.Call(hMenu, MF_STRING, ID_TOGGLE_ENABLE, uintptr(unsafe.Pointer(toggleText)))

	settingsText, _ := syscall.UTF16PtrFromString("Settings")
	procAppendMenuW.Call(hMenu, MF_STRING, ID_OPEN_SETTINGS, uintptr(unsafe.Pointer(settingsText)))

	procAppendMenuW.Call(hMenu, MF_SEPARATOR, 0, 0)

	exitText, _ := syscall.UTF16PtrFromString("Exit")
	procAppendMenuW.Call(hMenu, MF_STRING, ID_EXIT_APP, uintptr(unsafe.Pointer(exitText)))

	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	procSetForegroundWindow.Call(hWnd)
	procTrackPopupMenu.Call(hMenu, TPM_RIGHTBUTTON, uintptr(pt.X), uintptr(pt.Y), 0, hWnd, 0)
	procPostMessageW.Call(hWnd, WM_NULL, 0, 0)
}

func (st *SystemTray) ShowConsole() {
	if st.consoleHwnd != 0 {
		procShowWindow.Call(st.consoleHwnd, SW_SHOW)
		procSetForegroundWindow.Call(st.consoleHwnd)
	}
}

func (st *SystemTray) HideConsole() {
	if st.consoleHwnd != 0 {
		procShowWindow.Call(st.consoleHwnd, SW_HIDE)
	}
}

// UpdateTooltip updates tray icon tooltip based on current configuration
func (st *SystemTray) UpdateTooltip(cfg Config) {
	st.mu.Lock()
	defer st.mu.Unlock()

	if !st.isAdded {
		return
	}

	st.setTooltipLocked(formatTooltip(cfg))
	procShellNotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&st.nid)))
}

func (st *SystemTray) setTooltipLocked(tip string) {
	tipUtf16, _ := syscall.UTF16FromString(tip)
	var tipArr [128]uint16
	copy(tipArr[:], tipUtf16)
	st.nid.SzTip = tipArr
}

func formatTooltip(cfg Config) string {
	status := "Enabled"
	if !cfg.Enabled {
		status = "Disabled"
	}
	return fmt.Sprintf("MechChill - %s (%d%%)", status, cfg.Volume)
}

// Remove deletes the tray icon
func (st *SystemTray) Remove() {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.isAdded {
		procShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&st.nid)))
		st.isAdded = false
	}
	if st.hWnd != 0 {
		procDestroyWindow.Call(st.hWnd)
		st.hWnd = 0
	}
}
