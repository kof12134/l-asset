// L-Asset System Tray - Windows 系统托盘管理程序
// Copyright (c) 2026 乐为爸爸. All rights reserved.

package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"syscall"

	"github.com/getlantern/systray"
)

// ─── embedded icons ───

//go:embed icon_online.png
var iconOnlineData []byte

//go:embed icon_offline.png
var iconOfflineData []byte

// ─── config ───

type Config struct {
	// Executable path of l-asset-server.exe
	ServerExe string `json:"server_exe"`
	// Port for the web UI (default 5678)
	Port int `json:"port"`
}

func defaultConfig() Config {
	return Config{
		ServerExe: "l-asset-server.exe",
		Port:      5678,
	}
}

// ─── server process management ───

var serverCmd atomic.Value // *exec.Cmd

func serverRunning() bool {
	cmd, ok := serverCmd.Load().(*exec.Cmd)
	return ok && cmd != nil && cmd.Process != nil && cmd.ProcessState == nil
}

func serverPid() int {
	cmd, ok := serverCmd.Load().(*exec.Cmd)
	if !ok || cmd == nil || cmd.Process == nil {
		return -1
	}
	return cmd.Process.Pid
}

func startServer(cfg Config, appDir string) error {
	if serverRunning() {
		return fmt.Errorf("服务已在运行 (PID: %d)", serverPid())
	}

	serverPath := cfg.ServerExe
	if !filepath.IsAbs(serverPath) {
		serverPath = filepath.Join(appDir, serverPath)
	}

	cmd := exec.Command(serverPath)
	cmd.Dir = appDir
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("LASSET_PORT=%d", cfg.Port),
	)

	// Capture stdout/stderr to logs
	logDir := filepath.Join(appDir, "logs")
	os.MkdirAll(logDir, 0755)

	f, err := os.OpenFile(filepath.Join(logDir, "l-asset-tray.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		cmd.Stdout = f
		cmd.Stderr = f
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动服务失败: %v", err)
	}

	serverCmd.Store(cmd)
	log.Printf("服务启动成功, PID: %d", cmd.Process.Pid)
	return nil
}

func stopServer() error {
	cmd, ok := serverCmd.Load().(*exec.Cmd)
	if !ok || cmd == nil || cmd.Process == nil {
		return fmt.Errorf("服务未运行")
	}

	// Try graceful shutdown first
	cmd.Process.Signal(os.Signal(syscall.SIGINT))

	// Force kill after a short wait
	go func() {
		ps, _ := cmd.Process.Wait()
		_ = ps
	}()

	serverCmd.Store((*exec.Cmd)(nil))
	log.Println("服务已停止")
	return nil
}

// ─── systray lifecycle ───

var (
	cfg          Config
	appDir       string
	menuStatus   *systray.MenuItem
	menuOpen     *systray.MenuItem
	menuStart    *systray.MenuItem
	menuStop     *systray.MenuItem
	menuRestart  *systray.MenuItem
	menuSettings *systray.MenuItem
	menuDataDir  *systray.MenuItem
	menuQuit     *systray.MenuItem
)

func onReady() {
	systray.SetIcon(iconOfflineData)
	systray.SetTitle("L-Asset")
	systray.SetTooltip("L-Asset 资产管理系统")

	menuStatus = systray.AddMenuItem("⚪ 已停止", "服务状态")
	menuStatus.Disable()

	systray.AddSeparator()

	menuOpen = systray.AddMenuItem("🌐 打开网页", "浏览器打开资产管理系统")
	menuStart = systray.AddMenuItem("▶ 启动服务", "启动 L-Asset 服务器")
	menuStop = systray.AddMenuItem("⏹ 停止服务", "停止 L-Asset 服务器")
	menuRestart = systray.AddMenuItem("🔄 重启服务", "重启 L-Asset 服务器")

	systray.AddSeparator()

	menuSettings = systray.AddMenuItem("⚙ 端口设置", "修改 Web 端口")
	menuDataDir = systray.AddMenuItem("📂 打开 data 目录", "打开数据文件夹")

	systray.AddSeparator()

	menuQuit = systray.AddMenuItem("❌ 退出", "退出托盘程序")

	// Check if server is already running on startup
	if serverRunning() {
		updateStatus(true)
	}

	// Start menu event loop
	go handleMenuEvents()
}

func onExit() {
	// Clean stop on tray exit
	if serverRunning() {
		stopServer()
	}
}

func updateStatus(running bool) {
	if running {
		systray.SetIcon(iconOnlineData)
		menuStatus.SetTitle(fmt.Sprintf("🟢 运行中 (PID: %d)", serverPid()))
		menuStart.Hide()
		menuStop.Show()
		menuRestart.Show()
	} else {
		systray.SetIcon(iconOfflineData)
		menuStatus.SetTitle("⚪ 已停止")
		menuStart.Show()
		menuStop.Hide()
		menuRestart.Hide()
	}
}

func handleMenuEvents() {
	for {
		select {
		case <-menuOpen.ClickedCh:
			url := fmt.Sprintf("http://localhost:%d", cfg.Port)
			openBrowser(url)

		case <-menuStart.ClickedCh:
			menuStart.SetTitle("⏳ 启动中...")
			menuStart.Disable()
			go func() {
				err := startServer(cfg, appDir)
				menuStart.Enable()
				menuStart.SetTitle("▶ 启动服务")
				if err != nil {
					menuStatus.SetTitle("❌ " + err.Error())
					log.Printf("启动失败: %v", err)
				} else {
					updateStatus(true)
				}
			}()

		case <-menuStop.ClickedCh:
			menuStop.SetTitle("⏳ 停止中...")
			menuStop.Disable()
			go func() {
				err := stopServer()
				menuStop.Enable()
				menuStop.SetTitle("⏹ 停止服务")
				if err != nil {
					log.Printf("停止失败: %v", err)
				}
				updateStatus(false)
			}()

		case <-menuRestart.ClickedCh:
			menuRestart.SetTitle("⏳ 重启中...")
			menuRestart.Disable()
			go func() {
				if serverRunning() {
					stopServer()
				}
				// Small delay for port release
				// time.Sleep(500 * time.Millisecond)  // uncomment if needed
				err := startServer(cfg, appDir)
				menuRestart.Enable()
				menuRestart.SetTitle("🔄 重启服务")
				if err != nil {
					menuStatus.SetTitle("❌ " + err.Error())
					log.Printf("重启失败: %v", err)
				} else {
					updateStatus(true)
				}
			}()

		case <-menuSettings.ClickedCh:
			go showPortDialog()

		case <-menuDataDir.ClickedCh:
			dataPath := filepath.Join(appDir, "data")
			openExplorer(dataPath)

		case <-menuQuit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

// ─── platform helpers ───

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

func openExplorer(path string) {
	switch runtime.GOOS {
	case "windows":
		exec.Command("explorer", path).Start()
	case "darwin":
		exec.Command("open", path).Start()
	default:
		exec.Command("xdg-open", path).Start()
	}
}

func showPortDialog() {
	// On Windows we can't easily show input dialogs from Go.
	// Best approach: open the config file in notepad, or use a simple approach.
	// For v1, we'll write a note and let users edit config manually.
	configPath := filepath.Join(appDir, "tray-config.json")
	exec.Command("notepad", configPath).Start()
}

// ─── config helpers ───

func loadConfig() Config {
	cfg := defaultConfig()
	configPath := filepath.Join(appDir, "tray-config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		saveConfig(cfg) // save defaults
		return cfg
	}
	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		return cfg
	}
	if loaded.ServerExe != "" {
		cfg.ServerExe = loaded.ServerExe
	}
	if loaded.Port > 0 && loaded.Port <= 65535 {
		cfg.Port = loaded.Port
	}
	return cfg
}

func saveConfig(cfg Config) {
	configPath := filepath.Join(appDir, "tray-config.json")
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(configPath, data, 0644)
}

// ─── entry point ───

func main() {
	// Determine app directory (where the exe lives)
	exe, err := os.Executable()
	if err == nil {
		appDir = filepath.Dir(exe)
	} else {
		appDir, _ = os.Getwd()
	}

	// Set up logging
	logDir := filepath.Join(appDir, "logs")
	os.MkdirAll(logDir, 0755)
	logFile, err := os.OpenFile(filepath.Join(logDir, "tray.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
	}

	cfg = loadConfig()
	log.Printf("L-Asset Tray 启动, 端口: %d, 服务路径: %s", cfg.Port, cfg.ServerExe)

	// On Windows, hide console window
	if runtime.GOOS == "windows" {
		hideConsole()
	}

	systray.Run(onReady, onExit)
}

// ─── Windows console hiding ───

func hideConsole() {
	// Only works on Windows
	// Uses the -H windowsgui linker flag at build time to suppress console.
	// At runtime, no additional action needed.
}
