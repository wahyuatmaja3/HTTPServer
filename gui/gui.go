package gui

import (
	"fmt"
	"httpserverdb/server"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// INIConfig holds settings loaded from/saved to INI file
type INIConfig struct {
	Port           string
	IPs            []string
	MaxConnections int
	SessionTimeout int
	MaxThreads     int
	ListenQueue    int
	Top            int
	Left           int
}

// GUI is the main GUI application
type GUI struct {
	app    fyne.App
	window fyne.Window
	config INIConfig
	server *server.Server

	// Widgets
	ipList         *widget.List
	portEntry      *widget.Entry
	logText        *widget.Entry
	maxConnEntry   *widget.Entry
	sessionTOEntry *widget.Entry
	maxThreadEntry *widget.Entry
	listenQEntry   *widget.Entry
	startBtn       *widget.Button
	stopBtn        *widget.Button
	ipAddEntry     *widget.Entry

	mu      sync.Mutex
	running bool
}

// NewGUI creates and runs the GUI
func NewGUI() {
	g := &GUI{}
	g.loadINI()

	g.app = app.New()
	g.window = g.app.NewWindow("HTTPServerDB")
	g.window.Resize(fyne.NewSize(700, 550))

	g.buildUI()

	g.window.SetOnClosed(func() {
		if g.running {
			g.stopServer()
		}
		g.saveINI()
	})

	g.window.ShowAndRun()
}

func (g *GUI) buildUI() {
	// Port
	g.portEntry = widget.NewEntry()
	g.portEntry.SetText(g.config.Port)
	portRow := container.NewHBox(widget.NewLabel("Port:"), g.portEntry)

	// IP list
	ipData := g.config.IPs
	selectedIP := -1
	g.ipList = widget.NewList(
		func() int { return len(ipData) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			obj.(*widget.Label).SetText(ipData[id])
		},
	)
	g.ipList.OnSelected = func(id widget.ListItemID) {
		selectedIP = int(id)
	}
	g.ipList.Resize(fyne.NewSize(200, 120))

	g.ipAddEntry = widget.NewEntry()
	g.ipAddEntry.SetPlaceHolder("IP Address")

	addIPBtn := widget.NewButton("Add IP", func() {
		ip := g.ipAddEntry.Text
		if ip != "" {
			g.config.IPs = append(g.config.IPs, ip)
			ipData = g.config.IPs
			g.ipList.Refresh()
			g.ipAddEntry.SetText("")
		}
	})

	removeIPBtn := widget.NewButton("Remove IP", func() {
		if selectedIP >= 0 && selectedIP < len(g.config.IPs) {
			g.config.IPs = append(g.config.IPs[:selectedIP], g.config.IPs[selectedIP+1:]...)
			ipData = g.config.IPs
			selectedIP = -1
			g.ipList.Refresh()
		}
	})

	ipButtons := container.NewHBox(addIPBtn, removeIPBtn)
	ipSection := container.NewVBox(
		widget.NewLabel("IP Addresses:"),
		container.NewGridWrap(fyne.NewSize(280, 120), g.ipList),
		container.NewHBox(g.ipAddEntry, ipButtons),
	)

	// Start/Stop buttons
	g.startBtn = widget.NewButton("Start Server", func() { g.startServer() })
	g.stopBtn = widget.NewButton("Stop Server", func() { g.stopServer() })
	g.stopBtn.Disable()
	btnRow := container.NewHBox(g.startBtn, g.stopBtn)

	// Settings
	g.maxConnEntry = widget.NewEntry()
	g.maxConnEntry.SetText(strconv.Itoa(g.config.MaxConnections))

	g.sessionTOEntry = widget.NewEntry()
	g.sessionTOEntry.SetText(strconv.Itoa(g.config.SessionTimeout))

	g.maxThreadEntry = widget.NewEntry()
	g.maxThreadEntry.SetText(strconv.Itoa(g.config.MaxThreads))

	g.listenQEntry = widget.NewEntry()
	g.listenQEntry.SetText(strconv.Itoa(g.config.ListenQueue))

	settingsForm := container.NewVBox(
		widget.NewLabel("Settings:"),
		container.NewGridWithColumns(2,
			widget.NewLabel("MaxConnections:"), g.maxConnEntry,
			widget.NewLabel("SessionTimeout:"), g.sessionTOEntry,
			widget.NewLabel("MaxThreads:"), g.maxThreadEntry,
			widget.NewLabel("ListenQueue:"), g.listenQEntry,
		),
	)

	// Log area
	g.logText = widget.NewMultiLineEntry()
	g.logText.Wrapping = fyne.TextWrapWord
	g.logText.SetMinRowsVisible(12)

	logSection := container.NewVBox(
		widget.NewLabel("Log:"),
		container.NewGridWrap(fyne.NewSize(680, 200), g.logText),
	)

	// Layout
	leftPanel := container.NewVBox(
		portRow,
		ipSection,
		btnRow,
	)

	rightPanel := container.NewVBox(
		settingsForm,
	)

	topSection := container.NewHBox(leftPanel, layout.NewSpacer(), rightPanel)
	content := container.NewVBox(topSection, logSection)

	g.window.SetContent(content)
}

func (g *GUI) startServer() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.running {
		return
	}

	port := g.portEntry.Text
	maxConn, _ := strconv.Atoi(g.maxConnEntry.Text)
	sessionTO, _ := strconv.Atoi(g.sessionTOEntry.Text)
	maxThreads, _ := strconv.Atoi(g.maxThreadEntry.Text)
	listenQ, _ := strconv.Atoi(g.listenQEntry.Text)

	if maxConn == 0 {
		maxConn = 100
	}
	if sessionTO == 0 {
		sessionTO = 8000
	}
	if maxThreads == 0 {
		maxThreads = 2000
	}

	config := server.Config{
		Port:           port,
		IPs:            g.config.IPs,
		MaxConnections: maxConn,
		SessionTimeout: sessionTO,
		MaxThreads:     maxThreads,
		ListenQueue:    listenQ,
		TablesDir:      server.GetTablesDir(),
	}

	g.server = server.NewServer(config, func(msg string) {
		g.appendLog(msg)
	})

	if err := g.server.Start(); err != nil {
		g.appendLog(fmt.Sprintf("Error starting server: %v", err))
		return
	}

	g.running = true
	g.startBtn.Disable()
	g.stopBtn.Enable()
	g.appendLog(fmt.Sprintf("Server started on port %s at %s", port, time.Now().Format("2006-01-02 15:04:05")))
}

func (g *GUI) stopServer() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.running {
		return
	}

	if g.server != nil {
		g.server.Stop()
	}

	g.running = false
	g.startBtn.Enable()
	g.stopBtn.Disable()
	g.appendLog("Server stopped")
}

func (g *GUI) appendLog(msg string) {
	timestamp := time.Now().Format("15:04:05")
	line := fmt.Sprintf("[%s] %s\n", timestamp, msg)
	current := g.logText.Text
	g.logText.SetText(current + line)
}

func (g *GUI) loadINI() {
	g.config = INIConfig{
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

	lines := strings.Split(string(data), "\n")
	ipCount := 0
	g.config.IPs = nil

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "Port":
			g.config.Port = val
		case "IPs":
			ipCount, _ = strconv.Atoi(val)
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

	_ = ipCount
}

func (g *GUI) saveINI() {
	var sb strings.Builder
	sb.WriteString("[Settings]\n")
	sb.WriteString(fmt.Sprintf("Port=%s\n", g.portEntry.Text))
	sb.WriteString(fmt.Sprintf("IPs=%d\n", len(g.config.IPs)))
	for i, ip := range g.config.IPs {
		sb.WriteString(fmt.Sprintf("IP%d=%s\n", i+1, ip))
	}
	sb.WriteString(fmt.Sprintf("MaxConnections=%s\n", g.maxConnEntry.Text))
	sb.WriteString(fmt.Sprintf("ListenQueue=%s\n", g.listenQEntry.Text))
	sb.WriteString(fmt.Sprintf("SessionTimeOut=%s\n", g.sessionTOEntry.Text))
	sb.WriteString(fmt.Sprintf("MaxThreads=%s\n", g.maxThreadEntry.Text))
	sb.WriteString("[Placement]\n")
	sb.WriteString(fmt.Sprintf("Top=%d\n", g.config.Top))
	sb.WriteString(fmt.Sprintf("Left=%d\n", g.config.Left))

	os.WriteFile("HTTPServerDB.ini", []byte(sb.String()), 0644)
}
