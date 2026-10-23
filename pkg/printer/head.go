package printer

import (
	"math"
	"sync"
	"time"
)

// BurnRow represents one 384-dot horizontal printed line on the paper.
// Each dot is an 8-bit intensity (0 = paper white, 255 = saturated thermal black).
type BurnRow struct {
	Dots []uint8
}

// ThermalHeadListener is notified when a scanline is burned or paper is advanced.
type ThermalHeadListener interface {
	OnRowBurned(rowIndex int, row []uint8)
	OnPaperCleared()
	OnStatusChanged()
}

// ThermalHead models the physical thermal printhead, motor, and paper roll.
type ThermalHead struct {
	mu sync.RWMutex

	dotsPerLine int
	paperStrip  [][]uint8 // Accumulated paper strip (each row is dotsPerLine bytes)

	// Physics timing mode: if true, pauses goroutines to match real hardware speed.
	RealTimePhysics bool

	// Dynamic Thermal State
	ambientTemp float64 // Ambient temperature (default 25.0°C)
	currentTemp float64 // Current head temperature (°C)
	lastStepTime time.Time

	// Power Supply Simulation
	sourceVoltage float64 // Open-circuit supply voltage (e.g. 7.2V)
	supplyImpedance float64 // Ohms (default 0.25 ohm)

	// Listeners for UI & event streaming
	listeners []ThermalHeadListener

	// Active profile and reset callback for brownout reboot
	profile       Profile
	resetCallback func()
}

// NewThermalHead constructs a new ThermalHead.
func NewThermalHead(dotsPerLine int) *ThermalHead {
	return &ThermalHead{
		dotsPerLine:     dotsPerLine,
		paperStrip:      make([][]uint8, 0, 1024),
		RealTimePhysics: false, // Default to instant for fast tests; enabled in CLI
		ambientTemp:     25.0,
		currentTemp:     25.0,
		lastStepTime:    time.Now(),
		sourceVoltage:   7.2,
		supplyImpedance: 0.25,
	}
}

// AddListener registers a listener for real-time events.
func (th *ThermalHead) AddListener(l ThermalHeadListener) {
	th.mu.Lock()
	defer th.mu.Unlock()
	th.listeners = append(th.listeners, l)
}

// SetProfile associates the active printer profile with the thermal head.
func (th *ThermalHead) SetProfile(prof Profile) {
	th.mu.Lock()
	defer th.mu.Unlock()
	th.profile = prof
}

// SetResetCallback configures a callback invoked on MCU brownout reset.
func (th *ThermalHead) SetResetCallback(cb func()) {
	th.mu.Lock()
	defer th.mu.Unlock()
	th.resetCallback = cb
}

// SetRealTimePhysics toggles real-time microsecond sleep pauses.
func (th *ThermalHead) SetRealTimePhysics(enabled bool) {
	th.mu.Lock()
	defer th.mu.Unlock()
	th.RealTimePhysics = enabled
}

// TotalRows returns the number of vertical dot rows currently on the paper strip.
func (th *ThermalHead) TotalRows() int {
	th.mu.RLock()
	defer th.mu.RUnlock()
	return len(th.paperStrip)
}

// GetPaperStrip returns a snapshot copy of the printed paper strip.
func (th *ThermalHead) GetPaperStrip() [][]uint8 {
	th.mu.RLock()
	defer th.mu.RUnlock()
	copied := make([][]uint8, len(th.paperStrip))
	for i, r := range th.paperStrip {
		c := make([]uint8, len(r))
		copy(c, r)
		copied[i] = c
	}
	return copied
}

// ClearPaper tears/clears the printed paper strip.
func (th *ThermalHead) ClearPaper() {
	th.mu.Lock()
	th.paperStrip = make([][]uint8, 0, 1024)
	listeners := append([]ThermalHeadListener(nil), th.listeners...)
	th.mu.Unlock()

	for _, l := range listeners {
		l.OnPaperCleared()
	}
}

// BurnRow fires thermal strobes across a single horizontal line of 384 dots,
// advances the stepper motor by 1 dot row (0.125 mm), and returns the elapsed duration.
func (th *ThermalHead) BurnRow(rawBits []uint8, reg *Registers) (time.Duration, bool) {
	th.mu.Lock()

	// Update passive cooling since last step
	now := time.Now()
	elapsed := now.Sub(th.lastStepTime).Seconds()
	th.lastStepTime = now
	// Exponential cooling: T = T_amb + (T - T_amb) * e^(-t / tau), tau ~ 8.0s
	th.currentTemp = th.ambientTemp + (th.currentTemp-th.ambientTemp)*math.Exp(-elapsed/8.0)
	reg.HeadTemperature = th.currentTemp

	// 1. Check hardware fault cutoffs
	if reg.PaperState == PaperOut || reg.PlatenOpen || reg.HeadTemperature > 60.0 {
		th.mu.Unlock()
		return 0, false // Mechanism halted
	}

	// 2. Count active black dots in this line
	activeDots := 0
	for _, b := range rawBits {
		if b > 0 {
			activeDots++
		}
	}

	// 3. Calculate heating physics and dot darkness
	// Strobe size = 8 * (n1 + 1). Default n1=7 => 64 dots per strobe group.
	dotsPerStrobe := int(reg.MaxHeatingDots+1) * 8
	if dotsPerStrobe < 8 {
		dotsPerStrobe = 8
	}
	strobeGroups := int(math.Ceil(float64(activeDots) / float64(dotsPerStrobe)))
	if strobeGroups < 1 && activeDots > 0 {
		strobeGroups = 1
	}

	// Microseconds per strobe: heat time + heat interval
	heatTimeUs := int(reg.HeatingTime) * 10       // e.g. 80 * 10 = 800 us
	heatIntervalUs := int(reg.HeatingInterval) * 10 // e.g. 2 * 10 = 20 us
	breakTimeUs := int((reg.PrintDensity >> 5) & 0x07) * 250 // D7..D5 * 250 us
	motorStepUs := 2100 // Nominal stepper advance duration (~2.1 ms per dot row)

	totalRowDurationUs := (strobeGroups * (heatTimeUs + heatIntervalUs)) + breakTimeUs + motorStepUs
	burnDuration := time.Duration(totalRowDurationUs) * time.Microsecond

	// Check voltage sag under instantaneous peak current:
	// Peak current: each heating dot element is ~180 ohms. 64 dots at 7.2V = ~2.56A.
	peakCurrent := float64(min(activeDots, dotsPerStrobe)) * (reg.SupplyVoltage / 180.0)
	voltageSag := peakCurrent * th.supplyImpedance
	effectiveVoltage := reg.SupplyVoltage - voltageSag

	// Brownout reset check: if effective voltage drops < 4.5V, CPU resets!
	if effectiveVoltage < 4.5 && activeDots > 120 {
		th.mu.Unlock()
		if th.resetCallback != nil {
			th.resetCallback()
		} else if th.profile != nil {
			reg.Reset(th.profile)
		}
		return burnDuration, false
	}

	// Calculate dot darkness intensity (0..255) based on heat energy
	// Energy factor = (effectiveVoltage / 7.2)^2 * (heatTimeUs / 800.0) * (0.50 + 0.05 * density)
	densityVal := float64(reg.PrintDensity & 0x1F) // D4..D0
	densityFactor := 0.50 + (0.05 * densityVal)
	voltageFactor := math.Pow(effectiveVoltage/7.2, 2.0)
	heatFactor := float64(heatTimeUs) / 800.0
	energy := voltageFactor * heatFactor * densityFactor

	var dotDarkness uint8
	if energy < 0.25 {
		dotDarkness = 0 // Insufficient heat to activate leuco-dye (blank page!)
	} else if energy < 0.65 {
		dotDarkness = uint8(60 + (energy-0.25)*(120/0.40)) // Faint gray
	} else if energy < 1.0 {
		dotDarkness = uint8(180 + (energy-0.65)*(65/0.35)) // Dark gray to black
	} else {
		dotDarkness = 255 // Saturated thermal black
	}

	// Construct row
	row := make([]uint8, th.dotsPerLine)
	for i := 0; i < th.dotsPerLine && i < len(rawBits); i++ {
		if rawBits[i] > 0 {
			row[i] = dotDarkness
		} else {
			row[i] = 0 // White paper
		}
	}

	// Accumulate heat in ceramic/aluminum thermal head heatsink
	th.currentTemp += float64(activeDots*heatTimeUs) / 2500000.0
	reg.HeadTemperature = th.currentTemp

	th.paperStrip = append(th.paperStrip, row)
	rowIndex := len(th.paperStrip) - 1

	listeners := append([]ThermalHeadListener(nil), th.listeners...)
	realTime := th.RealTimePhysics
	th.mu.Unlock()

	// Notify listeners
	for _, l := range listeners {
		l.OnRowBurned(rowIndex, row)
	}

	if realTime {
		time.Sleep(burnDuration)
	}

	return burnDuration, true
}

// FeedRows feeds blank paper by the specified number of dot rows.
func (th *ThermalHead) FeedRows(count int, reg *Registers) time.Duration {
	if count <= 0 {
		return 0
	}
	emptyBits := make([]uint8, th.dotsPerLine)
	totalDuration := time.Duration(0)

	for i := 0; i < count; i++ {
		dur, ok := th.BurnRow(emptyBits, reg)
		if !ok {
			break
		}
		totalDuration += dur
	}
	return totalDuration
}

// CurrentLEDState returns the front panel LED blink code (1, 2, 3, or 5 blinks).
func (th *ThermalHead) CurrentLEDState(reg *Registers) int {
	th.mu.RLock()
	defer th.mu.RUnlock()

	if reg.HeadTemperature > 60.0 {
		return 5 // 5 blinks: Overheating
	}
	if reg.PaperState == PaperOut {
		return 3 // 3 blinks: Paper out
	}
	if reg.PlatenOpen || reg.HeadTemperature < 0.0 {
		return 2 // 2 blinks: Platen open or ununited
	}
	return 1 // 1 blink: Normal operation
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
