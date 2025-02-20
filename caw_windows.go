package ActiveWindow

import (
	"context"
	"fmt"
	"golang.org/x/sys/windows"
	"log/slog"
	"strings"
	"syscall"
	"unsafe"
)

const (
	EVENT_SYSTEM_FOREGROUND = 3
	EVENT_OBJECT_NAMECHANGE = 0x800C
	WINEVENT_OUTOFCONTEXT   = 0
	WINEVENT_INCONTEXT      = 4
	WINEVENT_SKIPOWNPROCESS = 2
	WINEVENT_SKIPOWNTHREAD  = 1
	OBJID_WINDOW            = 0
)

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

type WINEVENTPROC func(hWinEventHook HWINEVENTHOOK, event uint32, hwnd HWND, idObject int32, idChild int32, idEventThread uint32, dwmsEventTime uint32) uintptr

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

var (
	aHWND       HWND
	messagesCAW = make(chan MessageCAW, 10)
	user32      = windows.NewLazyDLL("user32.dll")
	//kernel32    = windows.NewLazyDLL("kernel32.dll")

	procSetWinEventHook     = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent      = user32.NewProc("UnhookWinEvent")
	procGetMessage          = user32.NewProc("GetMessageW")
	procGetWindowText       = user32.NewProc("GetWindowTextW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessage     = user32.NewProc("DispatchMessageW")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procGetWindowTextLength = user32.NewProc("GetWindowTextLengthW")

	ActiveWinEventHook WINEVENTPROC = func(hWinEventHook HWINEVENTHOOK, event uint32, hwnd HWND, idObject int32, idChild int32, idEventThread uint32, dwmsEventTime uint32) uintptr {
		if event == EVENT_SYSTEM_FOREGROUND {
			// Смена активного окна
			aHWND = hwnd
			relayMessage(hwnd)
		} else if event == EVENT_OBJECT_NAMECHANGE && idObject == OBJID_WINDOW && aHWND == hwnd {
			// Смена заголовка окна
			relayMessage(hwnd)
		}
		//slog.Debug("found", slog.Any("hwnd", hwnd))
		return 0
	}
)

func Subscribe(ctx context.Context, msgCAW chan MessageCAW) {

	winEvHook := SetWinEventHook(EVENT_SYSTEM_FOREGROUND,
		EVENT_OBJECT_NAMECHANGE,
		0,
		ActiveWinEventHook,
		0,
		0,
		WINEVENT_OUTOFCONTEXT|WINEVENT_SKIPOWNTHREAD)
	slog.Debug("Windows Event Hook: ", slog.Any("handler", winEvHook))

	go func() {
		for {
			select {
			case <-ctx.Done():
				UnhookWinEvent(winEvHook)
				slog.Debug("Event Hook Active Window Exit")
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
		Hwnd: h,
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

func GetNameApp(hwnd HWND) (string, error) {
	// Получаем PID по HWND
	var pid uint32
	_, err := windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid)
	if err != nil {
		fmt.Println("GetWindowThreadProcessId failed:", err)
		return "", err
	}

	// Открываем процесс по PID
	processHandle, err := windows.OpenProcess(
		windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ,
		false,
		pid,
	)
	if err != nil {
		fmt.Println("OpenProcess failed:", err)
		return "", err
	}
	defer func() { _ = windows.CloseHandle(processHandle) }()

	// Получаем имя исполняемого файла процесса
	var path [windows.MAX_PATH]uint16
	size := uint32(len(path))

	// Используем QueryFullProcessImageNameW
	err = windows.QueryFullProcessImageName(processHandle, 0, &path[0], &size)
	if err != nil {
		fmt.Println("QueryFullProcessImageName failed:", err)
		return "", err
	}

	// Преобразуем путь в строку
	name := windows.UTF16ToString(path[:size])
	idx := strings.LastIndex(name, "\\")
	name = name[idx+1:]

	return name, nil
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
