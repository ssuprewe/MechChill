package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	modWinmm                 = syscall.NewLazyDLL("winmm.dll")
	procWaveOutOpen          = modWinmm.NewProc("waveOutOpen")
	procWaveOutClose         = modWinmm.NewProc("waveOutClose")
	procWaveOutPrepareHeader = modWinmm.NewProc("waveOutPrepareHeader")
	procWaveOutUnprepare     = modWinmm.NewProc("waveOutUnprepareHeader")
	procWaveOutWrite         = modWinmm.NewProc("waveOutWrite")
	procWaveOutReset         = modWinmm.NewProc("waveOutReset")
	procWaveOutSetVolume     = modWinmm.NewProc("waveOutSetVolume")
	procPlaySoundW           = modWinmm.NewProc("PlaySoundW")
)

const (
	WAVE_MAPPER     = ^uintptr(0)
	WAVE_FORMAT_PCM = 1

	SND_ASYNC     = 0x0001
	SND_NODEFAULT = 0x0002
	SND_MEMORY    = 0x0004

	NumVoices = 16 // Polyphony pool size for fast typing without clipping
)

type WAVEFORMATEX struct {
	wFormatTag      uint16
	nChannels       uint16
	nSamplesPerSec  uint32
	nAvgBytesPerSec uint32
	nBlockAlign     uint16
	wBitsPerSample  uint16
	cbSize          uint16
}

type WAVEHDR struct {
	lpData          uintptr
	dwBufferLength  uint32
	dwBytesRecorded uint32
	dwUser          uintptr
	dwFlags         uint32
	dwLoops         uint32
	lpNext          uintptr
	reserved        uintptr
}

type SoundData struct {
	Name       string
	RawWav     []byte
	PcmData    []byte
	Format     WAVEFORMATEX
	DurationMs float64
}

type Voice struct {
	hWave      uintptr
	format     WAVEFORMATEX
	hdr        WAVEHDR
	isOpen     bool
	isPrepared bool
}

type AudioEngine struct {
	mu            sync.Mutex
	sounds        map[string]*SoundData
	keyVariations []string
	voices        [NumVoices]Voice
	voiceIndex    int
	randomGen     *rand.Rand
	soundsDir     string
}

var GlobalAudio = NewAudioEngine()

func NewAudioEngine() *AudioEngine {
	return &AudioEngine{
		sounds:    make(map[string]*SoundData),
		randomGen: rand.New(rand.NewSource(time.Now().UnixNano())),
		soundsDir: "sounds",
	}
}

// Init initializes the audio engine and loads sounds
func (ae *AudioEngine) Init(soundsDir string) error {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	ae.soundsDir = soundsDir
	return ae.loadSoundsLocked(soundsDir)
}

// Close frees all waveOut handles
func (ae *AudioEngine) Close() {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	for i := 0; i < NumVoices; i++ {
		v := &ae.voices[i]
		if v.isOpen {
			procWaveOutReset.Call(v.hWave)
			if v.isPrepared {
				procWaveOutUnprepare.Call(v.hWave, uintptr(unsafe.Pointer(&v.hdr)), unsafe.Sizeof(v.hdr))
				v.isPrepared = false
			}
			procWaveOutClose.Call(v.hWave)
			v.isOpen = false
			v.hWave = 0
		}
	}
}

// LoadSounds reloads sounds from disk into memory
func (ae *AudioEngine) LoadSounds(dir string) error {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	return ae.loadSoundsLocked(dir)
}

func (ae *AudioEngine) loadSoundsLocked(dir string) error {
	ae.soundsDir = dir
	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read sound directory '%s': %w", dir, err)
	}

	newSounds := make(map[string]*SoundData)
	var variations []string
	var wavCount int

	for _, entry := range files {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".wav") {
			continue
		}
		// Don't count "sound.wav" if it's part of a Mechvibes atlas
		if strings.EqualFold(entry.Name(), "sound.wav") {
			continue
		}

		wavCount++
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		baseName := strings.TrimSuffix(strings.ToLower(entry.Name()), ".wav")
		sd, err := parseWavData(baseName, data)
		if err != nil {
			newSounds[baseName] = &SoundData{
				Name:   baseName,
				RawWav: data,
			}
		} else {
			newSounds[baseName] = sd
		}

		// Register key variations: key*, single letter, or numbered (1..99)
		isNumberedKey := false
		if _, err := strconv.Atoi(baseName); err == nil {
			isNumberedKey = true
		}
		if strings.HasPrefix(baseName, "key") || len(baseName) == 1 || isNumberedKey {
			variations = append(variations, baseName)
		}
	}

	// Check if this is a Mechvibes sliced soundpack (config.json + sound.wav)
	cfgPath := filepath.Join(dir, "config.json")
	soundWavPath := filepath.Join(dir, "sound.wav")

	if wavCount < 2 && fileExists(cfgPath) && fileExists(soundWavPath) {
		slicedSounds, slicedVars, err := loadMechvibesPack(cfgPath, soundWavPath)
		if err == nil && len(slicedSounds) > 0 {
			for k, v := range slicedSounds {
				newSounds[k] = v
			}
			variations = slicedVars
		}
	}

	// Normalize key mappings (ensure space, enter, backspace, shift exist if alternatives present)
	normalizeKeyMappings(newSounds)

	// If no specific key variations, default to "key1" or any loaded sound
	if len(variations) == 0 {
		if _, ok := newSounds["key1"]; ok {
			variations = append(variations, "key1")
		} else {
			for name := range newSounds {
				if name != "space" && name != "enter" && name != "backspace" && name != "shift" {
					variations = append(variations, name)
				}
			}
		}
	}

	ae.sounds = newSounds
	ae.keyVariations = variations
	return nil
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func normalizeKeyMappings(sounds map[string]*SoundData) {
	// Alias return / ret / return-new / ent -> enter
	if _, ok := sounds["enter"]; !ok {
		for _, alt := range []string{"return", "return-new", "ret", "ent"} {
			if s, ok := sounds[alt]; ok {
				sounds["enter"] = s
				break
			}
		}
	}
	// Alias back / back-new -> backspace
	if _, ok := sounds["backspace"]; !ok {
		for _, alt := range []string{"back", "back-new"} {
			if s, ok := sounds[alt]; ok {
				sounds["backspace"] = s
				break
			}
		}
	}
	// Alias space-new -> space
	if _, ok := sounds["space"]; !ok {
		if s, ok := sounds["space-new"]; ok {
			sounds["space"] = s
		}
	}
}

// Mechvibes sound pack loader (V1 and V2)
func loadMechvibesPack(cfgPath, wavPath string) (map[string]*SoundData, []string, error) {
	wavData, err := os.ReadFile(wavPath)
	if err != nil {
		return nil, nil, err
	}

	baseSound, err := parseWavData("atlas", wavData)
	if err != nil {
		return nil, nil, err
	}

	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, nil, err
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(cfgData, &parsed); err != nil {
		return nil, nil, err
	}

	sounds := make(map[string]*SoundData)
	var variations []string

	bytesPerSec := float64(baseSound.Format.nAvgBytesPerSec)
	blockAlign := int(baseSound.Format.nBlockAlign)
	if blockAlign <= 0 {
		blockAlign = 2
	}

	extractSlice := func(name string, startMs, endMs float64) {
		if endMs <= startMs || bytesPerSec <= 0 {
			return
		}
		// Add 25ms natural tail
		endMs += 25.0

		startByte := int((startMs / 1000.0) * bytesPerSec)
		endByte := int((endMs / 1000.0) * bytesPerSec)

		// Align to block
		startByte = (startByte / blockAlign) * blockAlign
		endByte = (endByte / blockAlign) * blockAlign

		if startByte >= len(baseSound.PcmData) {
			return
		}
		if endByte > len(baseSound.PcmData) {
			endByte = len(baseSound.PcmData)
		}

		pcmSlice := baseSound.PcmData[startByte:endByte]
		dur := float64(len(pcmSlice)) / bytesPerSec * 1000.0

		sounds[name] = &SoundData{
			Name:       name,
			PcmData:    pcmSlice,
			Format:     baseSound.Format,
			DurationMs: dur,
		}
	}

	// 1. Try V2 format: "definitions": { "Space": { "timing": [[start, end]] } }
	if defs, ok := parsed["definitions"].(map[string]interface{}); ok {
		keyNameMap := map[string]string{
			"Space":      "space",
			"Enter":      "enter",
			"Backspace":  "backspace",
			"ShiftLeft":  "shift",
			"ShiftRight": "shift",
			"KeyA":       "key1",
			"KeyB":       "key2",
			"KeyC":       "key3",
			"KeyD":       "key4",
			"KeyE":       "key5",
			"KeyF":       "key6",
		}

		for defKey, targetName := range keyNameMap {
			if keyObj, ok := defs[defKey].(map[string]interface{}); ok {
				if timingList, ok := keyObj["timing"].([]interface{}); ok && len(timingList) > 0 {
					if tPair, ok := timingList[0].([]interface{}); ok && len(tPair) >= 2 {
						sMs, _ := tPair[0].(float64)
						eMs, _ := tPair[1].(float64)
						extractSlice(targetName, sMs, eMs)
						if strings.HasPrefix(targetName, "key") {
							variations = append(variations, targetName)
						}
					}
				}
			}
		}
	}

	// 2. Try V1 format: "defines": { "57": [start_ms, duration_ms] }
	if defs, ok := parsed["defines"].(map[string]interface{}); ok {
		v1KeyMap := map[string]string{
			"57": "space",     // Space
			"28": "enter",     // Enter
			"14": "backspace", // Backspace
			"42": "shift",     // Left Shift
			"54": "shift",     // Right Shift
			"30": "key1",      // A
			"48": "key2",      // B
			"46": "key3",      // C
			"32": "key4",      // D
			"18": "key5",      // E
			"33": "key6",      // F
		}

		for code, targetName := range v1KeyMap {
			if val, ok := defs[code].([]interface{}); ok && len(val) >= 2 {
				sMs, _ := val[0].(float64)
				durMs, _ := val[1].(float64)
				extractSlice(targetName, sMs, sMs+durMs)
				if strings.HasPrefix(targetName, "key") {
					variations = append(variations, targetName)
				}
			}
		}
	}

	return sounds, variations, nil
}

func parseWavData(name string, data []byte) (*SoundData, error) {
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, fmt.Errorf("invalid RIFF WAVE header")
	}

	r := bytes.NewReader(data[12:])
	var wfx WAVEFORMATEX
	var pcmData []byte

	for r.Len() >= 8 {
		var chunkID [4]byte
		var chunkSize uint32
		if err := binary.Read(r, binary.LittleEndian, &chunkID); err != nil {
			break
		}
		if err := binary.Read(r, binary.LittleEndian, &chunkSize); err != nil {
			break
		}

		cName := string(chunkID[:])
		if cName == "fmt " {
			var formatTag, channels, blockAlign, bits uint16
			var sampleRate, byteRate uint32
			binary.Read(r, binary.LittleEndian, &formatTag)
			binary.Read(r, binary.LittleEndian, &channels)
			binary.Read(r, binary.LittleEndian, &sampleRate)
			binary.Read(r, binary.LittleEndian, &byteRate)
			binary.Read(r, binary.LittleEndian, &blockAlign)
			binary.Read(r, binary.LittleEndian, &bits)

			wfx.wFormatTag = formatTag
			wfx.nChannels = channels
			wfx.nSamplesPerSec = sampleRate
			wfx.nAvgBytesPerSec = byteRate
			wfx.nBlockAlign = blockAlign
			wfx.wBitsPerSample = bits
			wfx.cbSize = 0

			if chunkSize > 16 {
				r.Seek(int64(chunkSize-16), 1)
			}
		} else if cName == "data" {
			if chunkSize > uint32(r.Len()) {
				chunkSize = uint32(r.Len())
			}
			pcmData = make([]byte, chunkSize)
			binary.Read(r, binary.LittleEndian, &pcmData)
			break
		} else {
			r.Seek(int64(chunkSize), 1)
		}
	}

	if len(pcmData) == 0 {
		return nil, fmt.Errorf("no PCM data chunk found in WAV")
	}

	duration := 0.0
	if wfx.nAvgBytesPerSec > 0 {
		duration = float64(len(pcmData)) / float64(wfx.nAvgBytesPerSec) * 1000.0
	}

	return &SoundData{
		Name:       name,
		RawWav:     data,
		PcmData:    pcmData,
		Format:     wfx,
		DurationMs: duration,
	}, nil
}

// PlaySound plays a loaded sound by identifier
func (ae *AudioEngine) PlaySound(name string, volume int) error {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	sd, ok := ae.sounds[strings.ToLower(name)]
	if !ok {
		// Fallback to "key1" or any sound
		sd = ae.fallbackSoundLocked()
		if sd == nil {
			return fmt.Errorf("no sounds loaded")
		}
	}

	return ae.playSoundLocked(sd, volume)
}

func (ae *AudioEngine) fallbackSoundLocked() *SoundData {
	if s, ok := ae.sounds["key1"]; ok {
		return s
	}
	if s, ok := ae.sounds["space"]; ok {
		return s
	}
	for _, s := range ae.sounds {
		return s
	}
	return nil
}

func (ae *AudioEngine) playSoundLocked(sd *SoundData, volume int) error {
	if volume <= 0 {
		return nil
	}
	if volume > 100 {
		volume = 100
	}

	// Low-latency waveOut voice pool
	if len(sd.PcmData) > 0 && sd.Format.wFormatTag == WAVE_FORMAT_PCM {
		vIdx := ae.voiceIndex
		ae.voiceIndex = (ae.voiceIndex + 1) % NumVoices
		v := &ae.voices[vIdx]

		needReopen := !v.isOpen ||
			v.format.nSamplesPerSec != sd.Format.nSamplesPerSec ||
			v.format.nChannels != sd.Format.nChannels ||
			v.format.wBitsPerSample != sd.Format.wBitsPerSample

		if needReopen {
			if v.isOpen {
				procWaveOutReset.Call(v.hWave)
				if v.isPrepared {
					procWaveOutUnprepare.Call(v.hWave, uintptr(unsafe.Pointer(&v.hdr)), unsafe.Sizeof(v.hdr))
					v.isPrepared = false
				}
				procWaveOutClose.Call(v.hWave)
				v.isOpen = false
			}

			var h uintptr
			ret, _, _ := procWaveOutOpen.Call(
				uintptr(unsafe.Pointer(&h)),
				WAVE_MAPPER,
				uintptr(unsafe.Pointer(&sd.Format)),
				0,
				0,
				0,
			)
			if ret == 0 {
				v.hWave = h
				v.isOpen = true
				v.format = sd.Format
			}
		}

		if v.isOpen {
			// Set stream volume
			volVal := uint32(float64(volume) / 100.0 * 65535.0)
			dwVol := (volVal << 16) | volVal
			procWaveOutSetVolume.Call(v.hWave, uintptr(dwVol))

			procWaveOutReset.Call(v.hWave)
			if v.isPrepared {
				procWaveOutUnprepare.Call(v.hWave, uintptr(unsafe.Pointer(&v.hdr)), unsafe.Sizeof(v.hdr))
				v.isPrepared = false
			}

			v.hdr = WAVEHDR{
				lpData:         uintptr(unsafe.Pointer(&sd.PcmData[0])),
				dwBufferLength: uint32(len(sd.PcmData)),
			}

			prepRet, _, _ := procWaveOutPrepareHeader.Call(v.hWave, uintptr(unsafe.Pointer(&v.hdr)), unsafe.Sizeof(v.hdr))
			if prepRet == 0 {
				v.isPrepared = true
				procWaveOutWrite.Call(v.hWave, uintptr(unsafe.Pointer(&v.hdr)), unsafe.Sizeof(v.hdr))
				return nil
			}
		}
	}

	// Fallback to PlaySoundW
	if len(sd.RawWav) > 0 {
		procPlaySoundW.Call(
			uintptr(unsafe.Pointer(&sd.RawWav[0])),
			0,
			uintptr(SND_MEMORY|SND_ASYNC|SND_NODEFAULT),
		)
		return nil
	}

	return fmt.Errorf("unable to play sound: %s", sd.Name)
}

// PlayKeySound maps a virtual key code to the appropriate sound and plays it
func (ae *AudioEngine) PlayKeySound(vkCode uint32, randomSounds bool, volume int) string {
	var soundName string

	switch vkCode {
	case 0x20: // Space
		soundName = "space"
	case 0x0D: // Enter
		soundName = "enter"
	case 0x08, 0x2E: // Backspace, Delete
		soundName = "backspace"
	case 0x10, 0xA0, 0xA1: // Shift
		soundName = "shift"
	default:
		ae.mu.Lock()
		vars := ae.keyVariations
		ae.mu.Unlock()

		if len(vars) > 0 {
			if randomSounds {
				soundName = vars[ae.randomGen.Intn(len(vars))]
			} else {
				soundName = vars[0]
			}
		} else {
			soundName = "key1"
		}
	}

	_ = ae.PlaySound(soundName, volume)
	return soundName
}

// PlayKeyUpSound plays release up-stroke sound on key release (bas-çek)
func (ae *AudioEngine) PlayKeyUpSound(vkCode uint32, volume int) string {
	// Release sounds are naturally slightly softer (~75% volume)
	relVol := int(float64(volume) * 0.75)
	if relVol < 1 && volume > 0 {
		relVol = 1
	}

	var target string
	switch vkCode {
	case 0x20: // Space
		target = "space_up"
	case 0x0D: // Enter
		target = "enter_up"
	case 0x08, 0x2E: // Backspace, Delete
		target = "backspace_up"
	case 0x10, 0xA0, 0xA1: // Shift
		target = "shift_up"
	default:
		target = "key_up"
	}

	ae.mu.Lock()
	_, hasTarget := ae.sounds[target]
	_, hasRelease := ae.sounds["release"]
	ae.mu.Unlock()

	soundToPlay := target
	if !hasTarget {
		if hasRelease {
			soundToPlay = "release"
		} else {
			return "" // No release sound available in this pack, keep silent on release
		}
	}

	_ = ae.PlaySound(soundToPlay, relVol)
	return soundToPlay
}

// GetLoadedSoundsInfo returns formatted status of all loaded sounds
func (ae *AudioEngine) GetLoadedSoundsInfo() []string {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	var result []string
	if len(ae.sounds) == 0 {
		result = append(result, "No sound files loaded.")
		return result
	}

	for name, sd := range ae.sounds {
		var info string
		if sd.Format.wFormatTag == WAVE_FORMAT_PCM {
			info = fmt.Sprintf("%-12s: %5d Hz, %d ch, %2d-bit (%.1f ms, %d bytes)",
				name+".wav", sd.Format.nSamplesPerSec, sd.Format.nChannels, sd.Format.wBitsPerSample, sd.DurationMs, len(sd.PcmData))
		} else {
			info = fmt.Sprintf("%-12s: Raw WAV (%d bytes)", name+".wav", len(sd.RawWav))
		}
		result = append(result, info)
	}
	return result
}
