package ActiveWindow

import (
	"golang.org/x/sys/windows"
)

type MessageCAW struct {
	hwnd       HWND
	WindowName string
	WindowRect windows.Rect
	ChanOk     chan int
}
