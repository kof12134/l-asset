// L-Asset - 资产管理系统
// Copyright (c) 2026 乐为爸爸. All rights reserved.
// 未经授权禁止复制、分发或修改本软件。

package main

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// ─── Data Models ───

type ScrappedAsset struct {
	ID            int     `json:"id"`
	AssetTag      string  `json:"asset_tag"`
	Type          string  `json:"type"`
	Brand         string  `json:"brand"`
	Model         string  `json:"model"`
	Serial        string  `json:"serial"`
	CPU           string  `json:"cpu"`
	Memory        string  `json:"memory"`
	Disk          string  `json:"disk"`
	Status        string  `json:"status"`
	PurchaseDate  string  `json:"purchase_date"`
	PurchasePrice float64 `json:"purchase_price"`
	Supplier      string  `json:"supplier"`
	WarrantyEnd   string  `json:"warranty_end"`
	CurrentUser   string  `json:"current_user"`
	Location      string  `json:"location"`
	Notes         string  `json:"notes"`
	ScrapReason   string  `json:"scrap_reason"`
	ScrapNotes    string  `json:"scrap_notes"`
	ScrappedBy    string  `json:"scrapped_by"`
	ScrappedAt    string  `json:"scrapped_at"`
	RestoredAt    string  `json:"restored_at"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

type Asset struct {
	ID            int     `json:"id"`
	AssetTag      string  `json:"asset_tag"`
	Type          string  `json:"type"`
	Brand         string  `json:"brand"`
	Model         string  `json:"model"`
	Serial        string  `json:"serial"`
	CPU           string  `json:"cpu"`
	Memory        string  `json:"memory"`
	Disk          string  `json:"disk"`
	Status        string  `json:"status"`
	PurchaseDate  string  `json:"purchase_date"`
	PurchasePrice float64 `json:"purchase_price"`
	Supplier      string  `json:"supplier"`
	WarrantyEnd   string  `json:"warranty_end"`
	CurrentUser   string  `json:"current_user"`
	Location      string  `json:"location"`
	Notes         string  `json:"notes"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	CustomValues  []CustomFieldValue `json:"custom_values,omitempty"`
}

type CustomField struct {
	ID         int    `json:"id"`
	FieldName  string `json:"field_name"`
	FieldType  string `json:"field_type"`
	Options    string `json:"field_options"`
	SortOrder  int    `json:"sort_order"`
}

type CustomFieldValue struct {
	FieldID    int    `json:"field_id"`
	FieldName  string `json:"field_name"`
	FieldValue string `json:"field_value"`
}

type User struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Department string `json:"department"`
	Phone      string `json:"phone"`
	Email      string `json:"email"`
	Password   string `json:"-"`
	Role       string `json:"role"`
	Notes      string `json:"notes"`
	Active     int    `json:"active"`
	CreatedAt  string `json:"created_at"`
}

type Transaction struct {
	ID          int    `json:"id"`
	AssetID     int    `json:"asset_id"`
	AssetTag    string `json:"asset_tag"`
	Action      string `json:"action"`
	Operator    string `json:"operator"`
	TargetUser  string `json:"target_user"`
	Notes       string `json:"notes"`
	CreatedAt   string `json:"created_at"`
}

type Attachment struct {
	ID        int    `json:"id"`
	AssetID   int    `json:"asset_id"`
	FileName  string `json:"file_name"`
	FilePath  string `json:"file_path"`
	FileSize  int64  `json:"file_size"`
	MimeType  string `json:"mime_type"`
	CreatedAt string `json:"created_at"`
}

// ─── Globals ───

var db *sql.DB
var appDataDir string

// Session management
var (
	sessions = sync.Map{} // token -> Session
)

type Session struct {
	UserID   int
	UserName string
	Role     string
	Expires  time.Time
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func hashPassword(pwd string) string {
	h := sha256.Sum256([]byte(pwd))
	return fmt.Sprintf("%x", h)
}

func getSession(r *http.Request) *Session {
	cookie, err := r.Cookie("lasset_token")
	if err != nil {
		return nil
	}
	v, ok := sessions.Load(cookie.Value)
	if !ok {
		return nil
	}
	s := v.(Session)
	if time.Now().After(s.Expires) {
		sessions.Delete(cookie.Value)
		return nil
	}
	return &s
}

func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := getSession(r)
		if s == nil {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				jsonErr(w, "Unauthorized", 401)
			} else {
				http.Redirect(w, r, "/login", http.StatusFound)
			}
			return
		}
		next(w, r)
	}
}

func requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := getSession(r)
		if s == nil {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				jsonErr(w, "Unauthorized", 401)
			} else {
				http.Redirect(w, r, "/login", http.StatusFound)
			}
			return
		}
		if s.Role != "admin" {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				jsonErr(w, "Forbidden: admin required", 403)
			} else {
				http.Error(w, "Forbidden", 403)
			}
			return
		}
		next(w, r)
	}
}

//go:embed templates/*
var templateFS embed.FS

func main() {
	appDataDir = "./data"
	if os.Getenv("LASSET_DATA") != "" {
		appDataDir = os.Getenv("LASSET_DATA")
	}
	os.MkdirAll(appDataDir, 0755)

	port := "5678"
	if os.Getenv("LASSET_PORT") != "" {
		port = os.Getenv("LASSET_PORT")
	}

	var err error
	dbPath := filepath.Join(appDataDir, "l-asset.db")
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	db.SetMaxOpenConns(1) // SQLite doesn't support concurrent writes

	initDB()
	ensureDefaults()

	mux := http.NewServeMux()

	// Auth routes (no auth required)
	mux.HandleFunc("/login", handleLoginPage)
	mux.HandleFunc("/api/login", handleLogin)

	// API routes (auth required)
	mux.HandleFunc("/api/assets", requireAuth(handleAssets))
	mux.HandleFunc("/api/assets/", requireAuth(handleAssetByID))
	mux.HandleFunc("/api/assets/batch-import", requireAuth(handleBatchImport))
	mux.HandleFunc("/api/assets/batch/", requireAuth(handleBatchAction))
	mux.HandleFunc("/api/assets/batch-delete", requireAdmin(handleBatchDelete))
	mux.HandleFunc("/api/assets/export", requireAuth(handleExport))
	mux.HandleFunc("/api/assets/template", requireAuth(handleDownloadTemplate))
	mux.HandleFunc("/api/transactions", requireAuth(handleTransactions))
	mux.HandleFunc("/api/fields", requireAuth(handleCustomFields))
	mux.HandleFunc("/api/stats", requireAuth(handleStats))
	mux.HandleFunc("/api/field-presets", requireAuth(handleFieldPresets))
	mux.HandleFunc("/api/default-fields", requireAuth(handleDefaultFields))
	mux.HandleFunc("/api/users", requireAuth(handleUsers))
	mux.HandleFunc("/api/users/", requireAuth(handleUserByID))
	mux.HandleFunc("/api/logout", requireAuth(handleLogout))
	mux.HandleFunc("/api/me", requireAuth(handleMe))
	mux.HandleFunc("/api/settings", requireAuth(handleSettings))
	mux.HandleFunc("/api/system/export-xml", requireAuth(handleExportXML))
	mux.HandleFunc("/api/system/import-xml", requireAuth(handleImportXML))
	mux.HandleFunc("/api/system/export-backup", requireAuth(handleExportBackup))
	mux.HandleFunc("/api/system/import-backup", requireAuth(handleImportBackup))
	mux.HandleFunc("/api/attachments/", requireAuth(handleAttachments))
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := getSession(r)
		if s == nil {
			http.Error(w, "Unauthorized", 401)
			return
		}
		http.FileServer(http.Dir(filepath.Join(appDataDir, "uploads"))).ServeHTTP(w, r)
	})))

	// Scrapped assets routes
	mux.HandleFunc("/api/scrapped", requireAuth(handleScrapped))
	mux.HandleFunc("/api/scrapped/", requireAuth(handleScrappedByID))
	mux.HandleFunc("/scrapped", requireAuth(handleScrappedPage))
	mux.HandleFunc("/scrapped/asset/", requireAuth(handleScrappedAssetDetailPage))

	// Page routes (auth required)
	mux.HandleFunc("/", requireAuth(handleIndex))
	mux.HandleFunc("/assets", requireAuth(handleAssetsPage))
	mux.HandleFunc("/asset/", requireAuth(handleAssetDetailPage))
	mux.HandleFunc("/import", requireAuth(handleImportPage))
	mux.HandleFunc("/fields", requireAuth(handleFieldsPage))
	mux.HandleFunc("/transactions", requireAuth(handleTransactionsPage))
	mux.HandleFunc("/users", requireAuth(handleUsersPage))
	mux.HandleFunc("/user/", requireAuth(handleUserAssetsPage))
	mux.HandleFunc("/settings", requireAuth(handleSettingsPage))
	mux.HandleFunc("/finance", requireAuth(handleFinancePage))
	mux.HandleFunc("/api/finance/summary", requireAuth(handleFinanceSummary))

	log.Printf("L-Asset started on http://0.0.0.0:%s", port)
	log.Printf("Database: %s", dbPath)
	log.Fatal(http.ListenAndServe("0.0.0.0:"+port, mux))
}

// ─── Database init ───

func initDB() {
	schema := `
	CREATE TABLE IF NOT EXISTS assets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		asset_tag TEXT UNIQUE,
		type TEXT DEFAULT '',
		brand TEXT DEFAULT '',
		model TEXT DEFAULT '',
		serial TEXT DEFAULT '',
		cpu TEXT DEFAULT '',
		memory TEXT DEFAULT '',
		disk TEXT DEFAULT '',
		status TEXT DEFAULT '在库',
		purchase_date TEXT DEFAULT '',
		purchase_price REAL DEFAULT 0,
		supplier TEXT DEFAULT '',
		warranty_end TEXT DEFAULT '',
		current_user TEXT DEFAULT '',
		location TEXT DEFAULT '',
		notes TEXT DEFAULT '',
		created_at TEXT DEFAULT (datetime('now','localtime')),
		updated_at TEXT DEFAULT (datetime('now','localtime'))
	);
	CREATE TABLE IF NOT EXISTS custom_fields (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		field_name TEXT UNIQUE NOT NULL,
		field_type TEXT NOT NULL DEFAULT 'text',
		field_options TEXT DEFAULT '',
		sort_order INTEGER DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS custom_field_values (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		asset_id INTEGER NOT NULL,
		field_id INTEGER NOT NULL,
		field_value TEXT DEFAULT '',
		FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE,
		FOREIGN KEY(field_id) REFERENCES custom_fields(id) ON DELETE CASCADE,
		UNIQUE(asset_id, field_id)
	);
	CREATE TABLE IF NOT EXISTS transactions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		asset_id INTEGER,
		action TEXT NOT NULL,
		operator TEXT DEFAULT '',
		target_user TEXT DEFAULT '',
		notes TEXT DEFAULT '',
		created_at TEXT DEFAULT (datetime('now','localtime')),
		asset_tag TEXT DEFAULT ''
	);
		CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT UNIQUE NOT NULL,
		department TEXT DEFAULT '',
		phone TEXT DEFAULT '',
		email TEXT DEFAULT '',
		password TEXT DEFAULT '',
		role TEXT DEFAULT 'user',
		notes TEXT DEFAULT '',
		active INTEGER DEFAULT 1,
		created_at TEXT DEFAULT (datetime('now','localtime'))
	);
	CREATE TABLE IF NOT EXISTS field_presets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		field_key TEXT NOT NULL,
		field_value TEXT NOT NULL,
		sort_order INTEGER DEFAULT 0,
		UNIQUE(field_key, field_value)
	);
	-- Add abbr column for type abbreviations if not exists
		CREATE TABLE IF NOT EXISTS attachments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		asset_id INTEGER NOT NULL,
		file_name TEXT NOT NULL,
		file_path TEXT NOT NULL,
		file_size INTEGER DEFAULT 0,
		mime_type TEXT DEFAULT '',
		created_at TEXT DEFAULT (datetime('now','localtime')),
		FOREIGN KEY(asset_id) REFERENCES assets(id) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS scrapped_assets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		asset_tag TEXT,
		type TEXT DEFAULT '',
		brand TEXT DEFAULT '',
		model TEXT DEFAULT '',
		serial TEXT DEFAULT '',
		cpu TEXT DEFAULT '',
		memory TEXT DEFAULT '',
		disk TEXT DEFAULT '',
		status TEXT DEFAULT '已报废',
		purchase_date TEXT DEFAULT '',
		purchase_price REAL DEFAULT 0,
		supplier TEXT DEFAULT '',
		warranty_end TEXT DEFAULT '',
		current_user TEXT DEFAULT '',
		location TEXT DEFAULT '',
		notes TEXT DEFAULT '',
		scrap_reason TEXT DEFAULT '',
		scrap_notes TEXT DEFAULT '',
		scrapped_by TEXT DEFAULT '',
		scrapped_at TEXT DEFAULT (datetime('now','localtime')),
		restored_at TEXT DEFAULT '',
		created_at TEXT DEFAULT '',
		updated_at TEXT DEFAULT ''
	);
	`
	_, err := db.Exec(schema)
	if err != nil {
		log.Fatalf("Failed to init schema: %v", err)
	}
}

func ensureDefaults() {
	// Make sure default custom fields exist
	defaults := []struct {
		name string
		typ  string
	}{
		{"操作系统", "text"},
		{"屏幕尺寸", "text"},
	}
	for _, d := range defaults {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM custom_fields WHERE field_name=?", d.name).Scan(&count)
		if count == 0 {
			db.Exec("INSERT INTO custom_fields (field_name, field_type) VALUES (?, ?)", d.name, d.typ)
		}
	}

	// Ensure users table has password and role columns (migration for existing DBs)
	db.Exec("ALTER TABLE users ADD COLUMN password TEXT DEFAULT ''")
	db.Exec("ALTER TABLE users ADD COLUMN role TEXT DEFAULT 'user'")
	db.Exec("ALTER TABLE field_presets ADD COLUMN abbr TEXT DEFAULT ''")

	// If no users exist, create default admin
	var userCount int
	db.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount)
	if userCount == 0 {
		adminPW := hashPassword("admin123")
		db.Exec("INSERT INTO users (name, department, password, role) VALUES (?, '', ?, 'admin')", "admin", adminPW)
		log.Println("Default admin user created: admin / admin123")
	}

	// Default presets for select fields
	defaultPresets := []struct {
		key, value string
	}{
		{"type", "笔记本"},
		{"type", "台式机"},
		{"type", "显示器"},
		{"type", "打印机"},
		{"type", "其他"},
		{"status", "在库"},
		{"status", "已领用"},
		{"status", "已报废"},
		{"brand", "Lenovo"},
		{"brand", "Dell"},
		{"brand", "HP"},
		{"brand", "Apple"},
		{"brand", "Huawei"},
		{"location", "办公室"},
		{"location", "机房"},
		{"location", "仓库"},
		{"location", "会议室"},
		{"warranty_years", "1"},
		{"warranty_years", "2"},
		{"warranty_years", "3"},
		{"warranty_years", "5"},
	}
	for _, p := range defaultPresets {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM field_presets WHERE field_key=? AND field_value=?", p.key, p.value).Scan(&count)
		if count == 0 {
			db.Exec("INSERT INTO field_presets (field_key, field_value, sort_order) VALUES (?, ?, 0)", p.key, p.value)
		}
	}
}

// ─── Templates ───

func render(w http.ResponseWriter, pageTmpl string, data interface{}) {
	// Parse just layout + the specific page template (no conflict of define "content")
	t := template.New("").Funcs(template.FuncMap{
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		"add":      func(a, b int) int { return a + b },
		"sub":      func(a, b int) int { return a - b },
		"mul":      func(a, b float64) float64 { return a * b },
		"seq":      func(n int) []int { r := make([]int, n); for i := range r { r[i] = i }; return r },
	})

	_, err := t.ParseFS(templateFS, "templates/layout.html", pageTmpl)
	if err != nil {
		log.Printf("Template parse error: %v", err)
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = t.ExecuteTemplate(w, "layout.html", data)
	if err != nil {
		log.Printf("Template exec error: %v", err)
		http.Error(w, err.Error(), 500)
	}
}

func renderStandalone(w http.ResponseWriter, pageTmpl string, data interface{}) {
	t := template.New("").Funcs(template.FuncMap{
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
	})

	_, err := t.ParseFS(templateFS, pageTmpl)
	if err != nil {
		log.Printf("Template parse error: %v", err)
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = t.ExecuteTemplate(w, pageTmpl, data)
	if err != nil {
		log.Printf("Template exec error: %v", err)
		http.Error(w, err.Error(), 500)
	}
}

func jsonResp(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ─── Page Handlers ───

func handleLoginPage(w http.ResponseWriter, r *http.Request) {
	// If already logged in, redirect to home
	s := getSession(r)
	if s != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	renderStandalone(w, "templates/login.html", nil)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s := getSession(r)
	render(w, "templates/index.html", map[string]interface{}{
		"Page":     "index",
		"PageName": "概览",
		"User":     s.UserName,
		"IsAdmin":  s.Role == "admin",
	})
}

func handleAssetsPage(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	render(w, "templates/assets.html", map[string]interface{}{
		"Page":     "assets",
		"PageName": "资产列表",
		"User":     s.UserName,
		"IsAdmin":  s.Role == "admin",
	})
}

func handleAssetDetailPage(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/asset/")
	idStr = strings.TrimSuffix(idStr, "/")
	if idStr == "" {
		http.Redirect(w, r, "/assets", http.StatusFound)
		return
	}
	s := getSession(r)
	render(w, "templates/asset_detail.html", map[string]interface{}{
		"Page":     "asset_detail",
		"PageName": "资产详情",
		"AssetID":  idStr,
		"User":     s.UserName,
		"IsAdmin":  s.Role == "admin",
	})
}

func handleImportPage(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s.Role != "admin" {
		http.Redirect(w, r, "/", 302)
		return
	}
	render(w, "templates/import.html", map[string]interface{}{
		"Page":     "import",
		"PageName": "批量导入",
		"User":     s.UserName,
		"IsAdmin":  s.Role == "admin",
	})
}

func handleFieldsPage(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s.Role != "admin" {
		http.Error(w, "Forbidden", 403)
		return
	}
	render(w, "templates/fields.html", map[string]interface{}{
		"Page":     "fields",
		"PageName": "自定义字段",
		"User":     s.UserName,
		"IsAdmin":  s.Role == "admin",
	})
}

func handleTransactionsPage(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	render(w, "templates/transactions.html", map[string]interface{}{
		"Page":     "transactions",
		"PageName": "操作记录",
		"User":     s.UserName,
		"IsAdmin":  s.Role == "admin",
	})
}

func handleUsersPage(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s.Role != "admin" {
		http.Error(w, "Forbidden", 403)
		return
	}
	render(w, "templates/users.html", map[string]interface{}{
		"Page":     "users",
		"PageName": "用户管理",
		"User":     s.UserName,
		"IsAdmin":  s.Role == "admin",
	})
}

func handleUserAssetsPage(w http.ResponseWriter, r *http.Request) {
	// /user/{id}/assets
	path := strings.TrimPrefix(r.URL.Path, "/user/")
	path = strings.TrimSuffix(path, "/assets")
	path = strings.TrimSuffix(path, "/")
	uid, err := strconv.Atoi(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	s := getSession(r)

	var userName string
	err = db.QueryRow("SELECT name FROM users WHERE id=?", uid).Scan(&userName)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	type TmplAsset struct {
		ID           int
		AssetTag     string
		Type         string
		Brand        string
		Model        string
		Serial       string
		Location     string
		Status       string
		CheckoutTime string
		ReturnTime   string
	}

	// Current checked-out assets
	curRows, err := db.Query(`
		SELECT a.id, a.asset_tag, a.type, a.brand, a.model, a.serial, a.status, a.current_user, a.location,
			(SELECT MAX(t.created_at) FROM transactions t WHERE t.asset_id = a.id AND (t.action = 'checkout' OR t.action = '领用') AND t.target_user = ?) AS checkout_time
		FROM assets a
		WHERE a.current_user=? AND a.status='已领用'
		ORDER BY a.asset_tag`, userName, userName)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer curRows.Close()

	var currentAssets []TmplAsset
	for curRows.Next() {
		var a TmplAsset
		var curUser string
		var checkTime sql.NullString
		curRows.Scan(&a.ID, &a.AssetTag, &a.Type, &a.Brand, &a.Model, &a.Serial, &a.Status, &curUser, &a.Location, &checkTime)
		if checkTime.Valid {
			a.CheckoutTime = checkTime.String
		}
		currentAssets = append(currentAssets, a)
	}

	// Historical assets (previously checked out but no longer)
	histRows, err := db.Query(`
		SELECT t.asset_id, t.asset_tag, a.type, a.brand, a.model, a.serial, a.location,
			t.created_at AS checkout_time,
			(SELECT MIN(t3.created_at) FROM transactions t3 WHERE t3.asset_id = t.asset_id AND (t3.action = 'checkin' OR t3.action = '归还') AND t3.created_at > t.created_at) AS return_time
		FROM transactions t
		LEFT JOIN assets a ON t.asset_id = a.id
		WHERE t.target_user = ? AND (t.action = 'checkout' OR t.action = '领用')
		ORDER BY t.created_at DESC`, userName)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer histRows.Close()

	var historyAssets []TmplAsset
	for histRows.Next() {
		var a TmplAsset
		var checkTime sql.NullString
		var returnTime sql.NullString
		var aid sql.NullInt64
		var tag, typ, brand, model, serial, loc sql.NullString
		histRows.Scan(&aid, &tag, &typ, &brand, &model, &serial, &loc, &checkTime, &returnTime)
		if aid.Valid {
			a.ID = int(aid.Int64)
		}
		if tag.Valid {
			a.AssetTag = tag.String
		}
		if typ.Valid {
			a.Type = typ.String
		}
		if brand.Valid {
			a.Brand = brand.String
		}
		if model.Valid {
			a.Model = model.String
		}
		if serial.Valid {
			a.Serial = serial.String
		}
		if loc.Valid {
			a.Location = loc.String
		}
		if checkTime.Valid {
			a.CheckoutTime = checkTime.String
		}
		if returnTime.Valid {
			a.ReturnTime = returnTime.String
		}
		if !aid.Valid {
			a.Status = "(资产已删除)"
		}
		historyAssets = append(historyAssets, a)
	}

		// Collect distinct locations for the checkout dialog
	locRows, _ := db.Query("SELECT DISTINCT location FROM assets WHERE location != '' ORDER BY location")
	var locations []string
	if locRows != nil {
		defer locRows.Close()
		for locRows.Next() {
			var loc string
			locRows.Scan(&loc)
			locations = append(locations, loc)
		}
	}

	render(w, "templates/user_assets.html", map[string]interface{}{
		"Page":          "users",
		"PageName":      userName + " - 名下资产",
		"User":          s.UserName,
		"IsAdmin":       s.Role == "admin",
		"UserName":      userName,
		"CurrentAssets": currentAssets,
		"HistoryAssets": historyAssets,
		"Locations":     locations,
	})
}

func handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	render(w, "templates/settings.html", map[string]interface{}{
		"Page":     "settings",
		"PageName": "设置",
		"User":     s.UserName,
		"IsAdmin":  s.Role == "admin",
	})
}

// ─── API: Assets ───

func handleAssets(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		listAssets(w, r)
	} else if r.Method == "POST" {
		createAsset(w, r)
	} else {
		http.Error(w, "Method not allowed", 405)
	}
}

func listAssets(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	status := r.URL.Query().Get("status")
	assetType := r.URL.Query().Get("type")
	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("pageSize")
	sortBy := r.URL.Query().Get("sortBy")
	sortOrder := r.URL.Query().Get("sortOrder")

	page, _ := strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(pageSizeStr)
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}

	includeScrapped := r.URL.Query().Get("include_scrapped") == "true"

	// Build query
	where := []string{"1=1"}
	args := []interface{}{}

	if !includeScrapped {
		where = append(where, "status != ?")
		args = append(args, "已报废")
	}

	if search != "" {
		where = append(where, "(asset_tag LIKE ? OR brand LIKE ? OR model LIKE ? OR serial LIKE ? OR current_user LIKE ? OR notes LIKE ?)")
		s := "%" + search + "%"
		args = append(args, s, s, s, s, s, s)
	}
	if status != "" {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	if assetType != "" {
		where = append(where, "type = ?")
		args = append(args, assetType)
	}

	whereClause := strings.Join(where, " AND ")

	// Count
	var total int
	countQuery := "SELECT COUNT(*) FROM assets WHERE " + whereClause
	db.QueryRow(countQuery, args...).Scan(&total)

	// Order
	validSorts := map[string]bool{"asset_tag": true, "brand": true, "model": true, "status": true, "purchase_date": true, "created_at": true, "updated_at": true, "current_user": true, "type": true}
	if !validSorts[sortBy] {
		sortBy = "created_at"
	}
	if sortOrder != "asc" {
		sortOrder = "desc"
	}

	offset := (page - 1) * pageSize
	query := fmt.Sprintf("SELECT id, asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes, created_at, updated_at FROM assets WHERE %s ORDER BY %s %s LIMIT ? OFFSET ?", whereClause, sortBy, sortOrder)
	args = append(args, pageSize, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	assets := []Asset{}
	for rows.Next() {
		var a Asset
		rows.Scan(&a.ID, &a.AssetTag, &a.Type, &a.Brand, &a.Model, &a.Serial, &a.CPU, &a.Memory, &a.Disk, &a.Status, &a.PurchaseDate, &a.PurchasePrice, &a.Supplier, &a.WarrantyEnd, &a.CurrentUser, &a.Location, &a.Notes, &a.CreatedAt, &a.UpdatedAt)
		assets = append(assets, a)
	}

	totalPages := int(math.Ceil(float64(total) / float64(pageSize)))

	cfg := loadConfig()
	jsonResp(w, map[string]interface{}{
		"assets":        assets,
		"total":         total,
		"page":          page,
		"pageSize":      pageSize,
		"totalPages":    totalPages,
		"finance": map[string]interface{}{
			"tax_rate":        cfg.TaxRate,
			"base_currency":   cfg.BaseCurrency,
			"exchange_rates":  cfg.ExchangeRates,
		},
	})
}

func createAsset(w http.ResponseWriter, r *http.Request) {
	var a Asset
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		jsonErr(w, "Invalid JSON: "+err.Error(), 400)
		return
	}

	if a.AssetTag == "" {
		// Auto-generate tag: company_name-abbr-序列号 (e.g. 乐乐-NB-001)
		cfg := loadConfig()
		company := cfg.CompanyName
		if company == "" {
			company = "PC"
		}
		abbr := ""
		if a.Type != "" {
			db.QueryRow("SELECT abbr FROM field_presets WHERE field_key='type' AND field_value=?", a.Type).Scan(&abbr)
		}
		if abbr == "" {
			abbr = a.Type
			if abbr == "" {
				abbr = "PC"
			}
		}
		prefix := fmt.Sprintf("%s-%s-", company, abbr)
		pattern := prefix + "%"
		var maxSeq int
		db.QueryRow("SELECT COALESCE(MAX(CAST(SUBSTR(asset_tag, ?) AS INTEGER)), 0) FROM assets WHERE asset_tag LIKE ?", len(prefix)+1, pattern).Scan(&maxSeq)
		a.AssetTag = fmt.Sprintf("%s%03d", prefix, maxSeq+1)
	}
	// Auto-set status based on current_user (override whatever frontend sent)
	if a.CurrentUser != "" {
		a.Status = "已领用"
	} else if a.Status == "" || a.Status == "已领用" {
		a.Status = "在库"
	}

	result, err := db.Exec(`INSERT INTO assets (asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.AssetTag, a.Type, a.Brand, a.Model, a.Serial, a.CPU, a.Memory, a.Disk, a.Status, a.PurchaseDate, a.PurchasePrice, a.Supplier, a.WarrantyEnd, a.CurrentUser, a.Location, a.Notes)
	if err != nil {
		jsonErr(w, err.Error(), 400)
		return
	}

	id, _ := result.LastInsertId()
	a.ID = int(id)

	// Log transaction
	db.Exec("INSERT INTO transactions (asset_id, action, operator, notes, asset_tag) VALUES (?, 'create', ?, '资产入库', ?)",
		id, operatorName(r), a.AssetTag)

	jsonResp(w, a)
}

// ─── API: Asset by ID ───

func handleAssetByID(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/assets/")
	idStr = strings.TrimSuffix(idStr, "/")

	// Handle sub-routes
	if strings.HasSuffix(idStr, "/checkout") {
		assetAction(w, r, "checkout")
		return
	}
	if strings.HasSuffix(idStr, "/checkin") {
		assetAction(w, r, "checkin")
		return
	}
	if strings.HasSuffix(idStr, "/scrap") {
		assetAction(w, r, "scrap")
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		jsonErr(w, "Invalid ID", 400)
		return
	}

	switch r.Method {
	case "GET":
		getAsset(w, id)
	case "PUT":
		updateAsset(w, r, id)
	case "DELETE":
		deleteAsset(w, id)
	default:
		http.Error(w, "Method not allowed", 405)
	}
}

func getAsset(w http.ResponseWriter, id int) {
	var a Asset
	err := db.QueryRow(`SELECT id, asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes, created_at, updated_at FROM assets WHERE id=?`, id).
		Scan(&a.ID, &a.AssetTag, &a.Type, &a.Brand, &a.Model, &a.Serial, &a.CPU, &a.Memory, &a.Disk, &a.Status, &a.PurchaseDate, &a.PurchasePrice, &a.Supplier, &a.WarrantyEnd, &a.CurrentUser, &a.Location, &a.Notes, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		jsonErr(w, "Asset not found", 404)
		return
	}
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}

	// Get custom values
	rows, _ := db.Query(`SELECT cv.field_id, cf.field_name, cv.field_value FROM custom_field_values cv JOIN custom_fields cf ON cv.field_id=cf.id WHERE cv.asset_id=?`, id)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var cv CustomFieldValue
			rows.Scan(&cv.FieldID, &cv.FieldName, &cv.FieldValue)
			a.CustomValues = append(a.CustomValues, cv)
		}
	}

	cfg := loadConfig()
	jsonResp(w, map[string]interface{}{
		"asset":  a,
		"finance": map[string]interface{}{
			"tax_rate":       cfg.TaxRate,
			"base_currency":  cfg.BaseCurrency,
			"exchange_rates": cfg.ExchangeRates,
		},
	})
}

func updateAsset(w http.ResponseWriter, r *http.Request, id int) {
	var a Asset
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		jsonErr(w, "Invalid JSON: "+err.Error(), 400)
		return
	}

	// Determine status: prefer explicit value from edit form, else auto-set based on current_user
	newStatus := a.Status
	if newStatus == "" {
		var currentStatus string
		db.QueryRow("SELECT status FROM assets WHERE id=?").Scan(&currentStatus)
		newStatus = currentStatus
	}
	if a.CurrentUser != "" {
		newStatus = "已领用"
	} else if newStatus == "已领用" {
		newStatus = "在库"
	}

	_, err := db.Exec(`UPDATE assets SET asset_tag=?, type=?, brand=?, model=?, serial=?, cpu=?, memory=?, disk=?, purchase_date=?, purchase_price=?, supplier=?, warranty_end=?, status=?, current_user=?, location=?, notes=?, updated_at=datetime('now','localtime') WHERE id=?`,
		a.AssetTag, a.Type, a.Brand, a.Model, a.Serial, a.CPU, a.Memory, a.Disk, a.PurchaseDate, a.PurchasePrice, a.Supplier, a.WarrantyEnd, newStatus, a.CurrentUser, a.Location, a.Notes, id)
	if err != nil {
		jsonErr(w, err.Error(), 400)
		return
	}

	// Save custom values
	if a.CustomValues != nil {
		for _, cv := range a.CustomValues {
			db.Exec(`INSERT INTO custom_field_values (asset_id, field_id, field_value) VALUES (?,?,?) ON CONFLICT(asset_id, field_id) DO UPDATE SET field_value=?`,
				id, cv.FieldID, cv.FieldValue, cv.FieldValue)
		}
	}

	jsonResp(w, map[string]string{"status": "ok"})
}

func deleteAsset(w http.ResponseWriter, id int) {
	// Get asset tag for log
	var tag string
	db.QueryRow("SELECT asset_tag FROM assets WHERE id=?", id).Scan(&tag)

	db.Exec("DELETE FROM assets WHERE id=?", id)
	db.Exec("INSERT INTO transactions (asset_id, action, operator, notes, asset_tag) VALUES (?, 'delete', 'system', '资产已删除', ?)", id, tag)
	jsonResp(w, map[string]string{"status": "ok"})
}

// ─── API: Asset Actions (checkout/checkin/scrap) ───

func assetAction(w http.ResponseWriter, r *http.Request, action string) {
	if r.Method != "POST" {
		jsonErr(w, "POST required", 405)
		return
	}

	// Parse ID from path
	idStr := r.URL.Path
	// Remove the action suffix
	idStr = strings.TrimSuffix(idStr, "/"+action)
	idStr = strings.TrimSuffix(idStr, "/checkout")
	idStr = strings.TrimSuffix(idStr, "/checkin")
	idStr = strings.TrimSuffix(idStr, "/scrap")
	// Get the last segment as ID
	parts := strings.Split(strings.TrimRight(idStr, "/"), "/")
	idStr = parts[len(parts)-1]

	id, err := strconv.Atoi(idStr)
	if err != nil {
		jsonErr(w, "Invalid ID", 400)
		return
	}

	var body struct {
		TargetUser string `json:"target_user"`
		Operator   string `json:"operator"`
		Notes      string `json:"notes"`
		Reason     string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if body.Operator == "" {
		body.Operator = "admin"
	}

	var newStatus string
	var logAction string
	switch action {
	case "checkout":
		newStatus = "已领用"
		logAction = "checkout"
	case "checkin":
		newStatus = "在库"
		logAction = "checkin"
		body.TargetUser = ""
	case "scrap":
		newStatus = "已报废"
		logAction = "scrap"
		body.TargetUser = ""
	}

	if action == "scrap" {
		// Move asset to scrapped_assets table (transactional)
		tx, err := db.Begin()
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		defer tx.Rollback()

		// Read full asset
		var a Asset
		err = tx.QueryRow(`SELECT id, asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes, created_at, updated_at FROM assets WHERE id=?`, id).
			Scan(&a.ID, &a.AssetTag, &a.Type, &a.Brand, &a.Model, &a.Serial, &a.CPU, &a.Memory, &a.Disk, &a.Status, &a.PurchaseDate, &a.PurchasePrice, &a.Supplier, &a.WarrantyEnd, &a.CurrentUser, &a.Location, &a.Notes, &a.CreatedAt, &a.UpdatedAt)
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}

		// Insert into scrapped_assets
		_, err = tx.Exec(`INSERT INTO scrapped_assets (asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes, scrap_reason, scrap_notes, scrapped_by, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			a.AssetTag, a.Type, a.Brand, a.Model, a.Serial, a.CPU, a.Memory, a.Disk, "已报废",
			a.PurchaseDate, a.PurchasePrice, a.Supplier, a.WarrantyEnd, a.CurrentUser, a.Location, a.Notes,
			body.Reason, body.Notes, body.Operator, a.CreatedAt, a.UpdatedAt)
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}

		// Delete from assets
		_, err = tx.Exec("DELETE FROM assets WHERE id=?", id)
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}

		// Log transaction
		_, err = tx.Exec("INSERT INTO transactions (asset_id, action, operator, target_user, notes, asset_tag) VALUES (?,?,?,?,?,?)",
			id, logAction, body.Operator, body.TargetUser, body.Notes, a.AssetTag)
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}

		if err := tx.Commit(); err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}

		jsonResp(w, map[string]string{"status": "ok", "new_status": "已报废"})
		return
	}

	// 检查资产状态:仅当资产在库时允许领用
	if action == "checkout" || action == "领用" {
		var curStatus string
		db.QueryRow("SELECT status FROM assets WHERE id=?", id).Scan(&curStatus)
		if curStatus == "已领用" {
			jsonErr(w, "该资产已被其他用户领用", 400)
			return
		}
		if curStatus == "已报废" {
			jsonErr(w, "该资产已报废", 400)
			return
		}
	}

	// Get asset tag
	var tag string
	db.QueryRow("SELECT asset_tag FROM assets WHERE id=?", id).Scan(&tag)

	_, err = db.Exec("UPDATE assets SET status=?, current_user=?, updated_at=datetime('now','localtime') WHERE id=?", newStatus, body.TargetUser, id)
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}

	db.Exec("INSERT INTO transactions (asset_id, action, operator, target_user, notes, asset_tag) VALUES (?,?,?,?,?,?)",
		id, logAction, body.Operator, body.TargetUser, body.Notes, tag)

	jsonResp(w, map[string]string{"status": "ok", "new_status": newStatus})
}

// ─── API: Batch Actions (checkout/checkin/scrap) ───

func handleBatchAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonErr(w, "POST required", 405)
		return
	}

	// Extract action from URL: /api/assets/batch/checkout
	path := strings.TrimPrefix(r.URL.Path, "/api/assets/batch/")
	action := strings.TrimSuffix(path, "/")
	if action != "checkout" && action != "checkin" && action != "scrap" {
		jsonErr(w, "Invalid action: "+action, 400)
		return
	}

	var body struct {
		IDs        []int  `json:"ids"`
		TargetUser string `json:"target_user"`
		Operator   string `json:"operator"`
		Notes      string `json:"notes"`
		Reason     string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, "Invalid JSON: "+err.Error(), 400)
		return
	}
	if len(body.IDs) == 0 {
		jsonErr(w, "ids required", 400)
		return
	}
	if body.Operator == "" {
		body.Operator = operatorName(r)
	}

	var newStatus, logAction string
	switch action {
	case "checkout":
		newStatus = "已领用"
		logAction = "checkout"
	case "checkin":
		newStatus = "在库"
		logAction = "checkin"
		body.TargetUser = ""
	case "scrap":
		newStatus = "已报废"
		logAction = "scrap"
		body.TargetUser = ""
	}

	if action == "scrap" {
		// Batch scrap: move assets to scrapped_assets table
		tx, err := db.Begin()
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		defer tx.Rollback()

		affected := 0
		for _, id := range body.IDs {
			var a Asset
			err := tx.QueryRow(`SELECT id, asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes, created_at, updated_at FROM assets WHERE id=?`, id).
				Scan(&a.ID, &a.AssetTag, &a.Type, &a.Brand, &a.Model, &a.Serial, &a.CPU, &a.Memory, &a.Disk, &a.Status, &a.PurchaseDate, &a.PurchasePrice, &a.Supplier, &a.WarrantyEnd, &a.CurrentUser, &a.Location, &a.Notes, &a.CreatedAt, &a.UpdatedAt)
			if err != nil {
				continue
			}

			_, err = tx.Exec(`INSERT INTO scrapped_assets (asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes, scrap_reason, scrap_notes, scrapped_by, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				a.AssetTag, a.Type, a.Brand, a.Model, a.Serial, a.CPU, a.Memory, a.Disk, "已报废",
				a.PurchaseDate, a.PurchasePrice, a.Supplier, a.WarrantyEnd, a.CurrentUser, a.Location, a.Notes,
				body.Reason, body.Notes, body.Operator, a.CreatedAt, a.UpdatedAt)
			if err != nil {
				continue
			}

			_, err = tx.Exec("DELETE FROM assets WHERE id=?", id)
			if err != nil {
				continue
			}

			tx.Exec("INSERT INTO transactions (asset_id, action, operator, target_user, notes, asset_tag) VALUES (?,?,?,?,?,?)",
				id, logAction, body.Operator, "", body.Notes, a.AssetTag)
			affected++
		}

		if err := tx.Commit(); err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}

		jsonResp(w, map[string]interface{}{
			"status":     "ok",
			"affected":   affected,
			"new_status": "已报废",
		})
		return
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(body.IDs))
	args := make([]interface{}, len(body.IDs))
	for i, id := range body.IDs {
		placeholders[i] = "?"
		args[i] = id
	}

	// Get asset tags for transaction log
	rows, err := db.Query(fmt.Sprintf("SELECT id, asset_tag FROM assets WHERE id IN (%s)", strings.Join(placeholders, ",")), args...)
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}
	type idTag struct{ id int; tag string }
	var tags []idTag
	for rows.Next() {
		var t idTag
		rows.Scan(&t.id, &t.tag)
		tags = append(tags, t)
	}
	rows.Close()

	// Update statuses
	if action == "checkout" {
		_, err = db.Exec(fmt.Sprintf("UPDATE assets SET status=?, current_user=?, updated_at=datetime('now','localtime') WHERE id IN (%s)", strings.Join(placeholders, ",")),
			append([]interface{}{newStatus, body.TargetUser}, args...)...)
	} else {
		_, err = db.Exec(fmt.Sprintf("UPDATE assets SET status=?, current_user='', updated_at=datetime('now','localtime') WHERE id IN (%s)", strings.Join(placeholders, ",")),
			append([]interface{}{newStatus}, args...)...)
	}
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}

	// Log each transaction
	for _, t := range tags {
		target := body.TargetUser
		if action != "checkout" {
			target = ""
		}
		db.Exec("INSERT INTO transactions (asset_id, action, operator, target_user, notes, asset_tag) VALUES (?,?,?,?,?,?)",
			t.id, logAction, body.Operator, target, body.Notes, t.tag)
	}

	jsonResp(w, map[string]interface{}{
		"status":     "ok",
		"affected":   len(tags),
		"new_status": newStatus,
	})
}

// ─── API: Batch Delete (requires confirm text) ───

func handleBatchDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonErr(w, "POST required", 405)
		return
	}

	var body struct {
		IDs     []int  `json:"ids"`
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, "Invalid JSON: "+err.Error(), 400)
		return
	}
	if len(body.IDs) == 0 {
		jsonErr(w, "ids required", 400)
		return
	}
	if body.Confirm != "确认删除" {
		jsonErr(w, "请发送 confirm: '确认删除' 来确认此操作", 400)
		return
	}

	// Get asset tags for log
	placeholders := make([]string, len(body.IDs))
	args := make([]interface{}, len(body.IDs))
	for i, id := range body.IDs {
		placeholders[i] = "?"
		args[i] = id
	}

	rows, err := db.Query(fmt.Sprintf("SELECT id, asset_tag FROM assets WHERE id IN (%s)", strings.Join(placeholders, ",")), args...)
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}
	var tags []struct{ id int; tag string }
	for rows.Next() {
		var t struct{ id int; tag string }
		rows.Scan(&t.id, &t.tag)
		tags = append(tags, t)
	}
	rows.Close()

	_, err = db.Exec(fmt.Sprintf("DELETE FROM assets WHERE id IN (%s)", strings.Join(placeholders, ",")), args...)
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}

	// Log each deletion
	for _, t := range tags {
		db.Exec("INSERT INTO transactions (asset_id, action, operator, notes, asset_tag) VALUES (?, 'delete', ?, '批量删除', ?)",
			t.id, operatorName(r), t.tag)
	}

	jsonResp(w, map[string]interface{}{
		"status":   "ok",
		"affected": len(tags),
	})
}

// ─── API: Batch Import ───

func handleBatchImport(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s.Role != "admin" {
		jsonErr(w, "Forbidden: admin only", 403)
		return
	}
	if r.Method != "POST" {
		jsonErr(w, "POST required", 405)
		return
	}

	// Parse multipart form
	err := r.ParseMultipartForm(32 << 20) // 32MB
	if err != nil {
		jsonErr(w, "Parse error: "+err.Error(), 400)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		jsonErr(w, "File required: "+err.Error(), 400)
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	headers, err := reader.Read()
	if err != nil {
		jsonErr(w, "CSV read error: "+err.Error(), 400)
		return
	}

	// Map headers to fields
	// Build field map: support Chinese and English column names
	fieldMap := map[string]string{
		"资产编号":   "asset_tag",
		"类型":     "type",
		"品牌":     "brand",
		"型号":     "model",
		"序列号":    "serial",
		"CPU":    "cpu",
		"内存":     "memory",
		"硬盘":     "disk",
		"状态":     "status",
		"采购日期":   "purchase_date",
		"采购价格":   "purchase_price",
		"供应商":    "supplier",
		"保修到期":   "warranty_end",
		"使用人":    "current_user",
		"位置":     "location",
		"备注":     "notes",
		"asset_tag":      "asset_tag",
		"type":           "type",
		"brand":          "brand",
		"model":          "model",
		"serial":         "serial",
		"cpu":            "cpu",
		"memory":         "memory",
		"disk":           "disk",
		"status":         "status",
		"purchase_date":  "purchase_date",
		"purchase_price": "purchase_price",
		"supplier":       "supplier",
		"warranty_end":   "warranty_end",
		"current_user":   "current_user",
		"location":       "location",
		"notes":          "notes",
	}

	imported := 0
	errors := []string{}

	// Get all custom fields for mapping
	customFields := getCustomFieldMap()

	for rowNum := 0; ; rowNum++ {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			errors = append(errors, fmt.Sprintf("Row %d: %s", rowNum+2, err.Error()))
			continue
		}

		// Build asset from CSV row
		vals := make(map[string]string)
		customVals := make(map[int]string)
		for i, h := range headers {
			if i < len(record) {
				h = strings.TrimSpace(h)
				if dbField, ok := fieldMap[h]; ok {
					vals[dbField] = strings.TrimSpace(record[i])
				} else if fieldID, ok := customFields[h]; ok {
					customVals[fieldID] = strings.TrimSpace(record[i])
				}
			}
		}

		if vals["asset_tag"] == "" {
			var count int
			db.QueryRow("SELECT COUNT(*) FROM assets").Scan(&count)
			vals["asset_tag"] = fmt.Sprintf("PC-%d", count+imported+1)
		}
		if vals["status"] == "" {
			vals["status"] = "在库"
		}

		price, _ := strconv.ParseFloat(vals["purchase_price"], 64)

		result, err := db.Exec(`INSERT INTO assets (asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			vals["asset_tag"], vals["type"], vals["brand"], vals["model"], vals["serial"],
			vals["cpu"], vals["memory"], vals["disk"], vals["status"],
			vals["purchase_date"], price, vals["supplier"], vals["warranty_end"],
			vals["current_user"], vals["location"], vals["notes"])
		if err != nil {
			errors = append(errors, fmt.Sprintf("Row %d: %s", rowNum+2, err.Error()))
			continue
		}

		id, _ := result.LastInsertId()

		// Save custom values
		for fieldID, val := range customVals {
			db.Exec("INSERT INTO custom_field_values (asset_id, field_id, field_value) VALUES (?,?,?)", id, fieldID, val)
		}

		// Log
		db.Exec("INSERT INTO transactions (asset_id, action, operator, notes, asset_tag) VALUES (?, 'create', 'import', 'CSV批量导入', ?)", id, vals["asset_tag"])
		imported++
	}

	jsonResp(w, map[string]interface{}{
		"imported": imported,
		"errors":   errors,
	})
}

func getCustomFieldMap() map[string]int {
	m := make(map[string]int)
	rows, _ := db.Query("SELECT id, field_name FROM custom_fields")
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var id int
			var name string
			rows.Scan(&id, &name)
			m[name] = id
		}
	}
	return m
}

// ─── API: Download template (empty CSV) ───

func handleDownloadTemplate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=l-asset-import-template.csv")
	w.Write([]byte{0xEF, 0xBB, 0xBF}) // BOM for Excel

	writer := csv.NewWriter(w)
	headers := []string{"资产编号", "类型", "品牌", "型号", "序列号", "CPU", "内存", "硬盘", "状态", "采购日期", "采购价格", "供应商", "保修到期", "使用人", "位置", "备注"}

	// Add custom field columns
	cfRows, err := db.Query("SELECT field_name FROM custom_fields ORDER BY sort_order, id")
	if err == nil {
		defer cfRows.Close()
		for cfRows.Next() {
			var name string
			cfRows.Scan(&name)
			headers = append(headers, name)
		}
	}

	writer.Write(headers)
	// Add example row so users can see the format
	example := []string{"", "笔记本", "Lenovo", "ThinkPad X1 Carbon", "PF-XXXXXXXX", "i7-1365U", "16GB", "512GB SSD", "在库", "2026-01-15", "8999", "京东", "2029-01-14", "张三", "3楼办公室", "", "Windows 11", "14英寸"}
	// Pad/shorten example to match header count
	for len(example) < len(headers) {
		example = append(example, "")
	}
	writer.Write(example[:len(headers)])
	writer.Flush()
}

// ─── API: Export ───

func handleExport(w http.ResponseWriter, r *http.Request) {
	// Get custom field info first (close rows before next query)
	customFieldNames := []string{}
	customFieldIDs := []int{}
	{
		cfRows, err := db.Query("SELECT id, field_name FROM custom_fields ORDER BY sort_order, id")
		if err == nil {
			for cfRows.Next() {
				var id int
				var name string
				cfRows.Scan(&id, &name)
				customFieldNames = append(customFieldNames, name)
				customFieldIDs = append(customFieldIDs, id)
			}
			cfRows.Close()
		}
	}

	// Load all assets into memory first (to avoid SQLite connection deadlock)
	type exportRow struct {
		Asset
		CustomVals []string
	}
	var exportRows []exportRow

	// Phase 1: load all basic asset data into memory, close rows
	var basicAssets []Asset
	{
		assetRows, err := db.Query("SELECT id, asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes, created_at FROM assets WHERE status != '已报废' ORDER BY id")
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		for assetRows.Next() {
			var a Asset
			assetRows.Scan(&a.ID, &a.AssetTag, &a.Type, &a.Brand, &a.Model, &a.Serial, &a.CPU, &a.Memory, &a.Disk, &a.Status, &a.PurchaseDate, &a.PurchasePrice, &a.Supplier, &a.WarrantyEnd, &a.CurrentUser, &a.Location, &a.Notes, &a.CreatedAt)
			basicAssets = append(basicAssets, a)
		}
		assetRows.Close()
	}

	// Phase 2: lookup custom values (no concurrent rows open)
	for _, a := range basicAssets {
		er := exportRow{Asset: a}
		for _, fid := range customFieldIDs {
			var val string
			db.QueryRow("SELECT field_value FROM custom_field_values WHERE asset_id=? AND field_id=?", a.ID, fid).Scan(&val)
			er.CustomVals = append(er.CustomVals, val)
		}
		exportRows = append(exportRows, er)
	}

	// Now write CSV to response
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=assets-export.csv")
	w.Write([]byte{0xEF, 0xBB, 0xBF}) // BOM for Excel

	writer := csv.NewWriter(w)
	headers := []string{"资产编号", "类型", "品牌", "型号", "序列号", "CPU", "内存", "硬盘", "状态", "采购日期", "采购价格", "供应商", "保修到期", "使用人", "位置", "备注", "创建时间"}
	headers = append(headers, customFieldNames...)
	writer.Write(headers)

	for _, er := range exportRows {
		row := []string{er.AssetTag, er.Type, er.Brand, er.Model, er.Serial, er.CPU, er.Memory, er.Disk, er.Status, er.PurchaseDate, fmt.Sprintf("%.2f", er.PurchasePrice), er.Supplier, er.WarrantyEnd, er.CurrentUser, er.Location, er.Notes, er.CreatedAt}
		row = append(row, er.CustomVals...)
		writer.Write(row)
	}
	writer.Flush()
}

// ─── API: Transactions ───

func handleTransactions(w http.ResponseWriter, r *http.Request) {
	pageStr := r.URL.Query().Get("page")
	page, _ := strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}
	pageSizeStr := r.URL.Query().Get("pageSize")
	pageSize, _ := strconv.Atoi(pageSizeStr)
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	var total int
	db.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&total)

	rows, err := db.Query("SELECT id, asset_id, action, operator, target_user, notes, created_at, asset_tag FROM transactions ORDER BY id DESC LIMIT ? OFFSET ?", pageSize, offset)
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	txs := []Transaction{}
	for rows.Next() {
		var t Transaction
		rows.Scan(&t.ID, &t.AssetID, &t.Action, &t.Operator, &t.TargetUser, &t.Notes, &t.CreatedAt, &t.AssetTag)
		txs = append(txs, t)
	}

	totalPages := int(math.Ceil(float64(total) / float64(pageSize)))
	jsonResp(w, map[string]interface{}{
		"transactions": txs,
		"total":        total,
		"totalPages":   totalPages,
		"page":         page,
		"pageSize":     pageSize,
	})
}

// ─── API: Custom Fields ───

func handleCustomFields(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		listCustomFields(w)
	case "POST":
		createCustomField(w, r)
	case "DELETE":
		deleteCustomField(w, r)
	default:
		http.Error(w, "Method not allowed", 405)
	}
}

func listCustomFields(w http.ResponseWriter) {
	rows, err := db.Query("SELECT id, field_name, field_type, field_options, sort_order FROM custom_fields ORDER BY sort_order, id")
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	fields := []CustomField{}
	for rows.Next() {
		var f CustomField
		rows.Scan(&f.ID, &f.FieldName, &f.FieldType, &f.Options, &f.SortOrder)
		fields = append(fields, f)
	}
	jsonResp(w, fields)
}

func createCustomField(w http.ResponseWriter, r *http.Request) {
	var f CustomField
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		jsonErr(w, "Invalid JSON", 400)
		return
	}
	if f.FieldName == "" {
		jsonErr(w, "Field name required", 400)
		return
	}
	if f.FieldType == "" {
		f.FieldType = "text"
	}

	_, err := db.Exec("INSERT INTO custom_fields (field_name, field_type, field_options, sort_order) VALUES (?,?,?,?)",
		f.FieldName, f.FieldType, f.Options, f.SortOrder)
	if err != nil {
		jsonErr(w, err.Error(), 400)
		return
	}

	jsonResp(w, map[string]string{"status": "ok"})
}

func deleteCustomField(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		jsonErr(w, "Invalid ID", 400)
		return
	}
	db.Exec("DELETE FROM custom_fields WHERE id=?", id)
	db.Exec("DELETE FROM custom_field_values WHERE field_id=?", id)
	jsonResp(w, map[string]string{"status": "ok"})
}

// ─── API: Stats ───

func handleStats(w http.ResponseWriter, r *http.Request) {
	var total, inStock, checkedOut, scrapped int
	db.QueryRow("SELECT COUNT(*) FROM assets WHERE status != '已报废'").Scan(&total)
	db.QueryRow("SELECT COUNT(*) FROM assets WHERE status='在库'").Scan(&inStock)
	db.QueryRow("SELECT COUNT(*) FROM assets WHERE status='已领用'").Scan(&checkedOut)
	db.QueryRow("SELECT COUNT(*) FROM scrapped_assets").Scan(&scrapped)

	var laptopCount, desktopCount int
	db.QueryRow("SELECT COUNT(*) FROM assets WHERE type='laptop' OR type='笔记本' OR type='笔记本电脑'").Scan(&laptopCount)
	db.QueryRow("SELECT COUNT(*) FROM assets WHERE type='desktop' OR type='台式机' OR type='台式电脑'").Scan(&desktopCount)

	jsonResp(w, map[string]interface{}{
		"total":        total,
		"inStock":      inStock,
		"checkedOut":   checkedOut,
		"scrapped":     scrapped,
		"laptopCount":  laptopCount,
		"desktopCount": desktopCount,
	})
}

// ─── Scrapped Assets Page Handlers ───

func handleScrappedPage(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	render(w, "templates/scrapped.html", map[string]interface{}{
		"Page":     "scrapped",
		"PageName": "报废资产",
		"User":     s.UserName,
		"IsAdmin":  s.Role == "admin",
	})
}

func handleFinancePage(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	render(w, "templates/finance.html", map[string]interface{}{
		"Page":     "finance",
		"PageName": "财务管理",
		"User":     s.UserName,
		"IsAdmin":  s.Role == "admin",
	})
}

func calcDepreciation(price float64, pdate string, method string, now time.Time) (monthlyDepr float64, totalDepr float64, deprMonths int, curValue float64) {
	if price <= 0 {
		return 0, 0, 0, 0
	}
	var pastMonths int
	if pdate != "" {
		pt, err := time.Parse("2006-01-02", pdate[:10])
		if err == nil {
			pastMonths = int(now.Sub(pt).Hours() / (30.44 * 24))
			if pastMonths < 0 {
				pastMonths = 0
			}
		}
	}
	switch method {
	case "straight-3y":
		deprMonths = pastMonths
		if deprMonths > 36 { deprMonths = 36 }
		monthlyDepr = price / 36.0
	case "straight-10y":
		deprMonths = pastMonths
		if deprMonths > 120 { deprMonths = 120 }
		monthlyDepr = price / 120.0
	case "dbl-declining-5y":
		yearlyRate := 2.0 / 5.0
		years := pastMonths / 12
		deprMonths = pastMonths
		if years >= 3 {
			residual := price
			for i := 0; i < 3; i++ {
				residual *= (1.0 - yearlyRate)
			}
			monthlyDepr = residual / 24.0
			remaining := pastMonths - 36
			if remaining < 0 { remaining = 0 }
			totalDepr = price - residual + monthlyDepr*float64(remaining)
			if totalDepr > price { totalDepr = price }
			curValue = price - totalDepr
			if curValue < 0 { curValue = 0 }
			return
		}
		monthlyDepr = price * yearlyRate / 12.0
	case "sum-years-5y":
		deprMonths = pastMonths
		if deprMonths > 60 { deprMonths = 60 }
		years := pastMonths / 12
		if years > 4 { years = 4 }
		yearlyRate := float64(5-years) / 15.0
		monthlyDepr = price * yearlyRate / 12.0
	default: // straight-5y
		deprMonths = pastMonths
		if deprMonths > 60 { deprMonths = 60 }
		monthlyDepr = price / 60.0
	}
	if deprMonths < 0 { deprMonths = 0 }
	totalDepr = monthlyDepr * float64(deprMonths)
	if totalDepr > price { totalDepr = price }
	curValue = price - totalDepr
	if curValue < 0 { curValue = 0 }
	return
}

func handleFinanceSummary(w http.ResponseWriter, r *http.Request) {
	cfg := loadConfig()
	method := cfg.DepreciationMethod
	if method == "" {
		method = "straight-5y"
	}
	type FinanceItem struct {
		Tag            string  `json:"asset_tag"`
		Type           string  `json:"type"`
		Brand          string  `json:"brand"`
		Model          string  `json:"model"`
		Serial         string  `json:"serial"`
		Status         string  `json:"status"`
		PurchasePrice  float64 `json:"purchase_price"`
		PurchaseDate   string  `json:"purchase_date"`
		CurrentValue   float64 `json:"current_value"`
		MonthlyDepr    float64 `json:"monthly_depreciation"`
		TotalDepr      float64 `json:"total_depreciation"`
		DeprMonths     int     `json:"depreciation_months"`
	}
	rows, err := db.Query(`SELECT id, asset_tag, type, brand, model, serial, status, purchase_price, purchase_date FROM assets WHERE purchase_price > 0 ORDER BY type, asset_tag`)
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	typeByGroup := make(map[string]float64)
	items := []FinanceItem{}
	now := time.Now()
	for rows.Next() {
		var id int
		var tag, typ, brand, model, serial, status, pdate string
		var price float64
		rows.Scan(&id, &tag, &typ, &brand, &model, &serial, &status, &price, &pdate)
		monthlyDepr, totalDepr, deprMonths, curVal := calcDepreciation(price, pdate, method, now)
		typeByGroup[typ] += price
		items = append(items, FinanceItem{
			Tag: tag, Type: typ, Brand: brand, Model: model, Serial: serial,
			Status: status, PurchasePrice: price, PurchaseDate: pdate,
			CurrentValue: curVal, MonthlyDepr: monthlyDepr, TotalDepr: totalDepr, DeprMonths: deprMonths,
		})
	}
	var totalPrice, netTotal float64
	for _, v := range typeByGroup {
		totalPrice += v
	}
	for _, it := range items {
		netTotal += it.CurrentValue
	}
	jsonResp(w, map[string]interface{}{
		"items": items, "type_groups": typeByGroup,
		"total_price": totalPrice, "net_total": netTotal,
		"item_count": len(items), "method": method,
	})
}
func handleScrappedAssetDetailPage(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/scrapped/asset/")
	idStr = strings.TrimSuffix(idStr, "/")
	if idStr == "" {
		http.Redirect(w, r, "/scrapped", http.StatusFound)
		return
	}
	s := getSession(r)
	render(w, "templates/scrapped_asset_detail.html", map[string]interface{}{
		"Page":     "scrapped_asset_detail",
		"PageName": "报废资产详情",
		"AssetID":  idStr,
		"User":     s.UserName,
		"IsAdmin":  s.Role == "admin",
	})
}

// ─── API: Scrapped Assets ───

func handleScrapped(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		listScrapped(w, r)
	} else {
		http.Error(w, "Method not allowed", 405)
	}
}

func listScrapped(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	assetType := r.URL.Query().Get("type")
	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("pageSize")

	page, _ := strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(pageSizeStr)
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}

	where := []string{"1=1"}
	args := []interface{}{}

	if search != "" {
		where = append(where, "(asset_tag LIKE ? OR brand LIKE ? OR model LIKE ? OR serial LIKE ? OR scrap_reason LIKE ? OR scrap_notes LIKE ?)")
		s := "%" + search + "%"
		args = append(args, s, s, s, s, s, s)
	}
	if assetType != "" {
		where = append(where, "type = ?")
		args = append(args, assetType)
	}

	whereClause := strings.Join(where, " AND ")

	// Count
	var total int
	countQuery := "SELECT COUNT(*) FROM scrapped_assets WHERE " + whereClause
	db.QueryRow(countQuery, args...).Scan(&total)

	offset := (page - 1) * pageSize
	query := fmt.Sprintf("SELECT id, asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes, scrap_reason, scrap_notes, scrapped_by, scrapped_at, restored_at, created_at, updated_at FROM scrapped_assets WHERE %s ORDER BY scrapped_at DESC LIMIT ? OFFSET ?", whereClause)
	args = append(args, pageSize, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	assets := []ScrappedAsset{}
	for rows.Next() {
		var a ScrappedAsset
		rows.Scan(&a.ID, &a.AssetTag, &a.Type, &a.Brand, &a.Model, &a.Serial, &a.CPU, &a.Memory, &a.Disk, &a.Status, &a.PurchaseDate, &a.PurchasePrice, &a.Supplier, &a.WarrantyEnd, &a.CurrentUser, &a.Location, &a.Notes, &a.ScrapReason, &a.ScrapNotes, &a.ScrappedBy, &a.ScrappedAt, &a.RestoredAt, &a.CreatedAt, &a.UpdatedAt)
		assets = append(assets, a)
	}

	totalPages := int(math.Ceil(float64(total) / float64(pageSize)))
	jsonResp(w, map[string]interface{}{
		"assets":     assets,
		"total":      total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
	})
}

func handleScrappedByID(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/scrapped/")
	idStr = strings.TrimSuffix(idStr, "/")

	// Handle restore sub-route
	if strings.HasSuffix(idStr, "/restore") {
		scrappedRestore(w, r)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		jsonErr(w, "Invalid ID", 400)
		return
	}

	switch r.Method {
	case "GET":
		getScrapped(w, id)
	case "PUT":
		updateScrapped(w, r, id)
	default:
		http.Error(w, "Method not allowed", 405)
	}
}

func getScrapped(w http.ResponseWriter, id int) {
	var a ScrappedAsset
	err := db.QueryRow(`SELECT id, asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes, scrap_reason, scrap_notes, scrapped_by, scrapped_at, restored_at, created_at, updated_at FROM scrapped_assets WHERE id=?`, id).
		Scan(&a.ID, &a.AssetTag, &a.Type, &a.Brand, &a.Model, &a.Serial, &a.CPU, &a.Memory, &a.Disk, &a.Status, &a.PurchaseDate, &a.PurchasePrice, &a.Supplier, &a.WarrantyEnd, &a.CurrentUser, &a.Location, &a.Notes, &a.ScrapReason, &a.ScrapNotes, &a.ScrappedBy, &a.ScrappedAt, &a.RestoredAt, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		jsonErr(w, "Scrapped asset not found", 404)
		return
	}
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}
	jsonResp(w, a)
}

func updateScrapped(w http.ResponseWriter, r *http.Request, id int) {
	var body struct {
		Notes      string `json:"notes"`
		ScrapNotes string `json:"scrap_notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, "Invalid JSON: "+err.Error(), 400)
		return
	}

	if body.Notes != "" {
		db.Exec("UPDATE scrapped_assets SET notes=?, updated_at=datetime('now','localtime') WHERE id=?", body.Notes, id)
	}
	if body.ScrapNotes != "" {
		db.Exec("UPDATE scrapped_assets SET scrap_notes=?, updated_at=datetime('now','localtime') WHERE id=?", body.ScrapNotes, id)
	}

	jsonResp(w, map[string]string{"status": "ok"})
}

func scrappedRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonErr(w, "POST required", 405)
		return
	}

	// Parse ID from path
	idStr := strings.TrimPrefix(r.URL.Path, "/api/scrapped/")
	idStr = strings.TrimSuffix(idStr, "/restore")
	idStr = strings.TrimSuffix(idStr, "/")

	id, err := strconv.Atoi(idStr)
	if err != nil {
		jsonErr(w, "Invalid ID", 400)
		return
	}

	var body struct {
		Reason   string `json:"reason"`
		Notes    string `json:"notes"`
		Operator string `json:"operator"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if body.Operator == "" {
		body.Operator = operatorName(r)
	}

	tx, err := db.Begin()
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}
	defer tx.Rollback()

	// Read scrapped asset
	var a ScrappedAsset
	err = tx.QueryRow(`SELECT id, asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes, scrap_reason, scrap_notes, scrapped_by, scrapped_at, restored_at, created_at, updated_at FROM scrapped_assets WHERE id=?`, id).
		Scan(&a.ID, &a.AssetTag, &a.Type, &a.Brand, &a.Model, &a.Serial, &a.CPU, &a.Memory, &a.Disk, &a.Status, &a.PurchaseDate, &a.PurchasePrice, &a.Supplier, &a.WarrantyEnd, &a.CurrentUser, &a.Location, &a.Notes, &a.ScrapReason, &a.ScrapNotes, &a.ScrappedBy, &a.ScrappedAt, &a.RestoredAt, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		jsonErr(w, "Scrapped asset not found", 404)
		return
	}

	// Insert back into assets with status = '在库'
	res, err := tx.Exec(`INSERT INTO assets (asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.AssetTag, a.Type, a.Brand, a.Model, a.Serial, a.CPU, a.Memory, a.Disk, "在库",
		a.PurchaseDate, a.PurchasePrice, a.Supplier, a.WarrantyEnd, a.CurrentUser, a.Location, a.Notes,
		a.CreatedAt, time.Now().Format("2006-01-02 15:04:05"))
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}
	newAssetID, _ := res.LastInsertId()

	// Delete from scrapped_assets
	_, err = tx.Exec("DELETE FROM scrapped_assets WHERE id=?", id)
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}

	// Log transaction
	restoreNotes := body.Notes
	if body.Reason != "" {
		restoreNotes = "复活原因: " + body.Reason + "; " + restoreNotes
	}
	tx.Exec("INSERT INTO transactions (asset_id, action, operator, notes, asset_tag) VALUES (?, 'restore', ?, ?, ?)",
		newAssetID, body.Operator, restoreNotes, a.AssetTag)

	if err := tx.Commit(); err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}

	jsonResp(w, map[string]string{"status": "ok", "new_status": "在库"})
}

// ─── API: Field Presets ───

func handleFieldPresets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		fieldKey := r.URL.Query().Get("field_key")
		if fieldKey == "" {
			jsonErr(w, "field_key required", 400)
			return
		}
		rows, err := db.Query("SELECT id, field_key, field_value, sort_order, COALESCE(abbr,'') FROM field_presets WHERE field_key=? ORDER BY sort_order, id", fieldKey)
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		type Preset struct {
			ID         int    `json:"id"`
			FieldKey   string `json:"field_key"`
			FieldValue string `json:"field_value"`
			SortOrder  int    `json:"sort_order"`
			Abbr       string `json:"abbr"`
		}
		presets := []Preset{}
		for rows.Next() {
			var p Preset
			rows.Scan(&p.ID, &p.FieldKey, &p.FieldValue, &p.SortOrder, &p.Abbr)
			presets = append(presets, p)
		}
		jsonResp(w, presets)

	case "POST":
		var body struct {
			FieldKey   string `json:"field_key"`
			FieldValue string `json:"field_value"`
			Abbr       string `json:"abbr"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonErr(w, "Invalid JSON", 400)
			return
		}
		if body.FieldKey == "" || body.FieldValue == "" {
			jsonErr(w, "field_key and field_value required", 400)
			return
		}
		_, err := db.Exec("INSERT INTO field_presets (field_key, field_value, abbr) VALUES (?,?,?)", body.FieldKey, body.FieldValue, body.Abbr)
		if err != nil {
			jsonErr(w, err.Error(), 400)
			return
		}
		jsonResp(w, map[string]string{"status": "ok"})

	case "PUT":
		var body struct {
			ID         int    `json:"id"`
			FieldValue string `json:"field_value"`
			Abbr       string `json:"abbr"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonErr(w, "Invalid JSON", 400)
			return
		}
		if body.ID == 0 {
			jsonErr(w, "id required", 400)
			return
		}
		_, err := db.Exec("UPDATE field_presets SET field_value=?, abbr=? WHERE id=?", body.FieldValue, body.Abbr, body.ID)
		if err != nil {
			jsonErr(w, err.Error(), 400)
			return
		}
		jsonResp(w, map[string]string{"status": "ok"})

	case "DELETE":
		idStr := r.URL.Query().Get("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			jsonErr(w, "Invalid ID", 400)
			return
		}
		db.Exec("DELETE FROM field_presets WHERE id=?", id)
		jsonResp(w, map[string]string{"status": "ok"})

	default:
		http.Error(w, "Method not allowed", 405)
	}
}

// ─── API: Default Fields ───

func handleDefaultFields(w http.ResponseWriter, r *http.Request) {
	defaultFields := []map[string]interface{}{
		{"key": "asset_tag", "name": "资产编号", "type": "text"},
		{"key": "type", "name": "类型", "type": "select"},
		{"key": "brand", "name": "品牌", "type": "select"},
		{"key": "model", "name": "型号", "type": "text"},
		{"key": "serial", "name": "序列号", "type": "text"},
		{"key": "cpu", "name": "CPU", "type": "text"},
		{"key": "memory", "name": "内存", "type": "text"},
		{"key": "disk", "name": "硬盘", "type": "text"},
		{"key": "status", "name": "状态", "type": "select"},
		{"key": "supplier", "name": "供应商", "type": "text"},
		{"key": "current_user", "name": "使用人", "type": "text"},
		{"key": "location", "name": "位置", "type": "select"},
		{"key": "purchase_date", "name": "采购日期", "type": "date"},
		{"key": "warranty_years", "name": "保修年限", "type": "select"},
	}
	// For select fields, query presets
	for i, f := range defaultFields {
		if f["type"] == "select" {
			rows, err := db.Query("SELECT field_value FROM field_presets WHERE field_key=? ORDER BY sort_order, id", f["key"])
			if err == nil {
				var vals []string
				for rows.Next() {
					var v string
					rows.Scan(&v)
					vals = append(vals, v)
				}
				rows.Close()
				defaultFields[i]["presets"] = vals
			} else {
				defaultFields[i]["presets"] = []string{}
			}
		}
	}
	jsonResp(w, defaultFields)
}

// ─── Helpers ───

// ─── API: Users ───

func handleUsers(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	// Need admin for user management
	if r.Method == "POST" || r.Method == "DELETE" || r.Method == "PUT" {
		if s == nil || s.Role != "admin" {
			jsonErr(w, "Forbidden", 403)
			return
		}
	}

	if r.Method == "GET" {
		listUsers(w, r)
	} else if r.Method == "POST" {
		createUser(w, r, s)
	} else {
		http.Error(w, "Method not allowed", 405)
	}
}

func listUsers(w http.ResponseWriter, r *http.Request) {
	activeFilter := r.URL.Query().Get("active")
	var rows *sql.Rows
	var err error
	if activeFilter == "1" {
		rows, err = db.Query("SELECT id, name, department, phone, email, password, role, notes, active, created_at FROM users WHERE active=1 ORDER BY name")
	} else {
		rows, err = db.Query("SELECT id, name, department, phone, email, password, role, notes, active, created_at FROM users ORDER BY name")
	}
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		var u User
		rows.Scan(&u.ID, &u.Name, &u.Department, &u.Phone, &u.Email, &u.Password, &u.Role, &u.Notes, &u.Active, &u.CreatedAt)
		u.Password = "" // never expose password hash
		users = append(users, u)
	}
	jsonResp(w, users)
}

func createUser(w http.ResponseWriter, r *http.Request, s *Session) {
	var req struct {
		Name       string `json:"name"`
		Department string `json:"department"`
		Phone      string `json:"phone"`
		Email      string `json:"email"`
		Password   string `json:"password"`
		Role       string `json:"role"`
		Notes      string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "Invalid JSON: "+err.Error(), 400)
		return
	}
	if req.Name == "" {
		jsonErr(w, "Name is required", 400)
		return
	}
	if req.Password == "" {
		req.Password = "123"
	}
	pwdHash := hashPassword(req.Password)

	role := "user"
	if req.Role == "admin" && s != nil && s.Role == "admin" {
		role = "admin"
	}

	result, err := db.Exec("INSERT INTO users (name, department, phone, email, password, role, notes) VALUES (?,?,?,?,?,?,?)",
		req.Name, req.Department, req.Phone, req.Email, pwdHash, role, req.Notes)
	if err != nil {
		jsonErr(w, err.Error(), 400)
		return
	}

	id, _ := result.LastInsertId()
	jsonResp(w, User{
		ID:         int(id),
		Name:       req.Name,
		Department: req.Department,
		Phone:      req.Phone,
		Email:      req.Email,
		Role:       role,
		Notes:      req.Notes,
		Active:     1,
	})
}

func handleUserByID(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/users/")
	idStr = strings.TrimSuffix(idStr, "/")
	if idStr == "" {
		jsonErr(w, "Invalid ID", 400)
		return
	}

	// Batch action: /api/users/batch
	if idStr == "batch" {
		handleUserBatch(w, r)
		return
	}

	// Sub-route: /api/users/{id}/assets - list assets checked out to this user
	if strings.HasSuffix(idStr, "/assets") {
		uidStr := strings.TrimSuffix(idStr, "/assets")
		uidStr = strings.TrimSuffix(uidStr, "/")
		uid, err := strconv.Atoi(uidStr)
		if err != nil {
			jsonErr(w, "Invalid ID", 400)
			return
		}
		handleUserAssets(w, r, uid)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		jsonErr(w, "Invalid ID", 400)
		return
	}

	s := getSession(r)

	switch r.Method {
	case "GET":
		if s.Role != "admin" && s.UserID != id {
			jsonErr(w, "Forbidden", 403)
			return
		}
		getUser(w, id)
	case "PUT":
		if s.Role != "admin" && s.UserID != id {
			jsonErr(w, "Forbidden", 403)
			return
		}
		updateUser(w, r, id, s)
	case "DELETE":
		if s.Role != "admin" {
			jsonErr(w, "Forbidden", 403)
			return
		}
		deleteUser(w, id)
	default:
		http.Error(w, "Method not allowed", 405)
	}
}

func getUser(w http.ResponseWriter, id int) {
	var u User
	err := db.QueryRow("SELECT id, name, department, phone, email, password, role, notes, active, created_at FROM users WHERE id=?", id).
		Scan(&u.ID, &u.Name, &u.Department, &u.Phone, &u.Email, &u.Password, &u.Role, &u.Notes, &u.Active, &u.CreatedAt)
	if err == sql.ErrNoRows {
		jsonErr(w, "User not found", 404)
		return
	}
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}
	u.Password = ""
	jsonResp(w, u)
}

func updateUser(w http.ResponseWriter, r *http.Request, id int, s *Session) {
	var req struct {
		Name       string `json:"name"`
		Department string `json:"department"`
		Phone      string `json:"phone"`
		Email      string `json:"email"`
		Password   string `json:"password"`
		Role       string `json:"role"`
		Notes      string `json:"notes"`
		Active     int    `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "Invalid JSON: "+err.Error(), 400)
		return
	}
	if req.Name == "" {
		jsonErr(w, "Name is required", 400)
		return
	}

	if s.Role == "admin" {
		// Admin can update all fields including role
		// If role is empty, preserve existing role
		currentRole := req.Role
		if currentRole == "" {
			var existingRole string
			db.QueryRow("SELECT role FROM users WHERE id=?", id).Scan(&existingRole)
			currentRole = existingRole
		}
		if req.Password != "" {
			pwdHash := hashPassword(req.Password)
			_, err := db.Exec("UPDATE users SET name=?, department=?, phone=?, email=?, password=?, role=?, notes=?, active=? WHERE id=?",
				req.Name, req.Department, req.Phone, req.Email, pwdHash, currentRole, req.Notes, req.Active, id)
			if err != nil {
				jsonErr(w, err.Error(), 400)
				return
			}
		} else {
			_, err := db.Exec("UPDATE users SET name=?, department=?, phone=?, email=?, role=?, notes=?, active=? WHERE id=?",
				req.Name, req.Department, req.Phone, req.Email, currentRole, req.Notes, req.Active, id)
			if err != nil {
				jsonErr(w, err.Error(), 400)
				return
			}
		}
	} else {
		// Non-admin can only update their own basic info and password
		if req.Password != "" {
			pwdHash := hashPassword(req.Password)
			_, err := db.Exec("UPDATE users SET name=?, department=?, phone=?, email=?, password=? WHERE id=?",
				req.Name, req.Department, req.Phone, req.Email, pwdHash, id)
			if err != nil {
				jsonErr(w, err.Error(), 400)
				return
			}
		} else {
			_, err := db.Exec("UPDATE users SET name=?, department=?, phone=?, email=? WHERE id=?",
				req.Name, req.Department, req.Phone, req.Email, id)
			if err != nil {
				jsonErr(w, err.Error(), 400)
				return
			}
		}
	}
	jsonResp(w, map[string]string{"status": "ok"})
}

func deleteUser(w http.ResponseWriter, id int) {
	// Prevent deleting admin
	var name string
	db.QueryRow("SELECT name FROM users WHERE id=?", id).Scan(&name)
	if name == "admin" {
		jsonErr(w, "不能删除管理员账户", 400)
		return
	}
	_, err := db.Exec("DELETE FROM users WHERE id=?", id)
	if err != nil {
		jsonErr(w, err.Error(), 400)
		return
	}
	jsonResp(w, map[string]string{"status": "ok"})
}

// ─── API: Batch User Actions ───

func handleUserBatch(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil || s.Role != "admin" {
		jsonErr(w, "Forbidden", 403)
		return
	}
	if r.Method != "POST" {
		jsonErr(w, "POST required", 405)
		return
	}

	var body struct {
		IDs    []int  `json:"ids"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, "Invalid JSON", 400)
		return
	}
	if len(body.IDs) == 0 {
		jsonErr(w, "ids required", 400)
		return
	}

	// Prevent operating on admin user
	var adminID int
	db.QueryRow("SELECT id FROM users WHERE name='admin'").Scan(&adminID)

	switch body.Action {
	case "enable":
		for _, id := range body.IDs {
			if id == adminID {
				continue
			}
			db.Exec("UPDATE users SET active=1 WHERE id=?", id)
		}
		jsonResp(w, map[string]string{"status": "ok"})
	case "disable":
		for _, id := range body.IDs {
			if id == adminID {
				continue
			}
			db.Exec("UPDATE users SET active=0 WHERE id=?", id)
		}
		jsonResp(w, map[string]string{"status": "ok"})
	case "delete":
		for _, id := range body.IDs {
			if id == adminID {
				continue
			}
			db.Exec("DELETE FROM users WHERE id=?", id)
		}
		jsonResp(w, map[string]string{"status": "ok"})
	default:
		jsonErr(w, "Invalid action: "+body.Action, 400)
	}
}

// ─── Config ───

type AppConfig struct {
	CompanyName    string             `json:"company_name"`
	TaxRate        float64            `json:"tax_rate"`        // 默认税率（百分比，如13表示13%）
	BaseCurrency   string             `json:"base_currency"`  // 基准货币（默认 RMB）
	ExchangeRates  map[string]float64 `json:"exchange_rates"` // 汇率表 e.g. {"USD":0.14}
	DepreciationMethod string        `json:"depreciation_method"` // 折旧方法: "straight-5y", "straight-3y", "straight-10y", "dbl-declining-5y", "sum-years-5y"
}

func loadConfig() AppConfig {
	cfg := AppConfig{
		CompanyName:    "PC",
		TaxRate:        0,
		BaseCurrency:   "RMB",
		ExchangeRates:   map[string]float64{},
		DepreciationMethod: "straight-5y",
	}
	cfgPath := filepath.Join(appDataDir, "config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return cfg
	}
	json.Unmarshal(data, &cfg)
	if cfg.CompanyName == "" {
		cfg.CompanyName = "PC"
	}
	if cfg.BaseCurrency == "" {
		cfg.BaseCurrency = "RMB"
	}
	if cfg.ExchangeRates == nil {
		cfg.ExchangeRates = map[string]float64{}
	}
	return cfg
}

func saveConfig(cfg AppConfig) error {
	cfgPath := filepath.Join(appDataDir, "config.json")
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(cfgPath, data, 0644)
}

func handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		cfg := loadConfig()
		jsonResp(w, cfg)
	case "PUT":
		s := getSession(r)
		if s.Role != "admin" {
			jsonErr(w, "Forbidden", 403)
			return
		}
		var cfg AppConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			jsonErr(w, "Invalid JSON", 400)
			return
		}
		if err := saveConfig(cfg); err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		jsonResp(w, cfg)
	default:
		http.Error(w, "Method not allowed", 405)
	}
}

func handleUserAssets(w http.ResponseWriter, r *http.Request, uid int) {
	if r.Method != "GET" {
		jsonErr(w, "Method not allowed", 405)
		return
	}

	var userName string
	err := db.QueryRow("SELECT name FROM users WHERE id=?", uid).Scan(&userName)
	if err != nil {
		jsonErr(w, "User not found", 404)
		return
	}

	// Get assets with their most recent checkout time
	rows, err := db.Query(`
		SELECT a.id, a.asset_tag, a.type, a.brand, a.model, a.serial, a.status, a.current_user, a.location,
			(SELECT MAX(t.created_at) FROM transactions t WHERE t.asset_id = a.id AND (t.action = 'checkout' OR t.action = '领用')) AS checkout_time
		FROM assets a
		WHERE a.current_user=? AND a.status='已领用'
		ORDER BY a.asset_tag`, userName)
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	type UserAsset struct {
		ID           int    `json:"id"`
		Tag          string `json:"asset_tag"`
		Type         string `json:"type"`
		Brand        string `json:"brand"`
		Model        string `json:"model"`
		Serial       string `json:"serial"`
		Status       string `json:"status"`
		Location     string `json:"location"`
		CheckoutTime string `json:"checkout_time"`
	}

	var assets []UserAsset
	for rows.Next() {
		var a UserAsset
		var curUser string
		var checkTime sql.NullString
		rows.Scan(&a.ID, &a.Tag, &a.Type, &a.Brand, &a.Model, &a.Serial, &a.Status, &curUser, &a.Location, &checkTime)
		if checkTime.Valid {
			a.CheckoutTime = checkTime.String
		}
		assets = append(assets, a)
	}

	json.NewEncoder(w).Encode(assets)
}

func operatorName(r *http.Request) string {
	s := getSession(r)
	if s != nil {
		return s.UserName
	}
	name := r.URL.Query().Get("operator")
	if name == "" {
		name = "admin"
	}
	return name
}

// ─── XML Export / Import ───

type XMLRate struct {
	XMLName xml.Name `xml:"rate"`
	Code    string   `xml:"code,attr"`
	Value   float64  `xml:",chardata"`
}

type XMLConfig struct {
	XMLName            xml.Name `xml:"config"`
	CompanyName        string    `xml:"company-name,omitempty"`
	TaxRate            float64   `xml:"tax-rate,omitempty"`
	BaseCurrency       string    `xml:"base-currency,omitempty"`
	DepreciationMethod string    `xml:"depreciation-method,omitempty"`
	ExchangeRates      []XMLRate `xml:"exchange-rates>rate,omitempty"`
}

type XMLExport struct {
	XMLName        xml.Name               `xml:"l-asset"`
	Version        string                 `xml:"version,attr"`
	ExportedAt     string                 `xml:"exported-at,attr"`
	Config         XMLConfig              `xml:"config"`
	CustomFields   []CustomField          `xml:"custom-fields>custom-field"`
	Users          []User                 `xml:"users>user"`
	Assets         []XMLAsset             `xml:"assets>asset"`
	ScrappedAssets []XMLScrappedAsset    `xml:"scrapped-assets>scrapped-asset"`
	Attachments    []Attachment           `xml:"attachments>attachment"`
	Transactions   []Transaction          `xml:"transactions>transaction"`
}

type XMLScrappedAsset struct {
	ScrappedAsset
	CustomValues []XMLCustomValue `xml:"custom-values>custom-value,omitempty"`
}

type XMLAttachment struct {
	ID        int    `xml:"id"`
	AssetID   int    `xml:"asset-id"`
	FileName  string `xml:"file-name"`
	FilePath  string `xml:"file-path"`
	FileSize  int64  `xml:"file-size"`
	MimeType  string `xml:"mime-type"`
	CreatedAt string `xml:"created-at"`
}


type XMLAsset struct {
	Asset
	CustomValues []XMLCustomValue `xml:"custom-values>custom-value,omitempty"`
}

type XMLCustomValue struct {
	FieldID    int    `xml:"field-id"`
	FieldName  string `xml:"field-name"`
	FieldValue string `xml:"field-value"`
}

func handleExportXML(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil || s.Role != "admin" {
		jsonErr(w, "Forbidden", 403)
		return
	}

	xmlData, err := exportXML()
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=l-asset-backup.xml")
	w.Write([]byte(xml.Header))
	w.Write(xmlData)
}

func exportXML() ([]byte, error) {
	cfg := loadConfig()
	xc := XMLConfig{
		CompanyName:        cfg.CompanyName,
		TaxRate:            cfg.TaxRate,
		BaseCurrency:       cfg.BaseCurrency,
		DepreciationMethod: cfg.DepreciationMethod,
	}
	for code, val := range cfg.ExchangeRates {
		xc.ExchangeRates = append(xc.ExchangeRates, XMLRate{Code: code, Value: val})
	}
	x := XMLExport{
		Version:    "1.0",
		ExportedAt: time.Now().Format("2006-01-02 15:04:05"),
		Config:     xc,
	}

	// Relax max open conns temporarily so nested queries don't deadlock
	db.SetMaxOpenConns(3)
	defer db.SetMaxOpenConns(1)

	// Custom fields
	{
		rows, err := db.Query("SELECT id, field_name, field_type, field_options, sort_order FROM custom_fields ORDER BY sort_order, id")
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var f CustomField
			rows.Scan(&f.ID, &f.FieldName, &f.FieldType, &f.Options, &f.SortOrder)
			x.CustomFields = append(x.CustomFields, f)
		}
		rows.Close()
	}

	// Users (include password hash for proper restore - reset if needed after import)
	{
		rows, err := db.Query("SELECT id, name, department, phone, email, coalesce(password,''), role, notes, active, created_at FROM users ORDER BY id")
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var u User
			rows.Scan(&u.ID, &u.Name, &u.Department, &u.Phone, &u.Email, &u.Password, &u.Role, &u.Notes, &u.Active, &u.CreatedAt)
			x.Users = append(x.Users, u)
		}
		rows.Close()
	}

	// Assets with custom values (phase 1: load all assets into memory, close rows)
	var basicAssets []Asset
	{
		rows, err := db.Query("SELECT id, asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes, created_at, updated_at FROM assets WHERE status != '已报废' ORDER BY id")
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var a Asset
			rows.Scan(&a.ID, &a.AssetTag, &a.Type, &a.Brand, &a.Model, &a.Serial, &a.CPU, &a.Memory, &a.Disk, &a.Status, &a.PurchaseDate, &a.PurchasePrice, &a.Supplier, &a.WarrantyEnd, &a.CurrentUser, &a.Location, &a.Notes, &a.CreatedAt, &a.UpdatedAt)
			basicAssets = append(basicAssets, a)
		}
		rows.Close()
	}
	// Phase 2: lookup custom values for each asset (no concurrent rows)
	for _, a := range basicAssets {
		xa := XMLAsset{Asset: a}
		cvRows, err := db.Query(`SELECT cf.id, cf.field_name, cv.field_value FROM custom_field_values cv JOIN custom_fields cf ON cv.field_id=cf.id WHERE cv.asset_id=?`, a.ID)
		if err == nil {
			for cvRows.Next() {
				var cv XMLCustomValue
				cvRows.Scan(&cv.FieldID, &cv.FieldName, &cv.FieldValue)
				xa.CustomValues = append(xa.CustomValues, cv)
			}
			cvRows.Close()
		}
		x.Assets = append(x.Assets, xa)
	}

	// Scrapped assets
	{
		scaRows, err := db.Query("SELECT id, asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes, scrap_reason, scrap_notes, scrapped_by, scrapped_at, restored_at, created_at, updated_at FROM scrapped_assets ORDER BY id")
		if err == nil {
			for scaRows.Next() {
				var sa ScrappedAsset
				scaRows.Scan(&sa.ID, &sa.AssetTag, &sa.Type, &sa.Brand, &sa.Model, &sa.Serial, &sa.CPU, &sa.Memory, &sa.Disk, &sa.Status, &sa.PurchaseDate, &sa.PurchasePrice, &sa.Supplier, &sa.WarrantyEnd, &sa.CurrentUser, &sa.Location, &sa.Notes, &sa.ScrapReason, &sa.ScrapNotes, &sa.ScrappedBy, &sa.ScrappedAt, &sa.RestoredAt, &sa.CreatedAt, &sa.UpdatedAt)
				xsa := XMLScrappedAsset{ScrappedAsset: sa}
				x.ScrappedAssets = append(x.ScrappedAssets, xsa)
			}
			scaRows.Close()
		}
	}

	// Transactions
	{
		rows, err := db.Query("SELECT id, asset_id, action, operator, target_user, notes, created_at, asset_tag FROM transactions ORDER BY id")
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var t Transaction
			rows.Scan(&t.ID, &t.AssetID, &t.Action, &t.Operator, &t.TargetUser, &t.Notes, &t.CreatedAt, &t.AssetTag)
			x.Transactions = append(x.Transactions, t)
		}
		rows.Close()
	}

	// Attachments metadata (without file data)
	{
		rows, err := db.Query("SELECT id, asset_id, file_name, file_path, file_size, mime_type, created_at FROM attachments ORDER BY id")
		if err == nil {
			for rows.Next() {
				var att Attachment
				rows.Scan(&att.ID, &att.AssetID, &att.FileName, &att.FilePath, &att.FileSize, &att.MimeType, &att.CreatedAt)
				x.Attachments = append(x.Attachments, Attachment{
					ID:        att.ID,
					AssetID:   att.AssetID,
					FileName:  att.FileName,
					FilePath:  att.FilePath,
					FileSize:  att.FileSize,
					MimeType:  att.MimeType,
					CreatedAt: att.CreatedAt,
				})
			}
			rows.Close()
		}
	}

	return xml.MarshalIndent(x, "", "  ")
}

func handleImportXML(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil || s.Role != "admin" {
		jsonErr(w, "Forbidden", 403)
		return
	}

	err := r.ParseMultipartForm(32 << 20) // 32MB
	if err != nil {
		jsonErr(w, "Parse error: "+err.Error(), 400)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		jsonErr(w, "File required: "+err.Error(), 400)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		jsonErr(w, "Read error: "+err.Error(), 400)
		return
	}

	var x XMLExport
	if err := xml.Unmarshal(data, &x); err != nil {
		jsonErr(w, "XML解析失败: "+err.Error(), 400)
		return
	}

	// Import in a transaction
	tx, err := db.Begin()
	if err != nil {
		jsonErr(w, "Database error: "+err.Error(), 500)
		return
	}
	defer tx.Rollback()

	// Clear all existing data
	tx.Exec("DELETE FROM custom_field_values")
	tx.Exec("DELETE FROM custom_fields")
	tx.Exec("DELETE FROM transactions")
	tx.Exec("DELETE FROM assets")
	tx.Exec("DELETE FROM users")
	tx.Exec("DELETE FROM field_presets")

	// Reset autoincrement
	tx.Exec("DELETE FROM sqlite_sequence")

	// Import custom fields (save new id mapping)
	fieldIDMap := make(map[int]int) // old ID -> new ID
	for _, f := range x.CustomFields {
		result, err := tx.Exec("INSERT INTO custom_fields (field_name, field_type, field_options, sort_order) VALUES (?,?,?,?)",
			f.FieldName, f.FieldType, f.Options, f.SortOrder)
		if err != nil {
			jsonErr(w, "导入自定义字段失败: "+err.Error(), 500)
			return
		}
		newID, _ := result.LastInsertId()
		fieldIDMap[f.ID] = int(newID)
	}

	// Import users
	userIDMap := make(map[int]int) // old ID -> new ID
	for _, u := range x.Users {
		result, err := tx.Exec("INSERT INTO users (name, department, phone, email, password, role, notes, active, created_at) VALUES (?,?,?,?,?,?,?,?,?)",
			u.Name, u.Department, u.Phone, u.Email, u.Password, u.Role, u.Notes, u.Active, u.CreatedAt)
		if err != nil {
			jsonErr(w, "导入用户失败: "+err.Error(), 500)
			return
		}
		newID, _ := result.LastInsertId()
		userIDMap[u.ID] = int(newID)
	}

	// Import assets
	for _, a := range x.Assets {
		result, err := tx.Exec(`INSERT INTO assets (asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			a.AssetTag, a.Type, a.Brand, a.Model, a.Serial, a.CPU, a.Memory, a.Disk, a.Status, a.PurchaseDate, a.PurchasePrice, a.Supplier, a.WarrantyEnd, a.CurrentUser, a.Location, a.Notes, a.CreatedAt, a.UpdatedAt)
		if err != nil {
			jsonErr(w, "导入资产失败: "+err.Error(), 500)
			return
		}
		newAssetID, _ := result.LastInsertId()

		// Import custom values for this asset
		for _, cv := range a.CustomValues {
			// Find the field ID by name
			var fieldID int
			err := tx.QueryRow("SELECT id FROM custom_fields WHERE field_name=?", cv.FieldName).Scan(&fieldID)
			if err != nil {
				continue // skip unknown fields
			}
			tx.Exec("INSERT INTO custom_field_values (asset_id, field_id, field_value) VALUES (?,?,?)",
				newAssetID, fieldID, cv.FieldValue)
		}

		// Update asset_id mapping for transactions
		_ = newAssetID // will use for transactions below
	}

	// Import transactions (with updated asset IDs)
	for _, t := range x.Transactions {
		tx.Exec("INSERT INTO transactions (asset_id, action, operator, target_user, notes, created_at, asset_tag) VALUES (?,?,?,?,?,?,?)",
			t.AssetID, t.Action, t.Operator, t.TargetUser, t.Notes, t.CreatedAt, t.AssetTag)
	}

	// Import scrapped assets
	for _, sa := range x.ScrappedAssets {
		tx.Exec("INSERT INTO scrapped_assets (id, asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes, scrap_reason, scrap_notes, scrapped_by, scrapped_at, restored_at, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
			sa.ID, sa.AssetTag, sa.Type, sa.Brand, sa.Model, sa.Serial,
			sa.CPU, sa.Memory, sa.Disk, sa.Status,
			sa.PurchaseDate, sa.PurchasePrice, sa.Supplier, sa.WarrantyEnd,
			sa.CurrentUser, sa.Location, sa.Notes,
			sa.ScrapReason, sa.ScrapNotes, sa.ScrappedBy,
			sa.ScrappedAt, sa.RestoredAt, sa.CreatedAt, sa.UpdatedAt)
	}

	// Save config (convert XMLConfig to AppConfig)
	xc := x.Config
	ac := AppConfig{
		CompanyName:        xc.CompanyName,
		TaxRate:            xc.TaxRate,
		BaseCurrency:       xc.BaseCurrency,
		DepreciationMethod: xc.DepreciationMethod,
		ExchangeRates:      map[string]float64{},
	}
	for _, r := range xc.ExchangeRates {
		ac.ExchangeRates[r.Code] = r.Value
	}
	saveConfig(ac)

	if err := tx.Commit(); err != nil {
		jsonErr(w, "提交事务失败: "+err.Error(), 500)
		return
	}

	jsonResp(w, map[string]interface{}{
		"status":       "ok",
		"imported":     len(x.Assets),
		"users":        len(x.Users),
		"fields":       len(x.CustomFields),
		"transactions": len(x.Transactions),
	})
}

// ─── ZIP Backup / Restore ───

func handleExportBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonErr(w, "POST required", 405)
		return
	}

	// Generate XML data
	xmlData, err := exportXML()
	if err != nil {
		jsonErr(w, "Export failed: "+err.Error(), 500)
		return
	}

	// Create ZIP in memory
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// 1. Write XML
	xw, _ := zw.Create("backup.xml")
	xw.Write([]byte(xml.Header))
	xw.Write(xmlData)

	// 2. Write attachments
	uploadsDir := filepath.Join(appDataDir, "uploads")
	entries, err := os.ReadDir(uploadsDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(uploadsDir, entry.Name()))
			if err != nil {
				continue
			}
			fw, _ := zw.Create("uploads/" + entry.Name())
			fw.Write(data)
		}
	}

	zw.Close()

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=l-asset-backup.zip")
	w.Write(buf.Bytes())
}

func handleImportBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonErr(w, "POST required", 405)
		return
	}

	err := r.ParseMultipartForm(256 << 20) // 256MB
	if err != nil {
		jsonErr(w, "Parse error: "+err.Error(), 400)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		jsonErr(w, "File required: "+err.Error(), 400)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		jsonErr(w, "Read error: "+err.Error(), 500)
		return
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		jsonErr(w, "Invalid ZIP file: "+err.Error(), 400)
		return
	}

	var xmlContent []byte
	uploadFiles := make(map[string][]byte)

	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			continue
		}
		content, _ := io.ReadAll(rc)
		rc.Close()

		if f.Name == "backup.xml" {
			xmlContent = content
		} else if len(f.Name) > 8 && f.Name[:8] == "uploads/" {
			uploadFiles[f.Name[8:]] = content
		}
	}

	if xmlContent == nil {
		jsonErr(w, "ZIP does not contain backup.xml", 400)
		return
	}

	// Import XML
	var x XMLExport
	if err := xml.Unmarshal(xmlContent, &x); err != nil {
		jsonErr(w, "XML parse error: "+err.Error(), 400)
		return
	}

	db.SetMaxOpenConns(3)
	defer db.SetMaxOpenConns(1)

	tx, err := db.Begin()
	if err != nil {
		jsonErr(w, "Begin transaction failed: "+err.Error(), 500)
		return
	}
	defer tx.Rollback()

	// Clear existing data
	tx.Exec("DELETE FROM transactions")
	tx.Exec("DELETE FROM custom_field_values")
	tx.Exec("DELETE FROM assets")
	tx.Exec("DELETE FROM scrapped_assets")
	tx.Exec("DELETE FROM users WHERE name != 'admin'")
	tx.Exec("DELETE FROM custom_fields")
	tx.Exec("DELETE FROM attachments")

	// Reimport admin (keep original admin password if not in backup)
	// admin will be imported from XML if present; skipped by WHERE name != 'admin'

	// Import users
	for _, u := range x.Users {
		tx.Exec("INSERT INTO users (id, name, department, phone, email, password, role, notes, active, created_at) VALUES (?,?,?,?,?,?,?,?,?,?)",
			u.ID, u.Name, u.Department, u.Phone, u.Email, u.Password, u.Role, u.Notes, u.Active, u.CreatedAt)
	}

	// Import custom fields
	for _, cf := range x.CustomFields {
		tx.Exec("INSERT INTO custom_fields (id, field_name, field_type, sort_order) VALUES (?,?,?,?)",
			cf.ID, cf.FieldName, cf.FieldType, cf.SortOrder)
	}

	// Import assets
	for _, a := range x.Assets {
		tx.Exec("INSERT INTO assets (id, asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
			a.ID, a.AssetTag, a.Type, a.Brand, a.Model, a.Serial,
			a.CPU, a.Memory, a.Disk, a.Status,
			a.PurchaseDate, a.PurchasePrice, a.Supplier, a.WarrantyEnd,
			a.CurrentUser, a.Location, a.Notes, a.CreatedAt)

		// Custom values
		for _, cv := range a.CustomValues {
			tx.Exec("INSERT INTO custom_field_values (asset_id, field_id, field_value) VALUES (?,?,?)",
				a.ID, cv.FieldID, cv.FieldValue)
		}
	}

	// Import transactions
	for _, t := range x.Transactions {
		tx.Exec("INSERT INTO transactions (asset_id, action, operator, target_user, notes, created_at, asset_tag) VALUES (?,?,?,?,?,?,?)",
			t.AssetID, t.Action, t.Operator, t.TargetUser, t.Notes, t.CreatedAt, t.AssetTag)
	}

	// Restore attachments: write files and restore DB records
	uploadsDir := filepath.Join(appDataDir, "uploads")
	os.MkdirAll(uploadsDir, 0755)

	// Write uploaded files from ZIP
	for name, content := range uploadFiles {
		os.WriteFile(filepath.Join(uploadsDir, name), content, 0644)
	}

	// Restore attachment database records from XML
	for _, att := range x.Attachments {
		tx.Exec("INSERT INTO attachments (id, asset_id, file_name, file_path, file_size, mime_type, created_at) VALUES (?,?,?,?,?,?,?)",
			att.ID, att.AssetID, att.FileName, att.FilePath, att.FileSize, att.MimeType, att.CreatedAt)
	}

	// Import scrapped assets
	for _, sa := range x.ScrappedAssets {
		tx.Exec("INSERT INTO scrapped_assets (id, asset_tag, type, brand, model, serial, cpu, memory, disk, status, purchase_date, purchase_price, supplier, warranty_end, current_user, location, notes, scrap_reason, scrap_notes, scrapped_by, scrapped_at, restored_at, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
			sa.ID, sa.AssetTag, sa.Type, sa.Brand, sa.Model, sa.Serial,
			sa.CPU, sa.Memory, sa.Disk, sa.Status,
			sa.PurchaseDate, sa.PurchasePrice, sa.Supplier, sa.WarrantyEnd,
			sa.CurrentUser, sa.Location, sa.Notes,
			sa.ScrapReason, sa.ScrapNotes, sa.ScrappedBy,
			sa.ScrappedAt, sa.RestoredAt, sa.CreatedAt, sa.UpdatedAt)
	}

	// Save config (convert XMLConfig to AppConfig)
	xc := x.Config
	ac := AppConfig{
		CompanyName:        xc.CompanyName,
		TaxRate:            xc.TaxRate,
		BaseCurrency:       xc.BaseCurrency,
		DepreciationMethod: xc.DepreciationMethod,
		ExchangeRates:      map[string]float64{},
	}
	for _, r := range xc.ExchangeRates {
		ac.ExchangeRates[r.Code] = r.Value
	}
	saveConfig(ac)

	if err := tx.Commit(); err != nil {
		jsonErr(w, "提交事务失败: "+err.Error(), 500)
		return
	}

	jsonResp(w, map[string]interface{}{
		"status":       "ok",
		"imported":     len(x.Assets),
		"users":        len(x.Users),
		"fields":       len(x.CustomFields),
		"transactions": len(x.Transactions),
		"attachments":  len(x.Attachments),
	})
}

// ─── Auth API Handlers ───

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonErr(w, "Method not allowed", 405)
		return
	}
	var req struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "Invalid JSON", 400)
		return
	}

	var u User
	err := db.QueryRow("SELECT id, name, department, phone, email, password, role, notes, active FROM users WHERE name=? AND active=1", req.Name).
		Scan(&u.ID, &u.Name, &u.Department, &u.Phone, &u.Email, &u.Password, &u.Role, &u.Notes, &u.Active)
	if err != nil {
		jsonErr(w, "用户名或密码错误", 401)
		return
	}

	if u.Password != hashPassword(req.Password) {
		jsonErr(w, "用户名或密码错误", 401)
		return
	}

	token := generateToken()
	sessions.Store(token, Session{
		UserID:   u.ID,
		UserName: u.Name,
		Role:     u.Role,
		Expires:  time.Now().Add(24 * time.Hour),
	})

	http.SetCookie(w, &http.Cookie{
		Name:    "lasset_token",
		Value:   token,
		Path:    "/",
		MaxAge:  86400, // 24h
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	jsonResp(w, map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":   u.ID,
			"name": u.Name,
			"role": u.Role,
		},
	})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonErr(w, "Method not allowed", 405)
		return
	}
	cookie, err := r.Cookie("lasset_token")
	if err == nil {
		sessions.Delete(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:   "lasset_token",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	jsonResp(w, map[string]string{"status": "ok"})
}

func handleMe(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil {
		jsonErr(w, "Unauthorized", 401)
		return
	}
	jsonResp(w, map[string]interface{}{
		"id":   s.UserID,
		"name": s.UserName,
		"role": s.Role,
	})
}

// ─── API: Attachments ───

func handleAttachments(w http.ResponseWriter, r *http.Request) {
	// Parse asset ID from URL: /api/attachments/<id>
	path := strings.TrimPrefix(r.URL.Path, "/api/attachments/")
	assetID, err := strconv.Atoi(strings.Split(path, "/")[0])
	if err != nil {
		jsonErr(w, "Invalid asset ID", 400)
		return
	}

	switch r.Method {
	case "GET":
		listAttachments(w, assetID)
	case "POST":
		uploadAttachment(w, r, assetID)
	case "DELETE":
		deleteAttachment(w, r, assetID)
	default:
		jsonErr(w, "Method not allowed", 405)
	}
}

func listAttachments(w http.ResponseWriter, assetID int) {
	rows, err := db.Query("SELECT id, asset_id, file_name, file_path, file_size, mime_type, created_at FROM attachments WHERE asset_id=? ORDER BY created_at DESC", assetID)
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var atts []Attachment
	for rows.Next() {
		var a Attachment
		rows.Scan(&a.ID, &a.AssetID, &a.FileName, &a.FilePath, &a.FileSize, &a.MimeType, &a.CreatedAt)
		atts = append(atts, a)
	}
	jsonResp(w, atts)
}

func uploadAttachment(w http.ResponseWriter, r *http.Request, assetID int) {
	err := r.ParseMultipartForm(32 << 20) // 32MB max
	if err != nil {
		jsonErr(w, "Parse error: "+err.Error(), 400)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		jsonErr(w, "File required: "+err.Error(), 400)
		return
	}
	defer file.Close()

	// ── File validation ──
	// Size limit: 5MB
	if header.Size > 5*1024*1024 {
		jsonErr(w, "文件大小不能超过 5MB", 400)
		return
	}

	// Extension whitelist
	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowedExts := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".bmp": true,
		".pdf": true,
	}
	if !allowedExts[ext] {
		jsonErr(w, "不支持的文件格式,仅允许图片(jpg/png/gif/webp/bmp)和 PDF", 400)
		return
	}

	// Verify MIME type by reading header bytes
	buf := make([]byte, 512)
	file.Read(buf)
	file.Seek(0, 0)
	detectedMime := http.DetectContentType(buf)

	allowedMimes := map[string]bool{
		"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true, "image/bmp": true,
		"application/pdf": true,
	}
	if !allowedMimes[detectedMime] {
		jsonErr(w, "文件内容类型不合法,仅允许图片和 PDF", 400)
		return
	}

	// Verify asset exists
	var tag string
	err = db.QueryRow("SELECT asset_tag FROM assets WHERE id=?", assetID).Scan(&tag)
	if err != nil {
		jsonErr(w, "Asset not found", 404)
		return
	}

	uploadsDir := filepath.Join(appDataDir, "uploads")
	os.MkdirAll(uploadsDir, 0755)

	// Generate unique filename to prevent collisions
	name := fmt.Sprintf("%d_%s%s", assetID, time.Now().Format("20060102150405"), ext)
	savePath := filepath.Join(uploadsDir, name)

	dst, err := os.Create(savePath)
	if err != nil {
		jsonErr(w, "Save error: "+err.Error(), 500)
		return
	}
	defer dst.Close()

	written, err := io.Copy(dst, file)
	if err != nil {
		jsonErr(w, "Write error: "+err.Error(), 500)
		return
	}

	_, err = db.Exec("INSERT INTO attachments (asset_id, file_name, file_path, file_size, mime_type) VALUES (?,?,?,?,?)",
		assetID, header.Filename, name, written, detectedMime)
	if err != nil {
		jsonErr(w, "DB error: "+err.Error(), 500)
		return
	}

	jsonResp(w, map[string]string{"status": "ok", "file": name})
}

func deleteAttachment(w http.ResponseWriter, r *http.Request, assetID int) {
	// Get attachment ID from body
	var body struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, "Invalid JSON", 400)
		return
	}

	// Get file path
	var filePath string
	err := db.QueryRow("SELECT file_path FROM attachments WHERE id=? AND asset_id=?", body.ID, assetID).Scan(&filePath)
	if err != nil {
		jsonErr(w, "Attachment not found", 404)
		return
	}

	// Delete file from disk
	fullPath := filepath.Join(appDataDir, "uploads", filePath)
	os.Remove(fullPath)

	// Delete from DB
	db.Exec("DELETE FROM attachments WHERE id=? AND asset_id=?", body.ID, assetID)

	jsonResp(w, map[string]string{"status": "ok"})
}

