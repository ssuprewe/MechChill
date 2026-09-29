# MechChill

> Ultra-lightweight, zero-idle-CPU mechanical keyboard sound engine for Windows.

[![Go Version](https://img.shields.io/badge/go-1.21%2B-blue.svg)](https://golang.org)
[![Platform](https://img.shields.io/badge/platform-Windows-0078D6.svg)](#requirements)
[![Architecture](https://img.shields.io/badge/arch-x64-lightgrey.svg)](#building-from-source)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

MechChill brings the tactile, satisfying sound of high-end mechanical keyboards to every keystroke across your Windows system. Built purely in Go with native Win32 API calls (`winmm.dll`, `user32.dll`), MechChill eliminates the high CPU usage, memory bloat, and audio latency common in Electron-based sound utilities.

---

## Highlights

- **Near-Zero Latency Audio**: 16-voice polyphonic audio pool using the native Win32 `waveOut` API, delivering instant click responses and dynamic voice stealing even during rapid typing.
- **Minimal Footprint**: Consumes less than 5 MB of RAM and 0.0% CPU when idle through event-driven Windows message hooks (`WH_KEYBOARD_LL`).
- **Tactile Key Down & Release**: Independent sounds for key downstrokes and upstrokes (bas-çek) to replicate mechanical switch physics.
- **Repeat Suppression**: Configurable filtering prevents machine-gun sound loops when holding keys down.
- **Modular Sound Packs**: Includes pre-loaded switches and supports drag-and-drop custom packs, including Mechvibes sound pack formats.
- **System Tray Integration**: Quietly runs in the background. Minimize to tray with a keystroke and restore at any time.
- **UIPI Elevation**: Built-in one-key elevation (`[A]`) to ensure keystrokes register across administrative windows, games, and Task Manager.
- **Zero External Audio Dependencies**: No CGO, DirectX, SDL, or external audio libraries required.

---

## Requirements

- **Operating System**: Windows 10 / Windows 11 (64-bit recommended)
- **Runtime**: None required for the standalone executable (`MechChill.exe`)
- **Compilation (Optional)**: Go 1.21 or newer

---

## Quick Start

1. Download or clone this repository:
   ```powershell
   git clone https://github.com/your-username/MechChill.git
   cd MechChill
   ```
2. Run `MechChill.exe`.
3. Type anywhere on your keyboard to hear the switch sounds immediately.

> [!TIP]
> Press `[Q]` at any time to hide the console window to the System Tray. Double-click the tray icon or right-click and select **Show Console** to restore it.

---

## Building from Source

To compile the binary yourself using standard Go tooling:

### Using the Build Script
```cmd
build.bat
```

### Manual Compilation
```powershell
go build -ldflags "-s -w" -o MechChill.exe .
```

The resulting executable is fully portable and self-contained. Ensure that the `packs/` and/or `sounds/` directory resides in the same folder as `MechChill.exe`.

---

## Controls and CLI Dashboard

MechChill provides an interactive terminal interface alongside system tray controls:

```text
 ╔═══════════════════════════════════════════════════════╗
 ║   ⌨   M E C H C H I L L  v1.1                         ║
 ║   Ultra-Lightweight Mechanical Sound Engine           ║
 ╚═══════════════════════════════════════════════════════╝

   Status     : ● ACTIVE
   Privileges : ● ELEVATED (Active in ALL windows & Task Manager)
   Volume     : [█████░░░░░] 50%
   Sound Pack : CherryMX-Blue
```

### Menu Options

| Key | Action | Description |
|:---:|:---|:---|
| `1` | **Settings** | Adjust volume (0–100), toggle sound, release sounds, key repeat, and startup |
| `2` | **Sound Test** | Play a sample click sequence to test current audio output |
| `3` | **Reload Sounds** | Hot-reload all sound files from disk without restarting the application |
| `4` | **Dev Tools** | Live keyboard event logger, memory stats, voice allocation inspection |
| `5` | **About** | Application architecture details and shortcuts |
| `A` | **Relaunch as Admin** | Elevates the process to bypass Windows UIPI restrictions |
| `Q` | **Hide to Tray** | Minimizes the terminal window to the notification area |

---

## Sound Packs

MechChill includes several switch profiles in the `packs/` folder:

- **Cherry MX Blue**: Crisp, clicky tactile switches
- **NovelKeys Cream**: Smooth POM linear acoustics
- **ZealPC Tealios V2**: Deep, thocky high-end linear switches
- **Cherry G80-3494**: Soft, smooth silent red linear profile
- **Bubble**: Playful bubble popping effects
- **Vintage Typewriter**: Classic typewriter mechanics with carriage return

### Adding Custom Packs

Create a new subfolder in `packs/<YourPackName>/` with individual `.wav` files:

```text
packs/MySwitch/
├── key1.wav ... key6.wav   # Standard keystrokes (randomized)
├── space.wav               # Spacebar
├── enter.wav               # Enter key
├── backspace.wav           # Backspace key
├── shift.wav               # Shift keys
└── release.wav             # Key release / upstroke (optional)
```

MechChill also reads Mechvibes-style sound packs containing a `config.json` sound map definition.

---

## Configuration

Settings are saved in `config.json` next to the executable and persist between sessions:

```json
{
  "volume": 50,
  "enabled": true,
  "random_sounds": true,
  "sound_pack": "CherryMX-Blue",
  "startup_enabled": false,
  "allow_repeat": false,
  "release_sound": true
}
```

| Field | Type | Default | Description |
|:---|:---:|:---:|:---|
| `volume` | `int` | `50` | Output volume level (0 to 100). |
| `enabled` | `bool` | `true` | Master audio enable/mute toggle. |
| `random_sounds` | `bool` | `true` | Randomizes sound variation across standard alphanumeric keys. |
| `sound_pack` | `string` | `"CherryMX-Blue"` | Active pack folder name inside `packs/`. |
| `startup_enabled` | `bool` | `false` | Registers MechChill in Windows Registry (`Run` key) for auto-start. |
| `allow_repeat` | `bool` | `false` | When `true`, holding down a key continuously retriggers sounds. |
| `release_sound` | `bool` | `true` | Plays an upstroke sound when a key is released. |

---

## Privilege Levels & Windows UIPI

> [!IMPORTANT]
> Under Windows User Interface Privilege Isolation (UIPI), standard user processes cannot receive low-level keyboard hooks while an elevated window (such as Task Manager, administrative command prompts, or games run as admin) has focus.
> 
> Press `[A]` in the menu or run MechChill as Administrator to ensure consistent acoustics across all open software.

> [!NOTE]
> Keystrokes are captured strictly in memory to calculate sound playback triggers and are never logged, cached, or transmitted over any network interface.
