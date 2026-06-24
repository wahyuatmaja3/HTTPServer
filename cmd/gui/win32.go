//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")

	pRegisterClassExW       = user32.NewProc("RegisterClassExW")
	pCreateWindowExW        = user32.NewProc("CreateWindowExW")
	pDefWindowProcW         = user32.NewProc("DefWindowProcW")
	pDestroyWindow          = user32.NewProc("DestroyWindow")
	pPostQuitMessage        = user32.NewProc("PostQuitMessage")
	pPostMessageW           = user32.NewProc("PostMessageW")
	pGetMessageW            = user32.NewProc("GetMessageW")
	pTranslateMessage       = user32.NewProc("TranslateMessage")
	pDispatchMessageW       = user32.NewProc("DispatchMessageW")
	pIsDialogMessageW       = user32.NewProc("IsDialogMessageW")
	pLoadCursorW            = user32.NewProc("LoadCursorW")
	pLoadIconW              = user32.NewProc("LoadIconW")
	pSendMessageW           = user32.NewProc("SendMessageW")
	pSetWindowTextW         = user32.NewProc("SetWindowTextW")
	pGetWindowTextW         = user32.NewProc("GetWindowTextW")
	pGetWindowTextLengthW   = user32.NewProc("GetWindowTextLengthW")
	pUpdateWindow           = user32.NewProc("UpdateWindow")
	pShowWindow             = user32.NewProc("ShowWindow")
	pEnableWindow           = user32.NewProc("EnableWindow")

	pAdjustWindowRectEx = user32.NewProc("AdjustWindowRectEx")
	pCreateFontW        = gdi32.NewProc("CreateFontW")

	pGetModuleHandleW     = kernel32.NewProc("GetModuleHandleW")
	pLoadLibraryW         = kernel32.NewProc("LoadLibraryW")
	pInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
)

// loadRichEdit loads the RichEdit control DLL so the "RICHEDIT50W" /
// "RichEdit20W" window classes become available. Returns the class name that
// successfully registered.
func loadRichEdit() string {
	if h, _, _ := pLoadLibraryW.Call(uintptr(unsafe.Pointer(mustUTF16("Msftedit.dll")))); h != 0 {
		return "RICHEDIT50W"
	}
	if h, _, _ := pLoadLibraryW.Call(uintptr(unsafe.Pointer(mustUTF16("Riched20.dll")))); h != 0 {
		return "RichEdit20W"
	}
	return "EDIT" // fallback: no color, but still functional
}

// Window messages
const (
	WM_DESTROY     = 0x0002
	WM_CLOSE       = 0x0010
	WM_COMMAND     = 0x0111
	WM_NOTIFY      = 0x004E
	WM_SETFONT     = 0x0030
	WM_GETFONT     = 0x0031
	WM_SETTEXT     = 0x000C
	WM_USER        = 0x0400
	WM_APP         = 0x8000
	WM_CTLCOLORSTATIC = 0x0138
)

// Window styles
const (
	WS_OVERLAPPED   = 0x00000000
	WS_CAPTION      = 0x00C00000
	WS_SYSMENU      = 0x00080000
	WS_MINIMIZEBOX  = 0x00020000
	WS_VISIBLE      = 0x10000000
	WS_CHILD        = 0x40000000
	WS_TABSTOP      = 0x00010000
	WS_GROUP        = 0x00020000
	WS_BORDER       = 0x00800000
	WS_VSCROLL      = 0x00200000
	WS_CLIPCHILDREN = 0x02000000
	WS_CLIPSIBLINGS = 0x04000000

	WS_OVERLAPPEDWINDOW = WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_MINIMIZEBOX

	WS_EX_CLIENTEDGE  = 0x00000200
	WS_EX_CONTROLPARENT = 0x00010000
)

// Static / Button / Edit / ListBox styles
const (
	SS_LEFT      = 0x00000000
	SS_ETCHEDHORZ = 0x00000010

	BS_PUSHBUTTON  = 0x00000000
	BS_DEFPUSHBUTTON = 0x00000001
	BS_AUTOCHECKBOX = 0x00000003

	ES_LEFT       = 0x0000
	ES_MULTILINE  = 0x0004
	ES_AUTOVSCROLL = 0x0040
	ES_AUTOHSCROLL = 0x0080
	ES_READONLY   = 0x0800
	ES_NUMBER     = 0x2000

	LBS_NOTIFY       = 0x0001
	LBS_HASSTRINGS   = 0x0040
	LBS_NOINTEGRALHEIGHT = 0x0100
)

// ShowWindow
const (
	SW_HIDE = 0
	SW_SHOW = 5
	SW_SHOWNORMAL = 1
)

// GWLP
const (
	GWLP_WNDPROC  = -4
	GWLP_USERDATA = -21
)

// Button messages / checkbox state
const (
	BM_GETCHECK = 0x00F0
	BM_SETCHECK = 0x00F1
	BST_UNCHECKED = 0
	BST_CHECKED   = 1
)

// ListBox messages
const (
	LB_ADDSTRING  = 0x0180
	LB_GETCURSEL  = 0x0188
	LB_GETCOUNT   = 0x018B
	LB_RESETCONTENT = 0x0184
)

// Edit messages
const (
	EM_SETSEL          = 0x00B1
	EM_REPLACESEL      = 0x00C2
	EM_LINESCROLL      = 0x00B6
	EM_GETLINECOUNT    = 0x00BA
	EM_SETCHARFORMAT   = 0x0444
	EM_SCROLLCARET     = 0x00B7
	SCF_SELECTION      = 0x0001
	CFM_COLOR          = 0x40000000
	CFE_AUTOCOLOR      = 0x40000000
)

// Tab control
const (
	TCM_FIRST    = 0x1300
	TCM_INSERTITEMW = TCM_FIRST + 62
	TCIF_TEXT    = 0x0001
)

// Stock objects / colors
const (
	WHITE_BRUSH      = 0
	NULL_BRUSH       = 5
	DEFAULT_GUI_FONT = 17
)

// Font weights
const (
	FW_NORMAL = 400
	DEFAULT_CHARSET = 1
	ANTIALIASED_QUALITY = 4
)

// CreateFont params we care about
const (
	OUT_DEFAULT_PRECIS   = 0
	CLIP_DEFAULT_PRECIS  = 0
	VARIABLE_PITCH       = 2
	FF_SWISS             = 0x20
)

// InitCommonControlsEx flags
const (
	ICC_TAB_CLASSES      = 0x00000008
	ICC_STANDARD_CLASSES = 0x00004000
)

// IDC cursor
const IDC_ARROW = 32512

// COLOR_BTNFACE index for background brush (+1 as required by WNDCLASS hbrBackground)
const COLOR_BTNFACE = 15

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type msg struct {
	hwnd    syscall.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

type point struct {
	x, y int32
}

type initCommonControlsExStruct struct {
	dwSize uint32
	dwICC  uint32
}

type tcItemW struct {
	mask        uint32
	dwState     uint32
	dwStateMask uint32
	pszText     *uint16
	cchTextMax  int32
	iImage      int32
	lParam      uintptr
}

func mustUTF16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func uintptrFromUTF16(p *uint16) uintptr {
	return uintptr(unsafe.Pointer(p))
}

func unsafeSizeofWndClass() uintptr {
	return unsafe.Sizeof(wndClassExW{})
}

func loWord(v uintptr) uint16 { return uint16(v & 0xffff) }

func getModuleHandle() syscall.Handle {
	h, _, _ := pGetModuleHandleW.Call(0)
	return syscall.Handle(h)
}

func loadCursor(id uintptr) syscall.Handle {
	h, _, _ := pLoadCursorW.Call(0, id)
	return syscall.Handle(h)
}

func loadIcon(inst syscall.Handle, id uintptr) syscall.Handle {
	h, _, _ := pLoadIconW.Call(uintptr(inst), id)
	return syscall.Handle(h)
}

func registerClassEx(wc *wndClassExW) uint16 {
	atom, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(wc)))
	return uint16(atom)
}

func createWindowEx(exStyle uint32, className, windowName *uint16, style uint32, x, y, w, h int32, parent, menu syscall.Handle, inst syscall.Handle) syscall.Handle {
	hwnd, _, _ := pCreateWindowExW.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		uintptr(parent), uintptr(menu), uintptr(inst), 0,
	)
	return syscall.Handle(hwnd)
}

func defWindowProc(hwnd syscall.Handle, m uint32, wParam, lParam uintptr) uintptr {
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(m), wParam, lParam)
	return r
}

func sendMessage(hwnd syscall.Handle, m uint32, wParam, lParam uintptr) uintptr {
	r, _, _ := pSendMessageW.Call(uintptr(hwnd), uintptr(m), wParam, lParam)
	return r
}

func setWindowText(hwnd syscall.Handle, text string) {
	pSetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(mustUTF16(text))))
}

func getWindowText(hwnd syscall.Handle) string {
	n, _, _ := pGetWindowTextLengthW.Call(uintptr(hwnd))
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	pGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(n+1))
	return syscall.UTF16ToString(buf)
}

func showWindow(hwnd syscall.Handle, cmd int32) {
	pShowWindow.Call(uintptr(hwnd), uintptr(cmd))
}

func updateWindow(hwnd syscall.Handle) {
	pUpdateWindow.Call(uintptr(hwnd))
}

func initCommonControls() {
	icc := initCommonControlsExStruct{
		dwSize: uint32(unsafe.Sizeof(initCommonControlsExStruct{})),
		dwICC:  ICC_TAB_CLASSES | ICC_STANDARD_CLASSES,
	}
	pInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
}

func createTahomaFont() syscall.Handle {
	// Tahoma. Height -12 ≈ 9pt at 96 DPI.
	h, _, _ := pCreateFontW.Call(
		uintptr(uint32(0xFFFFFFF4)), // height = -12 (int32) reinterpreted
		0,                   // width
		0,                   // escapement
		0,                   // orientation
		uintptr(FW_NORMAL),  // weight
		0,                   // italic
		0,                   // underline
		0,                   // strikeout
		uintptr(DEFAULT_CHARSET),
		uintptr(OUT_DEFAULT_PRECIS),
		uintptr(CLIP_DEFAULT_PRECIS),
		uintptr(ANTIALIASED_QUALITY),
		uintptr(VARIABLE_PITCH|FF_SWISS),
		uintptr(unsafe.Pointer(mustUTF16("Tahoma"))),
	)
	return syscall.Handle(h)
}

func getMessage(m *msg) int32 {
	r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(m)), 0, 0, 0)
	return int32(r)
}

func translateMessage(m *msg) {
	pTranslateMessage.Call(uintptr(unsafe.Pointer(m)))
}

func dispatchMessage(m *msg) {
	pDispatchMessageW.Call(uintptr(unsafe.Pointer(m)))
}

func isDialogMessage(hwnd syscall.Handle, m *msg) bool {
	r, _, _ := pIsDialogMessageW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(m)))
	return r != 0
}

func postQuitMessage(code int32) {
	pPostQuitMessage.Call(uintptr(code))
}

func tabInsertItem(hwnd syscall.Handle, index int32, text string) {
	item := tcItemW{
		mask:    TCIF_TEXT,
		pszText: mustUTF16(text),
	}
	sendMessage(hwnd, TCM_INSERTITEMW, uintptr(index), uintptr(unsafe.Pointer(&item)))
}

type rect struct {
	left, top, right, bottom int32
}

func adjustWindowRectEx(style, exStyle uint32, w, h int32) (int32, int32) {
	r := rect{0, 0, w, h}
	pAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&r)), uintptr(style), 0, uintptr(exStyle))
	return r.right - r.left, r.bottom - r.top
}

type charFormatW struct {
	cbSize          uint32
	dwMask          uint32
	dwEffects       uint32
	yHeight         int32
	yOffset         int32
	crTextColor     uint32
	bCharSet        byte
	bPitchAndFamily byte
	szFaceName      [32]uint16
	_               [2]byte
}

func enableWindow(hwnd syscall.Handle, enable bool) bool {
	var val uintptr
	if enable {
		val = 1
	}
	r, _, _ := pEnableWindow.Call(uintptr(hwnd), val)
	return r != 0
}
