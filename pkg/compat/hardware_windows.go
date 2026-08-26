//go:build windows

package compat

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func systemRAMBytes() (int64, error) {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	proc := kernel32.NewProc("GlobalMemoryStatusEx")
	var mem memoryStatusEx
	mem.Length = uint32(unsafe.Sizeof(mem))
	r1, _, err := proc.Call(uintptr(unsafe.Pointer(&mem)))
	if r1 == 0 {
		if err != nil {
			return 0, fmt.Errorf("GlobalMemoryStatusEx: %w", err)
		}
		return 0, fmt.Errorf("GlobalMemoryStatusEx failed")
	}
	return int64(mem.TotalPhys), nil
}
