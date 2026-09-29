package main

import (
	"fmt"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var (
	modUser32             = syscall.NewLazyDLL("user32.dll")
	procSetWindowsHookExW = modUser32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHook = modUser32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx    = modUser32.NewProc("CallNextHookEx")
	procGetKeyNameTextW   = modUser32.NewProc("GetKeyNameTextW")
	procMapVirtualKeyW    = modUser32.NewProc("MapVirtualKeyW")
)

const (
	WH_KEYBOARD_LL = 13
	WM_KEYDOWN     = 0x0100
	WM_KEYUP       = 0x0101
	WM_SYSKEYDOWN  = 0x0104
	WM_SYSKEYUP    = 0x0105
)

type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

var (
	hKeyboardHook   uintptr
	hookCallbackPtr uintptr

	// Track pressed keys to prevent machine-gun auto-repeat when holding keys down
	pressedKeys [256]bool

	// Atomic flags for debug mode and keyboard logging
	DebugKeyEvents atomic.Bool
	DebugMode      atomic.Bool
)

// InitKeyboardHook installs the WH_KEYBOARD_LL hook
func InitKeyboardHook() error {
	cb := syscall.NewCallback(lowLevelKeyboardProc)
	hookCallbackPtr = cb

	hHook, _, err := procSetWindowsHookExW.Call(
		WH_KEYBOARD_LL,
		cb,
		0,
		0,
	)
	if hHook == 0 {
		return fmt.Errorf("failed to install WH_KEYBOARD_LL hook: %w", err)
	}

	hKeyboardHook = hHook
	return nil
}

// CloseKeyboardHook removes the keyboard hook
func CloseKeyboardHook() {
	if hKeyboardHook != 0 {
		procUnhookWindowsHook.Call(hKeyboardHook)
		hKeyboardHook = 0
	}
}

func lowLevelKeyboardProc(nCode int32, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 {
		kbd := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lParam))
		vkCode := kbd.VkCode
		cfg := GetConfig()

		if wParam == WM_KEYDOWN || wParam == WM_SYSKEYDOWN {
			// Check if key is already held down (OS auto-repeat)
			isRepeat := false
			if vkCode < 256 {
				if pressedKeys[vkCode] {
					isRepeat = true
				} else {
					pressedKeys[vkCode] = true
				}
			}

			// If repeat and repeat is disabled, do not play sound!
			if !isRepeat || cfg.AllowRepeat {
				var soundPlayed string
				if cfg.Enabled {
					soundPlayed = GlobalAudio.PlayKeySound(vkCode, cfg.RandomSounds, cfg.Volume)
				}

				if DebugKeyEvents.Load() || DebugMode.Load() {
					keyName := GetKeyDisplayName(vkCode, kbd.ScanCode, kbd.Flags)
					repeatTag := ""
					if isRepeat {
						repeatTag = colYellow + " [REPEAT]" + colReset
					}
					if soundPlayed != "" {
						fmt.Printf("   %s[KEY DOWN]%s %s%-12s%s (VK: 0x%02X) -> %s%s%s%s\n",
							colCyan+colBold, colReset, colWhite+colBold, keyName, colReset, vkCode, colMagenta, soundPlayed, colReset, repeatTag)
					} else {
						fmt.Printf("   %s[KEY DOWN]%s %s%-12s%s (VK: 0x%02X) %s[Disabled]%s%s\n",
							colCyan, colReset, colWhite, keyName, colReset, vkCode, colGray, colReset, repeatTag)
					}
				}
			}
		} else if wParam == WM_KEYUP || wParam == WM_SYSKEYUP {
			// Key released
			if vkCode < 256 {
				pressedKeys[vkCode] = false
			}

			if cfg.Enabled && cfg.ReleaseSound {
				soundPlayed := GlobalAudio.PlayKeyUpSound(vkCode, cfg.Volume)
				if (DebugKeyEvents.Load() || DebugMode.Load()) && soundPlayed != "" {
					keyName := GetKeyDisplayName(vkCode, kbd.ScanCode, kbd.Flags)
					fmt.Printf("   %s[KEY UP]%s   %s%-12s%s (VK: 0x%02X) -> %s%s%s\n",
						colBlue, colReset, colWhite, keyName, colReset, vkCode, colMagenta, soundPlayed, colReset)
				}
			}
		}
	}

	ret, _, _ := procCallNextHookEx.Call(hKeyboardHook, uintptr(nCode), wParam, lParam)
	return ret
}

// GetKeyDisplayName returns human-readable name of key
func GetKeyDisplayName(vkCode, scanCode, flags uint32) string {
	switch vkCode {
	case 0x20:
		return "SPACE"
	case 0x0D:
		return "ENTER"
	case 0x08:
		return "BACKSPACE"
	case 0x09:
		return "TAB"
	case 0x1B:
		return "ESC"
	case 0x10:
		return "SHIFT"
	case 0xA0:
		return "LEFT SHIFT"
	case 0xA1:
		return "RIGHT SHIFT"
	case 0x11:
		return "CTRL"
	case 0xA2:
		return "LEFT CTRL"
	case 0xA3:
		return "RIGHT CTRL"
	case 0x12:
		return "ALT"
	case 0x14:
		return "CAPS LOCK"
	case 0x2E:
		return "DELETE"
	case 0x25:
		return "LEFT ARROW"
	case 0x26:
		return "UP ARROW"
	case 0x27:
		return "RIGHT ARROW"
	case 0x28:
		return "DOWN ARROW"
	}

	// Try Windows API GetKeyNameText
	lParam := (scanCode << 16)
	if (flags & 0x01) != 0 {
		lParam |= (1 << 24) // extended key
	}
	var buf [64]uint16
	ret, _, _ := procGetKeyNameTextW.Call(uintptr(lParam), uintptr(unsafe.Pointer(&buf[0])), 64)
	if ret > 0 {
		return syscall.UTF16ToString(buf[:ret])
	}

	// Fallback to ASCII character if printable
	if vkCode >= 0x30 && vkCode <= 0x5A { // 0-9, A-Z
		return string(rune(vkCode))
	}

	return fmt.Sprintf("KEY_0x%02X", vkCode)
}
