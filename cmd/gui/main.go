//go:build windows

// Pixel-perfect Win32 GUI for HTTP Server DB, built per gui.md.
// Uses only the Windows syscall API (no CGO), so it compiles without a
// C compiler. Wraps the existing httpserverdb/server package.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"httpserverdb/server"
)

// Control IDs (used as the child-window "menu" handle and decoded from WM_COMMAND).
const (
	idStartBtn  = 1001
	idDetailLog = 1002
	idIPList    = 1003
	idPortEdit  = 1004
	idMaxConn   = 1005
	idListenQ   = 1006
	idSessionTO = 1007
	idMaxThread = 1008
)

// wmAppendLog is posted from server goroutines to drain queued log lines on
// the UI thread.
const wmAppendLog = WM_APP + 1

// wmServerStarted is posted from the background start goroutine once the server
// is listening (or has failed). wParam==1 means success, 0 means failure.
const wmServerStarted = WM_APP + 2

type iniConfig struct {
	Port           string
	IPs            []string
	MaxConnections int
	SessionTimeout int
	MaxThreads     int
	ListenQueue    int
	Top            int
	Left           int
}

type logLine struct {
	text  string
	color server.LogColor
}

type appGUI struct {
	hwnd syscall.Handle
	font syscall.Handle
	inst syscall.Handle

	// controls
	hTab       syscall.Handle
	hIPList    syscall.Handle
	hPort      syscall.Handle
	hDetailLog syscall.Handle
	hMaxConn   syscall.Handle
	hListenQ   syscall.Handle
	hSessionTO syscall.Handle
	hMaxThread syscall.Handle
	hTotalReq  syscall.Handle
	hOnProses  syscall.Handle
	hMaxProses syscall.Handle
	hMaxTime   syscall.Handle
	hLog       syscall.Handle
	hStartBtn  syscall.Handle
	hEtched    syscall.Handle
	controls   []syscall.Handle // every child control, for font application

	config   iniConfig
	srv      *server.Server
	mu       sync.Mutex
	running  bool
	starting bool // true while background start goroutine is in progress
	logClass string

	// pending log lines delivered cross-thread
	logMu   sync.Mutex
	logPend []logLine

	// daily log file writing
	logFile     *os.File
	logFileMu   sync.Mutex
	logFileDate string
}

var app = &appGUI{}

func main() {
	app.loadINI()
	app.inst = getModuleHandle()
	initCommonControls()
	app.font = createTahomaFont()
	app.logClass = loadRichEdit()

	className := mustUTF16("HTTPServerDBWin")
	hIcon := loadIcon(app.inst, 1)
	if hIcon == 0 {
		hIcon = loadIcon(0, 32512) // IDI_APPLICATION fallback
	}
	wc := wndClassExW{
		cbSize:        uint32(unsafeSizeofWndClass()),
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     app.inst,
		hCursor:       loadCursor(IDC_ARROW),
		hIcon:         hIcon,
		hIconSm:       hIcon,
		hbrBackground: syscall.Handle(COLOR_BTNFACE + 1),
		lpszClassName: className,
	}
	registerClassEx(&wc)

	// Window: client area 488x360 (75% of 650x480). Compute window size to
	// account for non-client area (title bar, borders).
	style := uint32(WS_OVERLAPPEDWINDOW | WS_CLIPCHILDREN)
	winW, winH := adjustWindowRectEx(style, WS_EX_CONTROLPARENT, 488, 360)
	x := int32(app.config.Left)
	y := int32(app.config.Top)
	if x == 0 && y == 0 {
		x, y = 200, 120
	}
	app.hwnd = createWindowEx(
		WS_EX_CONTROLPARENT,
		className,
		mustUTF16("HTTP Server DB"),
		style,
		x, y, winW, winH,
		0, 0, app.inst,
	)

	app.buildControls()
	app.applyFont()
	app.seedLog()

	showWindow(app.hwnd, SW_SHOWNORMAL)
	updateWindow(app.hwnd)

	// Set a 1-second timer to automatically start the server
	setTimer(app.hwnd, 1, 1000, 0)

	runMessageLoop()
}

// child creates a child control, records it for font application, and returns
// its handle.
func (g *appGUI) child(class string, text string, style uint32, exStyle uint32, x, y, w, h int32, id uintptr) syscall.Handle {
	h2 := createWindowEx(
		exStyle,
		mustUTF16(class),
		mustUTF16(text),
		WS_CHILD|WS_VISIBLE|style,
		x, y, w, h,
		g.hwnd, syscall.Handle(id), g.inst,
	)
	g.controls = append(g.controls, h2)
	return h2
}

// buildControls lays out every widget using the exact coordinates from gui.md.
// The tab control sits at (10,10) size 630x170; its client area begins ~y=30,
// so child controls inside the tab are offset by (tabX+border, tabY+clientTop).
func (g *appGUI) buildControls() {
	// §3 Tab control container.
	g.hTab = g.child("SysTabControl32", "", WS_CLIPSIBLINGS, 0, 7, 7, 472, 127, 0)
	tabInsertItem(g.hTab, 0, "Settings")

	// Children are placed relative to the form; coordinates are scaled by 0.75
	ox, oy := int32(7), int32(7+15)

	// §3.1 Bind to IPs (left sector)
	g.child("STATIC", "Bind to IPs", SS_LEFT, 0, ox+7, oy+11, 75, 11, 0)
	g.hIPList = g.child("SysListView32", "",
		WS_BORDER|LVS_REPORT|LVS_NOCOLUMNHEADER|LVS_SINGLESEL|WS_TABSTOP,
		WS_EX_CLIENTEDGE, ox+7, oy+24, 98, 75, idIPList)

	// Set extended styles: checkboxes and full row select (wParam must match lParam)
	sendMessage(g.hIPList, LVM_SETEXTENDEDLISTVIEWSTYLE,
		LVS_EX_CHECKBOXES|LVS_EX_FULLROWSELECT,
		LVS_EX_CHECKBOXES|LVS_EX_FULLROWSELECT)

	// Add a single column
	col := lvColumnW{
		mask: LVCF_WIDTH,
		cx:   94,
	}
	sendMessage(g.hIPList, LVM_INSERTCOLUMNW, 0, uintptr(unsafe.Pointer(&col)))

	// Populate ListView
	for i, ip := range g.config.IPs {
		stateVal := uint32(0x1000) // Unchecked (1 << 12)
		if i < 3 {
			stateVal = 0x2000 // Checked (2 << 12)
		}
		item := lvItemW{
			mask:      LVIF_TEXT | LVIF_STATE,
			iItem:     int32(i),
			iSubItem:  0,
			pszText:   mustUTF16(ip),
			stateMask: LVIS_STATEIMAGEMASK,
			state:     stateVal,
		}
		sendMessage(g.hIPList, LVM_INSERTITEMW, 0, uintptr(unsafe.Pointer(&item)))
	}

	g.child("STATIC", "Bind to port", SS_LEFT, 0, ox+112, oy+11, 60, 11, 0)
	g.hPort = g.child("EDIT", g.config.Port, ES_LEFT|ES_AUTOHSCROLL|WS_TABSTOP,
		WS_EX_CLIENTEDGE, ox+112, oy+24, 56, 17, idPortEdit)

	g.hDetailLog = g.child("BUTTON", "Detail Log", BS_AUTOCHECKBOX|WS_TABSTOP, 0,
		ox+112, oy+82, 68, 15, idDetailLog)
	sendMessage(g.hDetailLog, BM_SETCHECK, BST_CHECKED, 0)

	// §3.2 Performance params (center sector): labels X=188, edits X=270, pitch 21.
	g.child("STATIC", "Max Connections", SS_LEFT, 0, ox+188, oy+13, 75, 11, 0)
	g.hMaxConn = g.child("EDIT", strconv.Itoa(g.config.MaxConnections), ES_LEFT|ES_NUMBER|WS_TABSTOP, WS_EX_CLIENTEDGE, ox+270, oy+11, 60, 17, idMaxConn)

	g.child("STATIC", "Listen Queue", SS_LEFT, 0, ox+188, oy+34, 75, 11, 0)
	g.hListenQ = g.child("EDIT", strconv.Itoa(g.config.ListenQueue), ES_LEFT|ES_NUMBER|WS_TABSTOP, WS_EX_CLIENTEDGE, ox+270, oy+32, 60, 17, idListenQ)

	g.child("STATIC", "Session TimeOut", SS_LEFT, 0, ox+188, oy+55, 75, 11, 0)
	g.hSessionTO = g.child("EDIT", strconv.Itoa(g.config.SessionTimeout), ES_LEFT|ES_NUMBER|WS_TABSTOP, WS_EX_CLIENTEDGE, ox+270, oy+53, 60, 17, idSessionTO)

	g.child("STATIC", "MaxThreads", SS_LEFT, 0, ox+188, oy+76, 75, 11, 0)
	g.hMaxThread = g.child("EDIT", strconv.Itoa(g.config.MaxThreads), ES_LEFT|ES_NUMBER|WS_TABSTOP, WS_EX_CLIENTEDGE, ox+270, oy+74, 60, 17, idMaxThread)

	// §3.3 Monitoring metrics (right sector): labels X=345, edits X=412, read-only.
	roStyle := uint32(ES_LEFT | ES_READONLY)
	g.child("STATIC", "Total Req", SS_LEFT, 0, ox+345, oy+13, 60, 11, 0)
	g.hTotalReq = g.child("EDIT", "", roStyle, WS_EX_CLIENTEDGE, ox+412, oy+11, 52, 17, 0)

	g.child("STATIC", "On Proses", SS_LEFT, 0, ox+345, oy+34, 60, 11, 0)
	g.hOnProses = g.child("EDIT", "", roStyle, WS_EX_CLIENTEDGE, ox+412, oy+32, 52, 17, 0)

	g.child("STATIC", "Max On Proses", SS_LEFT, 0, ox+345, oy+55, 60, 11, 0)
	g.hMaxProses = g.child("EDIT", "", roStyle, WS_EX_CLIENTEDGE, ox+412, oy+53, 52, 17, 0)

	g.child("STATIC", "Max Time", SS_LEFT, 0, ox+345, oy+76, 60, 11, 0)
	g.hMaxTime = g.child("EDIT", "", roStyle, WS_EX_CLIENTEDGE, ox+412, oy+74, 52, 17, 0)

	// §4 Etched separator at outer Y=142, and the log window.
	g.hEtched = g.child("STATIC", "", SS_ETCHEDHORZ, 0, 7, 142, 472, 2, 0)
	g.hLog = g.child(g.logClass, "",
		ES_MULTILINE|ES_READONLY|ES_AUTOVSCROLL|WS_VSCROLL|WS_BORDER,
		WS_EX_CLIENTEDGE, 7, 146, 472, 172, 0)

	// §5 Action panel: Start Server button.
	g.hStartBtn = g.child("BUTTON", "Start Server", BS_DEFPUSHBUTTON|WS_TABSTOP, 0,
		398, 332, 82, 21, idStartBtn)

	// Populate IP list.
	for _, ip := range g.config.IPs {
		sendMessage(g.hIPList, LB_ADDSTRING, 0, strPtr(ip))
	}
}

func strPtr(s string) uintptr {
	return uintptrFromUTF16(mustUTF16(s))
}

// applyFont sets the Tahoma 9pt font on every child control (labels included).
func (g *appGUI) applyFont() {
	for _, h := range g.controls {
		if h != 0 {
			sendMessage(h, WM_SETFONT, uintptr(g.font), 1)
		}
	}
}

// seedLog writes the initial three log lines from gui.md §4.
func (g *appGUI) seedLog() {
	now := time.Now().Format("01/02/2006 15:04:05")
	g.appendLog(now + " FormCreate")
	g.appendLog(now + " DB Open")
	g.appendLog(now + " Version: 9 Maret 2024")
}

// appendLogColor appends a colored line to the Rich Edit log box (safe to call from the UI thread).
func (g *appGUI) appendLogColor(line string, color server.LogColor) {
	var textToAppend string
	if getWindowTextLength(g.hLog) > 0 {
		textToAppend = "\r\n" + line
	} else {
		textToAppend = line
	}

	length := getWindowTextLength(g.hLog)
	sendMessage(g.hLog, EM_SETSEL, uintptr(length), uintptr(length))

	var rgb uint32
	switch color {
	case server.ColorBlack:
		rgb = 0x00000000
	case server.ColorBlue:
		rgb = 0x00FF0000 // Blue (BGR: 0x00FF0000)
	case server.ColorGreen:
		rgb = 0x00009900 // Green (BGR: 0x00009900)
	case server.ColorMagenta:
		rgb = 0x00990099 // Magenta/Purple (BGR: 0x00990099)
	default:
		rgb = 0x00000000
	}

	cf := charFormatW{
		dwMask:      CFM_COLOR,
		dwEffects:   0,
		crTextColor: rgb,
	}
	cf.cbSize = uint32(unsafe.Sizeof(cf))

	sendMessage(g.hLog, EM_SETCHARFORMAT, SCF_SELECTION, uintptr(unsafe.Pointer(&cf)))
	sendMessage(g.hLog, EM_REPLACESEL, 0, strPtr(textToAppend))
	sendMessage(g.hLog, EM_SCROLLCARET, 0, 0)

	g.writeToLogFile(line)
}

// appendLog appends a line to the log box in black (safe to call from the UI thread).
func (g *appGUI) appendLog(line string) {
	g.appendLogColor(line, server.ColorBlack)
}

func (g *appGUI) writeToLogFile(text string) {
	today := time.Now().Format("060102") // YYMMDD
	g.logFileMu.Lock()
	defer g.logFileMu.Unlock()
	if g.logFile == nil || g.logFileDate != today {
		if g.logFile != nil {
			g.logFile.Close()
		}
		fname := "HTTPServerDB" + today + ".log"
		f, err := os.OpenFile(fname, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			g.logFile = f
			g.logFileDate = today
		} else {
			g.logFile = nil
			g.logFileDate = ""
		}
	}
	if g.logFile != nil {
		g.logFile.WriteString(text + "\r\n")
	}
}

func (g *appGUI) closeLogFile() {
	g.logFileMu.Lock()
	defer g.logFileMu.Unlock()
	if g.logFile != nil {
		g.logFile.Close()
		g.logFile = nil
		g.logFileDate = ""
	}
}

// logFromServer is the callback passed to the server; it queues the line and
// posts a message so the UI thread does the actual control update.
func (g *appGUI) logFromServer(line string) {
	stamped := time.Now().Format("01/02/2006 15:04:05") + " " + line
	g.logMu.Lock()
	g.logPend = append(g.logPend, logLine{text: stamped, color: server.ColorBlack})
	g.logMu.Unlock()
	pPostMessageW.Call(uintptr(g.hwnd), wmAppendLog, 0, 0)
}

// logColorFromServer is the callback passed to the server; it queues the line and
// posts a message so the UI thread does the actual control update.
func (g *appGUI) logColorFromServer(line string, color server.LogColor) {
	stamped := time.Now().Format("01/02/2006 15:04:05") + " " + line
	g.logMu.Lock()
	g.logPend = append(g.logPend, logLine{text: stamped, color: color})
	g.logMu.Unlock()
	pPostMessageW.Call(uintptr(g.hwnd), wmAppendLog, 0, 0)
}

// getWindowTextLength is a helper that wraps pGetWindowTextLengthW
func getWindowTextLength(hwnd syscall.Handle) int {
	r, _, _ := pGetWindowTextLengthW.Call(uintptr(hwnd))
	return int(r)
}

func (g *appGUI) drainPendingLogs() {
	g.logMu.Lock()
	pending := g.logPend
	g.logPend = nil
	g.logMu.Unlock()
	for _, l := range pending {
		g.appendLogColor(l.text, l.color)
	}
	g.updateMetrics()
}

func (g *appGUI) updateMetrics() {
	if g.srv == nil {
		return
	}
	m := g.srv.Metrics()
	setWindowText(g.hTotalReq, strconv.FormatInt(m.TotalReq, 10))
	setWindowText(g.hOnProses, strconv.FormatInt(m.OnProses, 10))
	setWindowText(g.hMaxProses, strconv.FormatInt(m.MaxOnProses, 10))
	setWindowText(g.hMaxTime, server.FormatDuration(m.MaxTime))
}

func (g *appGUI) editInt(h syscall.Handle, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(getWindowText(h)))
	if err != nil {
		return def
	}
	return v
}

func (g *appGUI) startServer() {
	g.mu.Lock()
	if g.running || g.starting {
		g.mu.Unlock()
		return
	}

	// Collect all UI values here on the UI thread (SendMessage calls must be
	// on the thread that created the controls).
	port := strings.TrimSpace(getWindowText(g.hPort))
	if port == "" {
		port = "8024"
	}

	var checkedIPs []string
	count := sendMessage(g.hIPList, LVM_GETITEMCOUNT, 0, 0)
	for i := 0; i < int(count); i++ {
		state := sendMessage(g.hIPList, LVM_GETITEMSTATE, uintptr(i), LVIS_STATEIMAGEMASK)
		isChecked := (state & 0x2000) != 0
		if isChecked && i < len(g.config.IPs) {
			checkedIPs = append(checkedIPs, g.config.IPs[i])
		}
	}
	if len(checkedIPs) == 0 {
		checkedIPs = g.config.IPs
	}

	cfg := server.Config{
		Port:           port,
		IPs:            checkedIPs,
		MaxConnections: g.editInt(g.hMaxConn, 100),
		SessionTimeout: g.editInt(g.hSessionTO, 8000),
		MaxThreads:     g.editInt(g.hMaxThread, 2000),
		ListenQueue:    g.editInt(g.hListenQ, 0),
		TablesDir:      server.GetTablesDir(),
		DetailLog:      sendMessage(g.hDetailLog, BM_GETCHECK, 0, 0) == BST_CHECKED,
	}

	// Disable start button while starting to give user feedback
	g.starting = true
	setWindowText(g.hStartBtn, "Starting...")
	enableWindow(g.hStartBtn, false)
	g.mu.Unlock()

	// Run blocking start (reads API.DB from disk) in a goroutine so the UI
	// message loop stays responsive.
	srv := server.NewServer(cfg, g.logFromServer)
	srv.SetColorLog(g.logColorFromServer)
	hwnd := g.hwnd
	go func() {
		err := srv.Start()
		g.mu.Lock()
		if err == nil {
			g.srv = srv
			g.running = true
		} else {
			g.logMu.Lock()
			g.logPend = append(g.logPend, logLine{
				text:  time.Now().Format("01/02/2006 15:04:05") + " Error starting server: " + err.Error(),
				color: server.ColorBlack,
			})
			g.logMu.Unlock()
		}
		g.starting = false
		g.mu.Unlock()

		// Notify UI thread to finish setup (or revert button) safely.
		successVal := uintptr(0)
		if err == nil {
			successVal = 1
		}
		pPostMessageW.Call(uintptr(hwnd), wmServerStarted, successVal, 0)
	}()
}

func (g *appGUI) stopServer() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.running {
		return
	}
	if g.srv != nil {
		g.srv.Stop()
	}
	g.running = false
	setWindowText(g.hStartBtn, "Start Server")
	g.appendLog(time.Now().Format("01/02/2006 15:04:05") + " Server stopped")

	enableWindow(g.hIPList, true)
	enableWindow(g.hPort, true)
}

func (g *appGUI) toggleServer() {
	g.mu.Lock()
	running := g.running
	starting := g.starting
	g.mu.Unlock()
	if starting {
		return // ignore clicks while background start is in progress
	}
	if running {
		g.stopServer()
	} else {
		g.startServer()
	}
}

// --- INI load/save (matches gui/gui.go format) ---

func (g *appGUI) loadINI() {
	g.config = iniConfig{
		Port:           "8024",
		IPs:            []string{"127.0.0.1"},
		MaxConnections: 100,
		SessionTimeout: 8000,
		MaxThreads:     2000,
		ListenQueue:    0,
	}
	data, err := os.ReadFile("HTTPServerDB.ini")
	if err != nil {
		return
	}
	g.config.IPs = nil
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		switch key {
		case "Port":
			g.config.Port = val
		case "IPs":
			// Count line; the actual IPs are the IP1..IPn entries below. We
			// must consume it explicitly, otherwise it falls into the default
			// branch and "IPs" gets mistaken for an IP entry.
		case "MaxConnections":
			g.config.MaxConnections, _ = strconv.Atoi(val)
		case "SessionTimeOut":
			g.config.SessionTimeout, _ = strconv.Atoi(val)
		case "MaxThreads":
			g.config.MaxThreads, _ = strconv.Atoi(val)
		case "ListenQueue":
			g.config.ListenQueue, _ = strconv.Atoi(val)
		case "Top":
			g.config.Top, _ = strconv.Atoi(val)
		case "Left":
			g.config.Left, _ = strconv.Atoi(val)
		default:
			if strings.HasPrefix(key, "IP") && len(key) > 2 {
				g.config.IPs = append(g.config.IPs, val)
			}
		}
	}
	if len(g.config.IPs) == 0 {
		g.config.IPs = []string{"127.0.0.1"}
	}
}

func (g *appGUI) saveINI() {
	var sb strings.Builder
	sb.WriteString("[Settings]\n")
	sb.WriteString(fmt.Sprintf("Port=%s\n", strings.TrimSpace(getWindowText(g.hPort))))
	sb.WriteString(fmt.Sprintf("IPs=%d\n", len(g.config.IPs)))
	for i, ip := range g.config.IPs {
		sb.WriteString(fmt.Sprintf("IP%d=%s\n", i+1, ip))
	}
	sb.WriteString(fmt.Sprintf("MaxConnections=%s\n", strings.TrimSpace(getWindowText(g.hMaxConn))))
	sb.WriteString(fmt.Sprintf("ListenQueue=%s\n", strings.TrimSpace(getWindowText(g.hListenQ))))
	sb.WriteString(fmt.Sprintf("SessionTimeOut=%s\n", strings.TrimSpace(getWindowText(g.hSessionTO))))
	sb.WriteString(fmt.Sprintf("MaxThreads=%s\n", strings.TrimSpace(getWindowText(g.hMaxThread))))
	sb.WriteString("[Placement]\n")
	sb.WriteString(fmt.Sprintf("Top=%d\n", g.config.Top))
	sb.WriteString(fmt.Sprintf("Left=%d\n", g.config.Left))
	os.WriteFile("HTTPServerDB.ini", []byte(sb.String()), 0644)
}

// --- window procedure ---

func wndProc(hwnd syscall.Handle, m uint32, wParam, lParam uintptr) uintptr {
	switch m {
	case WM_TIMER:
		if wParam == 1 {
			killTimer(hwnd, 1)
			app.startServer()
			return 0
		}
	case wmServerStarted:
		// Called back from the background start goroutine via PostMessage.
		if wParam == 1 {
			// Success: lock down IP/port controls, show Stop button.
			setWindowText(app.hStartBtn, "Stop Server")
			enableWindow(app.hStartBtn, true)
			enableWindow(app.hIPList, false)
			enableWindow(app.hPort, false)
			app.updateMetrics()
		} else {
			// Failure: restore Start button so the user can retry.
			setWindowText(app.hStartBtn, "Start Server")
			enableWindow(app.hStartBtn, true)
		}
		app.drainPendingLogs()
		return 0
	case wmAppendLog:
		app.drainPendingLogs()
		return 0
	case WM_SIZE:
		w := int32(loWord(lParam))
		h := int32(hiWord(lParam))
		if w > 0 && h > 0 {
			// Settings tab stays fixed at top: do NOT resize hTab.
			// Only stretch the separator, log, and button.
			moveWindow(app.hEtched, 7, 142, w-14, 2, true)
			moveWindow(app.hLog, 7, 146, w-14, h-184, true)
			// Keep button anchored to bottom-right
			moveWindow(app.hStartBtn, w-89, h-28, 82, 21, true)
		}
		return 0
	case WM_COMMAND:
		id := loWord(wParam)
		switch id {
		case idStartBtn:
			app.toggleServer()
			return 0
		case idDetailLog:
			app.mu.Lock()
			srv := app.srv
			app.mu.Unlock()
			if srv != nil {
				checked := sendMessage(app.hDetailLog, BM_GETCHECK, 0, 0) == BST_CHECKED
				srv.SetDetailLog(checked)
			}
			return 0
		}
	case WM_CLOSE:
		if app.running {
			app.stopServer()
		}
		app.saveINI()
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		app.closeLogFile()
		postQuitMessage(0)
		return 0
	}
	return defWindowProc(hwnd, m, wParam, lParam)
}

func runMessageLoop() {
	var m msg
	for {
		r := getMessage(&m)
		if r == 0 || r == -1 {
			break
		}
		if isDialogMessage(app.hwnd, &m) {
			continue
		}
		translateMessage(&m)
		dispatchMessage(&m)
	}
}
