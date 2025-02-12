package ActiveWindow

import (
	"context"
	"golang.org/x/sys/windows"
	"log"
	"log/slog"
	"syscall"
	"unsafe"
)

var (
	messagesCAW = make(chan MessageCAW, 10)
	user32      = windows.NewLazyDLL("user32.dll")
	kernel32    = windows.NewLazyDLL("kernel32.dll")

	procSetWinEventHook  = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent   = user32.NewProc("UnhookWinEvent")
	procGetMessage       = user32.NewProc("GetMessageW")
	procGetWindowText    = user32.NewProc("GetWindowTextW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessage  = user32.NewProc("DispatchMessageW")
	procGetWindowRect    = user32.NewProc("GetWindowRect")

	procGetModuleHandle     = kernel32.NewProc("GetModuleHandleW")
	procGetWindowTextLength = user32.NewProc("GetWindowTextLengthW")

	ActiveWinEventHook WINEVENTPROC = func(hWinEventHook HWINEVENTHOOK, event uint32, hwnd HWND, idObject int32, idChild int32, idEventThread uint32, dwmsEventTime uint32) uintptr {
		relayMessage(hwnd)
		//		log.Println("fond", WinText)
		return 0
	}
)

type WINEVENTPROC func(hWinEventHook HWINEVENTHOOK, event uint32, hwnd HWND, idObject int32, idChild int32, idEventThread uint32, dwmsEventTime uint32) uintptr

type (
	HANDLE        uintptr
	HINSTANCE     HANDLE
	HHOOK         HANDLE
	HMODULE       HANDLE
	HWINEVENTHOOK HANDLE
	DWORD         uint32
	INT           int
	WPARAM        uintptr
	LPARAM        uintptr
	LRESULT       uintptr
	HWND          HANDLE
	UINT          uint32
	BOOL          int32
	ULONG_PTR     uintptr
	LONG          int32
	LPWSTR        *WCHAR
	WCHAR         uint16
)

type POINT struct {
	X, Y int32
}

type MSG struct {
	Hwnd    HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

type RECT struct {
	left   int32 // or Left, Top, etc. if this type is to be exported
	top    int32
	right  int32
	bottom int32
}

const (
	EVENT_SYSTEM_FOREGROUND = 3
	WINEVENT_OUTOFCONTEXT   = 0
	WINEVENT_INCONTEXT      = 4
	WINEVENT_SKIPOWNPROCESS = 2
	WINEVENT_SKIPOWNTHREAD  = 1
)

func Subscribe(ctx context.Context, msgCAW chan MessageCAW) {

	winEvHook := SetWinEventHook(EVENT_SYSTEM_FOREGROUND, EVENT_SYSTEM_FOREGROUND, 0, ActiveWinEventHook, 0, 0, WINEVENT_OUTOFCONTEXT|WINEVENT_SKIPOWNPROCESS)
	slog.Info("Windows Event Hook: ", slog.Any("handler", winEvHook))

	go func() {
		for {
			select {
			case <-ctx.Done():
				slog.Info("Exit Event Hook")
				UnhookWinEvent(winEvHook)
				return
			case msg := <-messagesCAW:
				msgCAW <- msg
				close(msg.ChanOk)
			}
		}
	}()

	var msg MSG
	go func() {
		if m := GetMessage(&msg, 0, 0, 0); m != 0 {
			TranslateMessage(&msg)
			DispatchMessage(&msg)
		}
	}()
}

func relayMessage(h HWND) {
	msgCAW := MessageCAW{
		hwnd:       h,
		WindowName: GetWindowText(h),
		WindowRect: GetWindowRect(h),
	}

	msgCAW.ChanOk = make(chan int)

	messagesCAW <- msgCAW

	<-msgCAW.ChanOk
}

func SetWinEventHook(eventMin DWORD, eventMax DWORD, hmodWinEventProc HMODULE, pfnWinEventProc WINEVENTPROC, idProcess DWORD, idThread DWORD, dwFlags DWORD) HWINEVENTHOOK {
	pfnWinEventProcCallback := syscall.NewCallback(pfnWinEventProc)
	ret, _, _ := procSetWinEventHook.Call(
		uintptr(eventMin),
		uintptr(eventMax),
		uintptr(hmodWinEventProc),
		pfnWinEventProcCallback,
		uintptr(idProcess),
		uintptr(idThread),
		uintptr(dwFlags),
	)
	return HWINEVENTHOOK(ret)
}

func UnhookWinEvent(hWinEventHook HWINEVENTHOOK) bool {
	ret, _, _ := procUnhookWinEvent.Call(
		uintptr(hWinEventHook),
	)
	return ret != 0
}

func GetWindowText(hwnd HWND) string {
	textLen := GetWindowTextLength(hwnd) + 1

	buf := make([]uint16, textLen)
	_, _, err := procGetWindowText.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(textLen))
	if (err != nil) && (err.Error() != "The operation completed successfully.") {
		slog.Error("GetWindowText:", slog.Any("error", err))
	}

	return syscall.UTF16ToString(buf)
}

func GetWindowTextLength(hwnd HWND) int {
	ret, _, _ := procGetWindowTextLength.Call(
		uintptr(hwnd))

	return int(ret)
}

func GetWindowRect(hwnd HWND) windows.Rect {
	var rect windows.Rect
	_, _, err := procGetWindowRect.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&rect)))
	if (err != nil) && (err.Error() != "The operation completed successfully.") {
		slog.Error("GetWindowRect", slog.Any("error ", err))
		return rect
	}

	return rect
}

func GetModuleHandle(modulename string) HINSTANCE {
	var mn uintptr
	if modulename == "" {
		mn = 0
	} else {
		uMN, err := syscall.UTF16PtrFromString(modulename)
		if err != nil {
			log.Println(err)
		}
		mn = uintptr(unsafe.Pointer(uMN))
	}
	ret, _, _ := procGetModuleHandle.Call(mn)
	return HINSTANCE(ret)
}

func GetMessage(msg *MSG, hwnd HWND, msgFilterMin UINT, msgFilterMax UINT) int {
	ret, _, _ := procGetMessage.Call(
		uintptr(unsafe.Pointer(msg)),
		uintptr(hwnd),
		uintptr(msgFilterMin),
		uintptr(msgFilterMax))

	return int(ret)
}

func TranslateMessage(msg *MSG) bool {
	ret, _, _ := procTranslateMessage.Call(
		uintptr(unsafe.Pointer(msg)))
	return ret != 0
}

func DispatchMessage(msg *MSG) uintptr {
	ret, _, _ := procDispatchMessage.Call(
		uintptr(unsafe.Pointer(msg)))
	return ret
}
