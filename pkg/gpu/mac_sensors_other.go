//go:build !darwin || !cgo

package gpu

// macSensors needs cgo on macOS; without it power and temperature are unknown.
func macSensors() (powerW, tempC float64) { return 0, 0 }
