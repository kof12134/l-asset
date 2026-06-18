// +build ignore

package main

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

func hashPassword(pwd string) string {
	h := sha256.Sum256([]byte(pwd))
	return fmt.Sprintf("%x", h)
}

func main() {
	appDataDir := "./data"
	if os.Getenv("LASSET_DATA") != "" {
		appDataDir = os.Getenv("LASSET_DATA")
	}

	dbPath := filepath.Join(appDataDir, "l-asset.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	db.SetMaxOpenConns(1)

	log.Println("🧹 清理已有数据...")
	db.Exec("DELETE FROM custom_field_values")
	db.Exec("DELETE FROM custom_fields")
	db.Exec("DELETE FROM transactions")
	db.Exec("DELETE FROM field_presets")
	db.Exec("DELETE FROM attachments")
	db.Exec("DELETE FROM scrapped_assets")
	db.Exec("DELETE FROM assets")
	db.Exec("DELETE FROM users")

	log.Println("📋 创建自定义字段...")
	db.Exec("INSERT INTO custom_fields (field_name, field_type) VALUES ('操作系统', 'text')")
	db.Exec("INSERT INTO custom_fields (field_name, field_type) VALUES ('屏幕尺寸', 'text')")

	log.Println("📋 创建预设值...")
	presets := []struct{ key, value string }{
		{"type", "笔记本"}, {"type", "台式机"}, {"type", "显示器"}, {"type", "打印机"}, {"type", "其他"},
		{"status", "在库"}, {"status", "已领用"}, {"status", "已报废"},
		{"brand", "Lenovo"}, {"brand", "Dell"}, {"brand", "HP"}, {"brand", "Apple"}, {"brand", "Huawei"},
		{"location", "办公室"}, {"location", "机房"}, {"location", "仓库"}, {"location", "会议室"},
		{"warranty_years", "1"}, {"warranty_years", "2"}, {"warranty_years", "3"}, {"warranty_years", "5"},
	}
	for _, p := range presets {
		db.Exec("INSERT OR IGNORE INTO field_presets (field_key, field_value, sort_order) VALUES (?, ?, 0)", p.key, p.value)
	}

	// ── 用户 ──
	log.Println("👤 创建用户...")
	adminPW := hashPassword("admin123")
	db.Exec("INSERT INTO users (name, department, phone, email, password, role, notes) VALUES (?,?,?,?,?,?,?)",
		"admin", "", "", "", adminPW, "admin", "系统管理员")

	userData := []struct {
		name, dept, phone, email, notes string
	}{
		{"张三", "技术部", "13800138001", "zhangsan@company.com", "后端开发，负责核心业务"},
		{"李四", "市场部", "13800138002", "lisi@company.com", "市场推广负责人"},
		{"王五", "人事部", "13800138003", "wangwu@company.com", "HRBP"},
		{"赵六", "财务部", "13800138004", "zhaoliu@company.com", "财务主管"},
		{"陈七", "技术部", "13800138005", "chenqi@company.com", "前端开发"},
		{"刘八", "技术部", "13800138006", "liuba@company.com", "测试工程师"},
		{"孙九", "行政部", "13800138007", "sunjie@company.com", "行政主管"},
		{"周十", "销售部", "13800138008", "zhoushi@company.com", "销售总监"},
		{"吴一", "技术部", "13800138009", "wuyi@company.com", "运维工程师"},
		{"郑二", "技术部", "13800138010", "zhenger@company.com", "数据库管理员"},
		{"林芳", "市场部", "13800138011", "linfang@company.com", "品牌经理"},
		{"黄明", "技术部", "13800138012", "huangming@company.com", "技术总监"},
		{"杨丽", "销售部", "13800138013", "yangli@company.com", "区域销售经理"},
		{"许强", "技术部", "13800138014", "xuqiang@company.com", "系统架构师"},
		{"何伟", "技术部", "13800138015", "hewei@company.com", "DevOps"},
		{"马超", "技术部", "13800138016", "machao@company.com", "安全工程师"},
		{"董洁", "人事部", "13800138017", "dongjie@company.com", "招聘专员"},
		{"方明", "行政部", "13800138018", "fangming@company.com", "资产管理"},
		{"钱峰", "财务部", "13800138019", "qianfeng@company.com", "出纳"},
		{"唐文", "销售部", "13800138020", "tangwen@company.com", "客服经理"},
	}
	pw := hashPassword("123456")
	for _, u := range userData {
		db.Exec("INSERT INTO users (name, department, phone, email, password, role, notes, active) VALUES (?,?,?,?,?,?,?,1)",
			u.name, u.dept, u.phone, u.email, pw, "user", u.notes)
	}

	// ── 资产 ──
	log.Println("💻 创建资产...")
	now := time.Now().Format("2006-01-02 15:04:05")

	type assetRow struct {
		tag, typ, brand, model, serial, status, user, location, notes string
		purchaseDate                                                   string
		price                                                          float64
		supplier                                                       string
		warrantyEnd                                                    string
		os, screen                                                     string
	}

	assets := []assetRow{
		{"NB-001", "笔记本", "Lenovo", "ThinkPad X1 Carbon Gen 11", "S/N: X1C23A0001", "已领用", "张三", "办公室A", "主力开发机", "2025-03-01", 12999, "京东", "2028-03-01", "Windows 11", "14英寸"},
		{"NB-002", "笔记本", "Lenovo", "ThinkPad X1 Carbon Gen 11", "S/N: X1C23A0002", "已领用", "张三", "办公室A", "备用开发机", "2025-03-01", 12999, "京东", "2028-03-01", "Windows 11", "14英寸"},
		{"NB-003", "笔记本", "Dell", "Latitude 5440", "S/N: LAT5440B001", "已领用", "李四", "办公室B", "市场部用", "2025-04-15", 8999, "Dell官方", "2028-04-15", "Windows 11", "14英寸"},
		{"NB-004", "笔记本", "HP", "EliteBook 840 G10", "S/N: EB840G1004", "已领用", "王五", "办公室C", "人事部用", "2025-05-01", 9999, "HP官方", "2028-05-01", "Windows 11", "14英寸"},
		{"NB-005", "笔记本", "Apple", "MacBook Pro 14 M3", "S/N: MBP14M3005", "已领用", "赵六", "办公室D", "财务用", "2025-06-01", 14999, "Apple Store", "2028-06-01", "macOS Sonoma", "14英寸"},
		{"NB-006", "笔记本", "Lenovo", "ThinkPad T14s Gen 4", "S/N: T14S4A0006", "已领用", "陈七", "办公室A", "前端开发机", "2025-06-15", 10999, "京东", "2028-06-15", "Windows 11", "14英寸"},
		{"NB-007", "笔记本", "Lenovo", "ThinkPad P16s Gen 2", "S/N: P16S2A0007", "已领用", "郑二", "机房", "数据库管理笔记本", "2025-07-01", 15999, "京东", "2028-07-01", "Windows 11", "16英寸"},
		{"NB-008", "笔记本", "Huawei", "MateBook X Pro 2024", "S/N: MBXP240008", "在库", "", "仓库", "新品待分配", "2025-08-01", 11999, "华为商城", "2028-08-01", "Windows 11", "14.2英寸"},
		{"NB-009", "笔记本", "Dell", "XPS 15 9530", "S/N: XPS15953009", "已领用", "林芳", "办公室B", "设计用笔记本", "2025-08-15", 13999, "Dell官方", "2028-08-15", "Windows 11", "15.6英寸"},
		{"DT-001", "台式机", "Lenovo", "ThinkCentre M950t", "S/N: M950tA0001", "已领用", "黄明", "办公室A", "技术总监主机", "2024-12-01", 8999, "京东", "2027-12-01", "Windows 11", "-"},
		{"DT-002", "台式机", "Dell", "OptiPlex 7010 Plus", "S/N: OP7010P002", "已领用", "刘八", "办公室A", "测试环境主机", "2025-01-15", 6999, "Dell官方", "2028-01-15", "Windows 11", "-"},
		{"DT-003", "台式机", "Lenovo", "ThinkStation P3 Tower", "S/N: TSP3T00003", "已领用", "许强", "机房", "开发服务器", "2025-02-01", 19999, "京东", "2028-02-01", "Ubuntu 22.04", "-"},
		{"DT-004", "台式机", "HP", "Z2 Tower G9", "S/N: Z2TG900004", "已领用", "吴一", "机房", "CI/CD 构建机", "2025-03-01", 12999, "HP官方", "2028-03-01", "Ubuntu 22.04", "-"},
		{"DT-005", "台式机", "Lenovo", "ThinkCentre M750t", "S/N: M750tA0005", "在库", "", "仓库", "备机", "2025-04-01", 5999, "京东", "2028-04-01", "Windows 10", "-"},
		{"DT-006", "台式机", "Dell", "Precision 3660", "S/N: PR36600006", "已领用", "马超", "机房", "安全审计工作站", "2025-05-01", 16999, "Dell官方", "2028-05-01", "Kali Linux", "-"},
		{"MNT-001", "显示器", "Dell", "U2723QE 4K", "S/N: U2723Q0001", "已领用", "张三", "办公室A", "4K显示器", "2025-03-01", 4599, "Dell官方", "2028-03-01", "-", "27英寸"},
		{"MNT-002", "显示器", "Dell", "U2723QE 4K", "S/N: U2723Q0002", "已领用", "黄明", "办公室A", "4K显示器", "2025-03-01", 4599, "Dell官方", "2028-03-01", "-", "27英寸"},
		{"MNT-003", "显示器", "Dell", "U2422H", "S/N: U2422H0003", "已领用", "陈七", "办公室A", "副屏", "2025-06-15", 1999, "京东", "2028-06-15", "-", "24英寸"},
		{"MNT-004", "显示器", "Lenovo", "ThinkVision P27u-20", "S/N: TVP272004", "在库", "", "仓库", "备机", "2025-07-01", 3499, "京东", "2028-07-01", "-", "27英寸"},
		{"MNT-005", "显示器", "Apple", "Studio Display", "S/N: ASD270005", "已领用", "赵六", "办公室D", "财务用", "2025-06-01", 11499, "Apple Store", "2028-06-01", "-", "27英寸"},
		{"PRT-001", "打印机", "HP", "LaserJet Pro M404dn", "S/N: LJM4040001", "已领用", "周十", "销售部", "销售部共享打印机", "2024-11-01", 3299, "HP官方", "2027-11-01", "-", "-"},
		{"PRT-002", "打印机", "HP", "Color LaserJet Pro M454dw", "S/N: CLJM454002", "已领用", "唐文", "销售部", "彩色打印需求", "2025-01-01", 4599, "HP官方", "2028-01-01", "-", "-"},
		{"PRT-003", "打印机", "Lenovo", "LJ2405D", "S/N: LJ2405D003", "在库", "", "仓库", "备机", "2025-04-01", 1299, "京东", "2028-04-01", "-", "-"},
		{"SRV-001", "台式机", "Lenovo", "ThinkSystem SR650 V3", "S/N: SR650V30001", "已领用", "何伟", "机房", "DevOps 服务器", "2024-10-01", 49999, "Lenovo官方", "2029-10-01", "Rocky Linux 9", "-"},
		{"SRV-002", "台式机", "Dell", "PowerEdge R750xs", "S/N: PER750X002", "已领用", "吴一", "机房", "备份服务器", "2024-12-01", 39999, "Dell官方", "2029-12-01", "Proxmox VE", "-"},
	}

	var osFieldID, screenFieldID int
	db.QueryRow("SELECT id FROM custom_fields WHERE field_name='操作系统'").Scan(&osFieldID)
	db.QueryRow("SELECT id FROM custom_fields WHERE field_name='屏幕尺寸'").Scan(&screenFieldID)

	for _, a := range assets {
		res, err := db.Exec(`INSERT INTO assets 
			(asset_tag, type, brand, model, serial, status, current_user, location, notes, 
			 purchase_date, purchase_price, supplier, warranty_end, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			a.tag, a.typ, a.brand, a.model, a.serial, a.status, a.user, a.location, a.notes,
			a.purchaseDate, a.price, a.supplier, a.warrantyEnd, now, now)
		if err != nil {
			log.Printf("  ⚠️  插入资产 %s 失败: %v", a.tag, err)
			continue
		}
		assetID, _ := res.LastInsertId()

		// Record transaction
		if a.status == "已领用" {
			db.Exec(`INSERT INTO transactions (asset_id, action, operator, target_user, notes, created_at, asset_tag)
				VALUES (?, '领用', 'admin', ?, '测试数据 - 自动领用', ?, ?)`, assetID, a.user, now, a.tag)
		} else if a.status == "在库" {
			db.Exec(`INSERT INTO transactions (asset_id, action, operator, notes, created_at, asset_tag)
				VALUES (?, '入库', 'admin', '测试数据 - 自动入库', ?, ?)`, assetID, now, a.tag)
		}

		// Custom fields
		if a.os != "" && a.os != "-" {
			db.Exec(`INSERT OR IGNORE INTO custom_field_values (asset_id, field_id, field_value) VALUES (?, ?, ?)`, assetID, osFieldID, a.os)
		}
		if a.screen != "" && a.screen != "-" {
			db.Exec(`INSERT OR IGNORE INTO custom_field_values (asset_id, field_id, field_value) VALUES (?, ?, ?)`, assetID, screenFieldID, a.screen)
		}
	}

	// Stats
	var userCount, assetCount, transactionCount int
	db.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount)
	db.QueryRow("SELECT COUNT(*) FROM assets").Scan(&assetCount)
	db.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&transactionCount)
	log.Printf("✅ 完成！共 %d 个用户、%d 个资产、%d 条操作记录", userCount, assetCount, transactionCount)
}
