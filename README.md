# ⌨️ MechChill

<div align="center">

**Ultra-Lightweight Mechanical Keyboard Sound Engine for Windows**

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![Platform](https://img.shields.io/badge/Platform-Windows%2010%20%2F%2011-0078D6?style=flat-square&logo=windows)](https://microsoft.com)
[![RAM Usage](https://img.shields.io/badge/RAM-%3C%205%20MB-success?style=flat-square)]()
[![Idle CPU](https://img.shields.io/badge/Idle%20CPU-0.0%25-brightgreen?style=flat-square)]()
[![License](https://img.shields.io/badge/License-MIT-blue?style=flat-square)](LICENSE)

*Satisfying mechanical keyboard clicks with pure native performance. No Electron. No heavy frameworks. Just chill.*

</div>

---

## 🌟 Why MechChill?

Most mechanical keyboard sound simulators (like Mechvibes and similar tools) run on **Electron or Chromium**, easily eating **150–400 MB of RAM** and running active polling loops in the background.

**MechChill** was built to solve this problem:
- **< 5 MB RAM** total footprint in memory.
- **0.0% Idle CPU**: Pure Windows event-driven architecture using native low-level keyboard hooks (`WH_KEYBOARD_LL`) and message pumps.
- **Ultra-low Latency**: Direct Win32 `waveOut` 16-voice polyphonic pool — no sound lag, no cutting off keystroke reverberations even at 150+ WPM.
- **Administrator / UIPI Support**: Works seamlessly across all windows, Task Manager, elevated terminals, and games.
- **Single-Instance Mutex**: Prevents accidental duplicate instances and audio echo.
- **Native ANSI Console UI**: Clean, colorized terminal with in-place screen clearing (`clear / cls`), live volume gauge, and quick status badges.
- **System Tray Integration**: Press `[Q]` or `[Ctrl+C]` to minimize to the background tray; runs quietly without cluttering your taskbar.

---

## 📸 Terminal Interface

```text
 ╔═══════════════════════════════════════════════════════╗
 ║   ⌨   M E C H C H I L L  v1.1                         ║
 ║   Ultra-Lightweight Mechanical Sound Engine           ║
 ╚═══════════════════════════════════════════════════════╝

   Status     : ● ACTIVE
   Privileges : ● ELEVATED (Active in ALL windows & Task Manager)
   Volume     : [██████░░░░] 60%
   Sound Pack : CherryMX-Blue

 ───────────────────────────────────────────────────────
   [1] Settings            (Volume, Packs, Repeat, Bas-Çek)
   [2] Sound Test         (Play sample clicks)
   [3] Reload Sounds      (Reload WAVs from disk)
   [4] Dev Tools          (Debug key events & RAM)
   [5] About              (App info & philosophy)

   [Q] Hide to System Tray  (Runs quietly in background)
 ───────────────────────────────────────────────────────
   Listening for keyboard input...
   ❯ 
```

---

## ✨ Features

- 🎧 **16-Voice Polyphonic WaveOut Engine**: Preloaded in-memory buffers ensure instantaneous response with zero disk I/O while typing.
- 🛡️ **UIPI Elevation Support**: Windows User Interface Privilege Isolation blocks standard apps from hooking administrator windows. MechChill includes built-in elevation detection and one-click `[A]` relaunch as Administrator.
- 🔒 **Single Instance Protection**: A Win32 system mutex ensures only one instance runs at a time. Launching a second instance restores the active console.
- 🔄 **Release Sounds (Bas-Çek)**: Natural up-stroke release sound playback for ultra-realistic mechanical switch simulation.
- 🎲 **Acoustic Variation**: Randomly alternates subtle audio variations for alphanumeric keys so typing sounds organic.
- 📦 **6 Pre-bundled Sound Packs**:
  - **Cherry MX Blue**: Loud, crisp, and clicky.
  - **ZealPC Tealios V2**: Ultra-smooth, thocky linear switch.
  - **NovelKeys Cream**: Deep POM mechanical linear profile.
  - **Vintage Typewriter**: Authentic vintage typewriter strikes and return bell.
  - **Cherry G80-3494**: Silent Red smooth linear switch.
  - **Bubble Pop**: Playful, bubbly popping sound feedback.
- 🧩 **Mechvibes Compatibility**: Automatically parses Mechvibes sliced sound atlas packs (`sound.wav` + `config.json` V1/V2).
- 📌 **System Tray & Background Support**:
  - Closing, pressing `[Q]`, or `[Ctrl+C]` hides the console window into the Windows System Tray.
  - Right-click tray icon to quick-toggle mute or open settings.
  - Double-click tray icon to restore the console.
- 🚀 **Windows Startup Support**: Easily toggle running MechChill on Windows boot directly from the settings menu.

---

## ⌨️ Controls & Shortcuts

| Action | How to Trigger |
| :--- | :--- |
| **Hide to Tray** | Press `[Q]`, `[Ctrl+C]` in console, or click close button `[X]` |
| **Relaunch as Admin** | Press `[A]` in console main menu or select `[9]` in Settings |
| **Restore Console** | Double-click the tray icon or right-click → `Open Console` |
| **Quick Mute / Unmute** | Right-click tray icon → `Enable / Disable Sounds` |
| **Change Volume** | Open Settings (`[1]`) → Select `[1] Volume` |
| **Switch Sound Pack** | Open Settings (`[1]`) → Select `[4] Sound Pack` |
| **Full Exit** | Right-click tray icon → `Exit` |

---

## 📦 Adding Custom Sound Packs

MechChill makes adding new switch sounds effortless. You can drop any pack inside the `packs/` directory:

### Option A: Discrete WAV Files
Create a folder inside `packs/` (e.g. `packs/Holy-Panda/`):
```text
packs/Holy-Panda/
├── key1.wav (or a.wav, b.wav, etc.)
├── key2.wav
├── space.wav
├── enter.wav
├── backspace.wav
├── shift.wav
└── release.wav (optional key release sound)
```

### Option B: Mechvibes Soundpacks
MechChill natively loads Mechvibes packs without conversion! Simply place the pack folder containing:
```text
packs/MyPack/
├── sound.wav
└── config.json
```
MechChill will parse the timing definitions automatically on startup.

---

## 🛠️ Building from Source

### Prerequisites
- **Windows 10 / 11** (x64)
- **Go 1.21+** installed ([golang.org](https://go.dev/))

### Compile:
Run the included build script:
```cmd
build.bat
```
Or build manually with optimized flags:
```powershell
go build -ldflags "-s -w" -o MechChill.exe .
```

The resulting `MechChill.exe` is a single, portable executable with zero runtime dependencies.

---

## 📄 License

This project is licensed under the [MIT License](LICENSE).
Feel free to use, modify, and distribute it!
