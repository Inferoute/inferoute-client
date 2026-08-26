//go:build windows

package tray

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	nimModify    = 0x00000001
	nifInfo      = 0x00000010
	niifInfo     = 0x00000001
	trayIconID   = 100 // fyne.io/systray uses this id
	systrayClass = "SystrayClass"
)

// Must match NOTIFYICONDATAW / fyne.io/systray so NIM_MODIFY hits the same icon.
type notifyIconData struct {
	Size                       uint32
	Wnd                        windows.Handle
	ID, Flags, CallbackMessage uint32
	Icon                       windows.Handle
	Tip                        [128]uint16
	State, StateMask           uint32
	Info                       [256]uint16
	Timeout, Version           uint32
	InfoTitle                  [64]uint16
	InfoFlags                  uint32
	GuidItem                   windows.GUID
	BalloonIcon                windows.Handle
}

func showStartupNotice() {
	go func() {
		hwnd := findSystrayWindow()
		if hwnd == 0 {
			return
		}

		var nid notifyIconData
		nid.Size = uint32(unsafe.Sizeof(nid))
		nid.Wnd = hwnd
		nid.ID = trayIconID
		nid.Flags = nifInfo
		nid.Timeout = 10000
		nid.InfoFlags = niifInfo
		copyUTF16(nid.InfoTitle[:], "Inferoute Client")
		copyUTF16(nid.Info[:], "Running in the notification area. Right-click the icon to open the dashboard or quit.")

		shell32 := windows.NewLazySystemDLL("shell32.dll")
		_, _, _ = shell32.NewProc("Shell_NotifyIconW").Call(
			uintptr(nimModify),
			uintptr(unsafe.Pointer(&nid)),
		)
	}()
}

func findSystrayWindow() windows.Handle {
	user32 := windows.NewLazySystemDLL("user32.dll")
	findWindow := user32.NewProc("FindWindowW")
	class, err := windows.UTF16PtrFromString(systrayClass)
	if err != nil {
		return 0
	}
	for i := 0; i < 20; i++ {
		r1, _, _ := findWindow.Call(uintptr(unsafe.Pointer(class)), 0)
		if r1 != 0 {
			return windows.Handle(r1)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0
}

func copyUTF16(dst []uint16, s string) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return
	}
	copy(dst, u)
}
