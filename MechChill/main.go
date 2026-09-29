package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	modKernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleWindow      = modKernel32.NewProc("GetConsoleWindow")
	procSetConsoleCtrlHandler = modKernel32.NewProc("SetConsoleCtrlHandler")
	procSetConsoleTitleW      = modKernel32.NewProc("SetConsoleTitleW")
	procGetStdHandle          = modKernel32.NewProc("GetStdHandle")
	procGetConsoleMode        = modKernel32.NewProc("GetConsoleMode")
	procSetConsoleMode        = modKernel32.NewProc("SetConsoleMode")
	procCreateMutexW          = modKernel32.NewProc("CreateMutexW")
	procGetLastError          = modKernel32.NewProc("GetLastError")

	procShowWindow       = modUser32.NewProc("ShowWindow")
	procGetSystemMenu    = modUser32.NewProc("GetSystemMenu")
	procDeleteMenu       = modUser32.NewProc("DeleteMenu")
	procGetMessageW      = modUser32.NewProc("GetMessageW")
	procTranslateMessage = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW = modUser32.NewProc("DispatchMessageW")
	procFindWindowW      = modUser32.NewProc("FindWindowW")

	procIsUserAnAdmin = modShell32.NewProc("IsUserAnAdmin")
	procShellExecuteW = modShell32.NewProc("ShellExecuteW")
)

const (
	SW_HIDE = 0
	SW_SHOW = 5

	SC_CLOSE     = 0xF060
	MF_BYCOMMAND = 0x00000000

	CTRL_C_EVENT        = 0
	CTRL_BREAK_EVENT    = 1
	CTRL_CLOSE_EVENT    = 2
	CTRL_LOGOFF_EVENT   = 5
	CTRL_SHUTDOWN_EVENT = 6

	STD_OUTPUT_HANDLE                  = ^uintptr(10) // -11
	ENABLE_VIRTUAL_TERMINAL_PROCESSING = 0x0004
	ERROR_ALREADY_EXISTS               = 183
)

// ANSI Color and Style Codes
const (
	colReset   = "\033[0m"
	colBold    = "\033[1m"
	colDim     = "\033[2m"
	colRed     = "\033[91m"
	colGreen   = "\033[92m"
	colYellow  = "\033[93m"
	colBlue    = "\033[94m"
	colMagenta = "\033[95m"
	colCyan    = "\033[96m"
	colWhite   = "\033[97m"
	colGray    = "\033[90m"
)

type MSG struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       POINT
	LPrivate uint32
}

var (
	consoleHwnd     uintptr
	ctrlHandlerPtr  uintptr
	isTerminating   atomic.Bool
	appExitChan     = make(chan struct{})
	showSettingsSig = make(chan struct{}, 1)
	shutdownOnce    sync.Once
)

func initConsole() {
	hOut, _, _ := procGetStdHandle.Call(STD_OUTPUT_HANDLE)
	if hOut != 0 && hOut != ^uintptr(0) {
		var mode uint32
		if ret, _, _ := procGetConsoleMode.Call(hOut, uintptr(unsafe.Pointer(&mode))); ret != 0 {
			mode |= ENABLE_VIRTUAL_TERMINAL_PROCESSING
			procSetConsoleMode.Call(hOut, uintptr(mode))
		}
	}
}

func clearScreen() {
	fmt.Print("\033[H\033[2J\033[3J")
}

func renderVolumeBar(vol int) string {
	bars := vol / 10
	if bars > 10 {
		bars = 10
	}
	if bars < 0 {
		bars = 0
	}
	filled := strings.Repeat("█", bars)
	empty := strings.Repeat("░", 10-bars)
	return fmt.Sprintf("%s[%s%s%s%s]%s %s%d%%%s", colBold, colYellow, filled, colGray, empty, colReset, colYellow+colBold, vol, colReset)
}

func checkIsAdmin() bool {
	ret, _, _ := procIsUserAnAdmin.Call()
	return ret != 0
}

func relaunchAsAdmin() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return
	}
	exeUtf16, _ := syscall.UTF16PtrFromString(exePath)
	verbUtf16, _ := syscall.UTF16PtrFromString("runas")

	ret, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbUtf16)),
		uintptr(unsafe.Pointer(exeUtf16)),
		0,
		0,
		SW_SHOW,
	)
	if ret > 32 {
		shutdownApp()
	}
}

func main() {
	// 1. Single Instance Protection (prevent duplicate hooks and audio conflicts)
	mutexName, _ := syscall.UTF16PtrFromString("Local\\MechChill_SingleInstance_Mutex")
	hMutex, _, _ := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(mutexName)))
	lastErr, _, _ := procGetLastError.Call()
	if lastErr == ERROR_ALREADY_EXISTS || hMutex == 0 {
		trayClass, _ := syscall.UTF16PtrFromString("MechChillTrayClass")
		if hTrayWnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(trayClass)), 0); hTrayWnd != 0 {
			procPostMessageW.Call(hTrayWnd, WM_COMMAND, uintptr(ID_OPEN_CONSOLE), 0)
		}
		fmt.Println("\n   [!] MechChill is already running in the background!")
		fmt.Println("   Restoring existing instance to foreground...")
		time.Sleep(1200 * time.Millisecond)
		os.Exit(0)
	}

	// 2. Lock OS thread for the Windows message loop
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// 3. Setup Console Window & ANSI Mode
	initConsole()
	hwnd, _, _ := procGetConsoleWindow.Call()
	consoleHwnd = hwnd

	if consoleHwnd != 0 {
		titleUtf16, _ := syscall.UTF16PtrFromString("MechChill - Mechanical Keyboard Sound Engine")
		procSetConsoleTitleW.Call(uintptr(unsafe.Pointer(titleUtf16)))

		// Disable the 'X' button on the console window to prevent accidental exit
		if hMenu, _, _ := procGetSystemMenu.Call(consoleHwnd, 0); hMenu != 0 {
			procDeleteMenu.Call(hMenu, SC_CLOSE, MF_BYCOMMAND)
		}
	}

	// 4. Register Console Ctrl Handler
	// When close, logoff, or CTRL+C occurs, hide or exit cleanly
	ctrlHandler := syscall.NewCallback(func(ctrlType uint32) uintptr {
		if ctrlType == CTRL_CLOSE_EVENT || ctrlType == CTRL_C_EVENT {
			// Hide to tray instead of abruptly crashing
			if consoleHwnd != 0 {
				procShowWindow.Call(consoleHwnd, SW_HIDE)
			}
			return 1 // Handled
		}
		if ctrlType == CTRL_LOGOFF_EVENT || ctrlType == CTRL_SHUTDOWN_EVENT {
			shutdownApp()
			return 0
		}
		return 0
	})
	ctrlHandlerPtr = ctrlHandler
	procSetConsoleCtrlHandler.Call(ctrlHandler, 1)

	// 4. Load Configuration
	cfg := LoadConfig()

	// 5. Load Sounds into memory
	soundsDir := ResolveSoundPackDir(cfg.SoundPack)
	if err := GlobalAudio.Init(soundsDir); err != nil {
		fmt.Printf("[Warning] Error loading sound files from '%s': %v\n", soundsDir, err)
		// Fallback to sounds/
		_ = GlobalAudio.Init("sounds")
	}

	// 6. Initialize Global Keyboard Hook
	if err := InitKeyboardHook(); err != nil {
		fmt.Printf("[Warning] Could not initialize keyboard hook: %v\n", err)
	}

	// 7. Initialize System Tray Icon
	err := GlobalTray.Init(consoleHwnd,
		func() {
			// On Settings requested from Tray
			select {
			case showSettingsSig <- struct{}{}:
			default:
			}
		},
		func() {
			// On Exit requested from Tray
			shutdownApp()
		},
	)
	if err != nil {
		fmt.Printf("[Warning] Could not initialize system tray: %v\n", err)
	}

	// 8. Start Console UI loop in background goroutine
	go runConsoleUI()

	// 9. Windows Message Loop (blocks here, 0% CPU when idle)
	var msg MSG
	for !isTerminating.Load() {
		ret, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0,
		)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	shutdownApp()
}

func shutdownApp() {
	shutdownOnce.Do(func() {
		isTerminating.Store(true)

		// 1. Unhook keyboard hook
		CloseKeyboardHook()

		// 2. Free audio resources
		GlobalAudio.Close()

		// 3. Remove tray icon
		GlobalTray.Remove()

		// 4. Save config
		SaveConfig()

		close(appExitChan)
		os.Exit(0)
	})
}

// -----------------------------------------------------------------------------
// Console Interface
// -----------------------------------------------------------------------------

func printMainMenu() {
	clearScreen()
	cfg := GetConfig()
	statusBadge := colGreen + colBold + "● ACTIVE" + colReset
	if !cfg.Enabled {
		statusBadge = colRed + colBold + "○ MUTED" + colReset
	}

	isAdmin := checkIsAdmin()
	privBadge := colGreen + colBold + "● ELEVATED" + colReset + colDim + " (Active in ALL windows & Task Manager)" + colReset
	if !isAdmin {
		privBadge = colYellow + "○ STANDARD" + colReset + colDim + " (Press [A] to Relaunch as Admin)" + colReset
	}

	adminOption := ""
	if !isAdmin {
		adminOption = "   " + colYellow + colBold + "[A] Relaunch as Administrator" + colReset + colDim + "  (Enables hook in Task Manager & Games)" + colReset + "\n"
	}

	fmt.Print(
		colCyan + colBold +
			" ╔═══════════════════════════════════════════════════════╗\n" +
			" ║" + colWhite + "   ⌨   M E C H C H I L L  v1.1                         " + colCyan + "║\n" +
			" ║" + colGray + "   Ultra-Lightweight Mechanical Sound Engine           " + colCyan + "║\n" +
			" ╚═══════════════════════════════════════════════════════╝" + colReset + "\n\n" +
			"   Status     : " + statusBadge + "\n" +
			"   Privileges : " + privBadge + "\n" +
			"   Volume     : " + renderVolumeBar(cfg.Volume) + "\n" +
			"   Sound Pack : " + colMagenta + colBold + cfg.SoundPack + colReset + "\n\n" +
			colGray + " ───────────────────────────────────────────────────────" + colReset + "\n" +
			"   " + colCyan + "[1]" + colReset + " Settings            " + colGray + "(Volume, Packs, Repeat, Bas-Çek)" + colReset + "\n" +
			"   " + colCyan + "[2]" + colReset + " Sound Test         " + colGray + "(Play sample clicks)" + colReset + "\n" +
			"   " + colCyan + "[3]" + colReset + " Reload Sounds      " + colGray + "(Reload WAVs from disk)" + colReset + "\n" +
			"   " + colCyan + "[4]" + colReset + " Dev Tools          " + colGray + "(Debug key events & RAM)" + colReset + "\n" +
			"   " + colCyan + "[5]" + colReset + " About              " + colGray + "(App info & philosophy)" + colReset + "\n" +
			adminOption + "\n" +
			"   " + colGreen + colBold + "[Q] Hide to System Tray" + colReset + colDim + "  (Runs quietly in background)" + colReset + "\n" +
			colGray + " ───────────────────────────────────────────────────────" + colReset + "\n" +
			colDim + "   Listening for keyboard input...\n" + colReset +
			"   " + colCyan + "❯ " + colReset)
}

func runConsoleUI() {
	reader := bufio.NewReader(os.Stdin)
	printMainMenu()

	for {
		select {
		case <-appExitChan:
			return
		case <-showSettingsSig:
			showSettingsMenu(reader)
			printMainMenu()
			continue
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		cmd := strings.TrimSpace(line)
		cmdUpper := strings.ToUpper(cmd)

		switch cmdUpper {
		case "1":
			showSettingsMenu(reader)
			printMainMenu()

		case "2":
			fmt.Print("\n" + colGreen + "   ♪ Playing test clicks..." + colReset)
			cfg := GetConfig()
			_ = GlobalAudio.PlaySound("space", cfg.Volume)
			time.Sleep(120 * time.Millisecond)
			_ = GlobalAudio.PlaySound("key1", cfg.Volume)
			time.Sleep(300 * time.Millisecond)
			printMainMenu()

		case "3":
			fmt.Print("\n" + colCyan + "   ↻ Reloading sounds from disk..." + colReset)
			cfg := GetConfig()
			soundsDir := ResolveSoundPackDir(cfg.SoundPack)
			if err := GlobalAudio.LoadSounds(soundsDir); err != nil {
				fmt.Printf("\n   %s[Error] Reload failed: %v%s\n", colRed, err, colReset)
				time.Sleep(1500 * time.Millisecond)
			} else {
				fmt.Printf("\n   %s✓ Sounds successfully reloaded from '%s'!%s\n", colGreen, soundsDir, colReset)
				time.Sleep(700 * time.Millisecond)
			}
			printMainMenu()

		case "4":
			showDevToolsMenu(reader)
			printMainMenu()

		case "5":
			showAboutMenu(reader)
			printMainMenu()

		case "A":
			if !checkIsAdmin() {
				fmt.Println("\n" + colYellow + "   Relaunching MechChill with Administrator privileges..." + colReset)
				relaunchAsAdmin()
			} else {
				fmt.Println("\n" + colGreen + "   Already running as Administrator!" + colReset)
				time.Sleep(800 * time.Millisecond)
				printMainMenu()
			}

		case "Q":
			clearScreen()
			fmt.Println("\n" + colGreen + colBold + "   ✔ Minimized to System Tray." + colReset)
			fmt.Println(colDim + "   Double-click the Tray icon or right-click to restore this console." + colReset)
			GlobalTray.HideConsole()

		default:
			if cmd != "" {
				printMainMenu()
				fmt.Print(colRed + "Unknown option. Choose 1-5 or Q.\n" + colReset + "   " + colCyan + "❯ " + colReset)
			}
		}
	}
}

func showSettingsMenu(reader *bufio.Reader) {
	for {
		clearScreen()
		cfg := GetConfig()

		toggleBadge := func(val bool) string {
			if val {
				return colGreen + colBold + "[ON]" + colReset
			}
			return colGray + "[OFF]" + colReset
		}

		isAdmin := checkIsAdmin()
		privBadge := colGreen + colBold + "● ELEVATED" + colReset
		if !isAdmin {
			privBadge = colYellow + "○ STANDARD " + colReset + colDim + "(Press [9] to Elevate)" + colReset
		}

		fmt.Print(
			colCyan + colBold +
				" ┌───────────────────────────────────────────────────┐\n" +
				" │   ⚙  M E C H C H I L L  -  S E T T I N G S        │\n" +
				" └───────────────────────────────────────────────────┘" + colReset + "\n\n" +
				"   " + colCyan + "[1]" + colReset + " Volume                 : " + renderVolumeBar(cfg.Volume) + "\n" +
				"   " + colCyan + "[2]" + colReset + " Enable Sounds          : " + toggleBadge(cfg.Enabled) + "\n" +
				"   " + colCyan + "[3]" + colReset + " Random Variations      : " + toggleBadge(cfg.RandomSounds) + "\n" +
				"   " + colCyan + "[4]" + colReset + " Sound Pack             : " + colMagenta + colBold + cfg.SoundPack + colReset + "\n" +
				"   " + colCyan + "[5]" + colReset + " Release Sound (Bas-Çek): " + toggleBadge(cfg.ReleaseSound) + "\n" +
				"   " + colCyan + "[6]" + colReset + " Key Repeat on Hold     : " + toggleBadge(cfg.AllowRepeat) + "\n" +
				"   " + colCyan + "[7]" + colReset + " Test Current Sounds    : " + colYellow + "♪ Space + Key click" + colReset + "\n" +
				"   " + colCyan + "[8]" + colReset + " Run at Windows Startup : " + toggleBadge(cfg.StartupEnabled) + "\n" +
				"   " + colCyan + "[9]" + colReset + " Privileges (Admin UIPI): " + privBadge + "\n\n" +
				"   " + colYellow + colBold + "[B]" + colReset + " Back to Main Menu\n\n" +
				colGray + " ───────────────────────────────────────────────────\n" + colReset +
				"   Select an option (1-9 or B): ")

		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		choice := strings.TrimSpace(line)
		choiceUpper := strings.ToUpper(choice)

		switch choiceUpper {
		case "1":
			fmt.Print("\n   " + colYellow + "Enter volume (0 - 100): " + colReset)
			volLine, _ := reader.ReadString('\n')
			vStr := strings.TrimSpace(volLine)
			val, err := strconv.Atoi(vStr)
			if err != nil || val < 0 || val > 100 {
				fmt.Println(colRed + "   Invalid volume. Please enter a number between 0 and 100." + colReset)
				time.Sleep(800 * time.Millisecond)
			} else {
				updated := UpdateConfig(func(c *Config) {
					c.Volume = val
				})
				GlobalTray.UpdateTooltip(updated)
				// Play sound feedback so the user hears the new volume!
				_ = GlobalAudio.PlaySound("key1", val)
			}

		case "2":
			updated := UpdateConfig(func(c *Config) {
				c.Enabled = !c.Enabled
			})
			GlobalTray.UpdateTooltip(updated)

		case "3":
			UpdateConfig(func(c *Config) {
				c.RandomSounds = !c.RandomSounds
			})

		case "4":
			selectSoundPackMenu(reader)

		case "5":
			UpdateConfig(func(c *Config) {
				c.ReleaseSound = !c.ReleaseSound
			})

		case "6":
			UpdateConfig(func(c *Config) {
				c.AllowRepeat = !c.AllowRepeat
			})

		case "7":
			fmt.Print("\n" + colGreen + "   ♪ Playing test sound (Press & Release)..." + colReset)
			_ = GlobalAudio.PlaySound("space", cfg.Volume)
			time.Sleep(100 * time.Millisecond)
			if cfg.ReleaseSound {
				_ = GlobalAudio.PlayKeyUpSound(0x20, cfg.Volume)
			}
			time.Sleep(200 * time.Millisecond)

		case "8":
			newVal := !cfg.StartupEnabled
			if err := SetStartupRegistry(newVal); err != nil {
				fmt.Printf("\n   %sFailed to update Windows startup: %v%s\n", colRed, err, colReset)
				time.Sleep(1200 * time.Millisecond)
			}

		case "9":
			if !checkIsAdmin() {
				fmt.Println("\n   " + colYellow + "Relaunching MechChill as Administrator..." + colReset)
				relaunchAsAdmin()
			} else {
				fmt.Println("\n   " + colGreen + "Already running with full Administrator privileges!" + colReset)
				time.Sleep(800 * time.Millisecond)
			}

		case "B", "Q":
			return

		default:
			// Invalid choice, loop continues and redraws clean menu
		}
	}
}

// ResolveSoundPackDir determines the folder path for a given sound pack name
func ResolveSoundPackDir(packName string) string {
	if packName == "Default" || packName == "" {
		return "sounds"
	}
	pDir := filepath.Join("packs", packName)
	if fi, err := os.Stat(pDir); err == nil && fi.IsDir() {
		return pDir
	}
	sDir := "sounds_" + strings.ToLower(packName)
	if fi, err := os.Stat(sDir); err == nil && fi.IsDir() {
		return sDir
	}
	if fi, err := os.Stat(packName); err == nil && fi.IsDir() {
		return packName
	}
	return "sounds"
}

// GetAvailablePacks scans packs/ directory and returns list of sound pack names
func GetAvailablePacks() []string {
	var list []string

	entries, err := os.ReadDir("packs")
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				list = append(list, e.Name())
			}
		}
	}

	list = append(list, "Default")
	return list
}

func selectSoundPackMenu(reader *bufio.Reader) {
	for {
		clearScreen()
		cfg := GetConfig()
		packs := GetAvailablePacks()

		fmt.Print(
			colCyan + colBold +
				" ┌───────────────────────────────────────────────────┐\n" +
				" │   📦  S E L E C T   S O U N D   P A C K           │\n" +
				" └───────────────────────────────────────────────────┘" + colReset + "\n\n" +
				"   Active Sound Pack: " + colMagenta + colBold + cfg.SoundPack + colReset + "\n\n" +
				"   Available Packs:\n")

		for i, p := range packs {
			desc := ""
			switch strings.ToLower(p) {
			case "typewriter":
				desc = " (Vintage Mechanical Typewriter with Bell)"
			case "cherry-g80-3494":
				desc = " (Cherry G80-3494 Smooth Silent Red Linear)"
			case "bubble":
				desc = " (Chill & Playful Popping Bubbles)"
			case "cherrymx-blue":
				desc = " (Loud Clicky - Cherry MX Blue)"
			case "tealios-v2":
				desc = " (Smooth Thocky - ZealPC Tealios V2)"
			case "nk-cream":
				desc = " (POM Mechanical - NovelKeys Cream)"
			case "default":
				desc = " (Default built-in sounds)"
			default:
				desc = " (Custom pack in packs/)"
			}

			if strings.EqualFold(cfg.SoundPack, p) {
				fmt.Printf("   %s %s[%d] %s%s%s\n", colGreen+colBold+"➜", colWhite+colBold, i+1, p, desc, colReset)
			} else {
				fmt.Printf("     %s[%d]%s %s%s\n", colCyan, i+1, colReset, p, colGray+desc+colReset)
			}
		}

		fmt.Print("\n" +
			"   " + colYellow + colBold + "[B]" + colReset + " Back to Settings\n\n" +
			colDim + "   Tip: Drop any Mechvibes or WAV folder into 'packs/'\n" + colReset +
			colGray + " ───────────────────────────────────────────────────\n" + colReset +
			fmt.Sprintf("   Select an option (1-%d or B): ", len(packs)))

		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		choice := strings.TrimSpace(line)
		choiceUpper := strings.ToUpper(choice)

		if choiceUpper == "B" || choiceUpper == "Q" || choice == "" {
			return
		}

		idx, err := strconv.Atoi(choice)
		if err == nil && idx >= 1 && idx <= len(packs) {
			selected := packs[idx-1]
			dir := ResolveSoundPackDir(selected)
			loadErr := GlobalAudio.LoadSounds(dir)
			if loadErr != nil {
				fmt.Printf("\n   %sError loading pack '%s': %v%s\n", colRed, selected, loadErr, colReset)
				time.Sleep(1200 * time.Millisecond)
			} else {
				UpdateConfig(func(c *Config) {
					c.SoundPack = selected
				})
				// Play test click
				_ = GlobalAudio.PlaySound("space", cfg.Volume)
				time.Sleep(120 * time.Millisecond)
				_ = GlobalAudio.PlaySound("key1", cfg.Volume)
				return
			}
		}
	}
}

func showDevToolsMenu(reader *bufio.Reader) {
	for {
		clearScreen()
		keyEvtStr := colGray + "[OFF]" + colReset
		if DebugKeyEvents.Load() {
			keyEvtStr = colGreen + colBold + "[ON - Logging Active]" + colReset
		}
		dbgStr := colGray + "[OFF]" + colReset
		if DebugMode.Load() {
			dbgStr = colGreen + colBold + "[ON]" + colReset
		}

		isAdmin := checkIsAdmin()
		privStr := colYellow + "STANDARD" + colReset
		if isAdmin {
			privStr = colGreen + colBold + "ELEVATED (Administrator)" + colReset
		}

		fmt.Print(
			colCyan + colBold +
				" ┌───────────────────────────────────────────────────┐\n" +
				" │   🛠   D E V   T O O L S                           │\n" +
				" └───────────────────────────────────────────────────┘" + colReset + "\n\n" +
				"   " + colCyan + "[1]" + colReset + " Real-time Keyboard Event Log : " + keyEvtStr + "\n" +
				"   " + colCyan + "[2]" + colReset + " Test All Audio Voices\n" +
				"   " + colCyan + "[3]" + colReset + " Reload Audio Files\n" +
				"   " + colCyan + "[4]" + colReset + " Show Loaded Sounds Info\n" +
				"   " + colCyan + "[5]" + colReset + " Show Memory & Performance Stats\n" +
				"   " + colCyan + "[6]" + colReset + " Debug Mode                   : " + dbgStr + "\n" +
				"   " + colCyan + "[7]" + colReset + " Process Privilege Level      : " + privStr + "\n\n" +
				"   " + colYellow + colBold + "[B]" + colReset + " Back to Main Menu\n\n" +
				colGray + " ───────────────────────────────────────────────────\n" + colReset +
				"   Select an option (1-7 or B): ")

		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		choice := strings.TrimSpace(line)
		choiceUpper := strings.ToUpper(choice)

		switch choiceUpper {
		case "1":
			curr := DebugKeyEvents.Load()
			DebugKeyEvents.Store(!curr)
			if !curr {
				fmt.Println("\n" + colGreen + "   Keyboard Event Logging started!" + colReset)
				fmt.Println(colDim + "   (Press keys anywhere to inspect. Press 1 again or B to exit)" + colReset)
				time.Sleep(1000 * time.Millisecond)
			}

		case "2":
			fmt.Println("\n" + colGreen + "   Testing polyphonic audio on all voices..." + colReset)
			cfg := GetConfig()
			sounds := []string{"key1", "key2", "key3", "space", "enter", "backspace", "shift"}
			for _, s := range sounds {
				fmt.Printf("   Playing %s.wav...\n", s)
				_ = GlobalAudio.PlaySound(s, cfg.Volume)
				time.Sleep(120 * time.Millisecond)
			}
			fmt.Println(colGreen + "   Audio test completed." + colReset)
			time.Sleep(500 * time.Millisecond)

		case "3":
			cfg := GetConfig()
			dir := ResolveSoundPackDir(cfg.SoundPack)
			if err := GlobalAudio.LoadSounds(dir); err != nil {
				fmt.Printf("\n   %sError reloading audio: %v%s\n", colRed, err, colReset)
			} else {
				fmt.Printf("\n   %sAudio files reloaded from '%s'!%s\n", colGreen, dir, colReset)
			}
			time.Sleep(800 * time.Millisecond)

		case "4":
			fmt.Println("\n" + colCyan + "   Loaded Sounds in Memory:" + colReset)
			infos := GlobalAudio.GetLoadedSoundsInfo()
			for _, info := range infos {
				fmt.Println("    • " + info)
			}
			fmt.Print("\n   " + colDim + "Press Enter to continue..." + colReset)
			_, _ = reader.ReadString('\n')

		case "5":
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			fmt.Println("\n" + colCyan + "   Memory & Performance Statistics:" + colReset)
			fmt.Printf("    • Allocated Memory (Alloc) : %s%8.2f KB (%.2f MB)%s\n", colGreen+colBold, float64(m.Alloc)/1024.0, float64(m.Alloc)/(1024.0*1024.0), colReset)
			fmt.Printf("    • Total Memory Allocated   : %8.2f KB\n", float64(m.TotalAlloc)/1024.0)
			fmt.Printf("    • System Memory (Sys)      : %8.2f KB (%.2f MB)\n", float64(m.Sys)/1024.0, float64(m.Sys)/(1024.0*1024.0))
			fmt.Printf("    • Garbage Collections (GC) : %8d\n", m.NumGC)
			fmt.Printf("    • Active Goroutines        : %8d\n", runtime.NumGoroutine())
			fmt.Println("\n   " + colGreen + "MechChill is optimized for minimal memory (<5MB) and zero idle CPU." + colReset)
			fmt.Print("\n   " + colDim + "Press Enter to continue..." + colReset)
			_, _ = reader.ReadString('\n')

		case "6":
			curr := DebugMode.Load()
			DebugMode.Store(!curr)

		case "7":
			if !checkIsAdmin() {
				fmt.Println("\n   " + colYellow + "Relaunching as Administrator..." + colReset)
				relaunchAsAdmin()
			} else {
				fmt.Println("\n   " + colGreen + "Already running with full Administrator privileges!" + colReset)
				time.Sleep(800 * time.Millisecond)
			}

		case "B", "Q":
			return
		}
	}
}

func showAboutMenu(reader *bufio.Reader) {
	clearScreen()
	isAdmin := checkIsAdmin()
	privStr := "Standard User"
	if isAdmin {
		privStr = "Elevated (Administrator)"
	}

	fmt.Print(
		colCyan + colBold +
			" ╔═══════════════════════════════════════════════════════╗\n" +
			" ║             ABOUT MECHCHILL v1.1                      ║\n" +
			" ╚═══════════════════════════════════════════════════════╝" + colReset + "\n\n" +
			"   An ultra-lightweight, zero-idle-CPU mechanical\n" +
			"   keyboard sound engine built natively for Windows.\n\n" +
			"   " + colYellow + colBold + "Highlights:" + colReset + "\n" +
			"   • " + colWhite + "Memory Footprint" + colReset + " : < 5 MB RAM\n" +
			"   • " + colWhite + "Idle CPU Usage" + colReset + "   : 0.0% (Pure Win32 event-driven)\n" +
			"   • " + colWhite + "Audio Engine" + colReset + "     : 16-Voice low-latency waveOut pool\n" +
			"   • " + colWhite + "UIPI Admin Hook" + colReset + "  : " + privStr + " (Keystrokes in all windows)\n" +
			"   • " + colWhite + "System Tray" + colReset + "      : Full background integration\n" +
			"   • " + colWhite + "Sound Packs" + colReset + "      : Modular packs + Mechvibes format support\n\n" +
			"   " + colYellow + colBold + "Tips & Shortcuts:" + colReset + "\n" +
			"   • Press " + colGreen + colBold + "[Q]" + colReset + " anytime to hide console to System Tray\n" +
			"   • Press " + colYellow + colBold + "[A]" + colReset + " in main menu to Relaunch as Administrator\n" +
			"   • Right-click the Tray icon for quick toggle & settings\n" +
			"   • Double-click the Tray icon to restore this console\n" +
			"   • Select 'Exit' from Tray to terminate the application\n\n" +
			"   " + colDim + "Press Enter to return to main menu..." + colReset)
	_, _ = reader.ReadString('\n')
}
