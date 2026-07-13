# L-Asset 资产管理系统 — 项目日志

**作者：** 乐为爸爸  
**技术顾问：** Hu（AI assistant）  
**编程语言：** Go 1.21+（编译期 Go 1.26.4）  
**数据库：** SQLite（通过 modernc.org/sqlite 纯 Go 驱动）  
**前端：** Alpine.js 3.x + Tailwind CSS（CDN）  
**当前源码：** `/vol1/1000/WorkSpace/asset-manager/main.go`（~3700 行）  
**更新日期：** 2026-07-13

---

## 目录

1. [项目概述](#1-项目概述)
2. [环境与约束](#2-环境与约束)
3. [功能变更记录](#3-功能变更记录)
   - 3.1 初始启动与端口修正
   - 3.2 模板冲突修复
   - 3.3 SQLite 死锁与 CSV 导出修复
   - 3.4 字段管理
   - 3.5 用户管理
   - 3.6 登录认证与权限
   - 3.7 修改密码
   - 3.8 用户启停
   - 3.9 批量导入权限
   - 3.10 资产领用关联用户
   - 3.11 企业名称配置
   - 3.12 版权信息
   - 3.13 选项设置页面
   - 3.14 Windows 打包
   - 3.15 克隆功能修复
   - 3.16 资产编号格式修正
   - 3.17 资产状态编辑修复
   - 3.18 Windows 启动脚本改进
   - 3.19 添加资产时自定义字段
   - 3.20 窗口托盘程序
   - 3.21 登录界面重构
   - 3.22 全面代码审计与修复（2026-07-13）
4. [架构决策](#4-架构决策)
5. [数据库表结构](#5-数据库表结构)
6. [API 文档](#6-api-文档)
7. [编译与部署](#7-编译与部署)
8. [已知问题 / 待办](#8-已知问题--待办)

---

## 1. 项目概述

L-Asset 是一个轻量级的资产管理系统，运行于 NAS，使用 SQLite 作为数据库（单文件、免安装），面向中小团队快速上线资产管理需求。

核心功能：
- 资产 CRUD、搜索、批量导入/导出 CSV
- 字段自定义（预设 + 自定义字段）
- 用户管理 + 角色权限（管理员/普通用户）
- 资产领用/归还/报废
- 财务管理（折旧计算）
- 数据备份与恢复（XML / ZIP）
- 附件上传
- Windows 系统托盘程序

## 2. 环境与约束

| 项目 | 内容 |
|---|---|
| 运行环境 | 飞牛私有云 fnOS（基于 Linux） |
| NAS 主机名 | Nas203 |
| Go 版本 | 1.26.4 |
| Go 代理 | `GOPROXY=https://goproxy.cn,direct`（国内源，避免超时） |
| 端口 | 5678（NAS 上 8080 被占用，故改用 5678） |
| 数据库 | SQLite，`SetMaxOpenConns(1)` — 不支持并发查询 |
| OpenClaw | 运行在 NAS 上的 AI agent，管理本项目 |
| 开发调试 | 通过 WebChat 与 OpenClaw 交互，AI 直接修改源码并编译 |
| 编译目标 | Linux amd64 + Windows amd64 |

## 3. 功能变更记录

### 3.1 初始启动与端口修正

**背景：** 原始代码监听 8080，但 NAS 上该端口已被占用。

**修改：**
- 默认端口改为 5678
- 支持环境变量 `LASSET_PORT` 覆盖
- 支持环境变量 `LASSET_DATA` 指定数据目录
- 数据目录自动创建（`os.MkdirAll`）

**文件：** `main.go` — `func main()`

### 3.2 模板冲突修复 ⚠️

**问题：** 所有子页面使用 `{{define "content"}}`，Go 的 `ParseFS` 按文件名排序加载所有模板。排序后 `transactions.html` 最后加载，其 `"content"` 定义覆盖了所有其他页面的内容。结果是**所有页面显示的 transactions 的内容**。

**根因：** Go `template.ParseFS` 中 `{{define}}` 名称冲突时，后加载的胜出。

**修复方案：** `render()` 函数不再一次性解析全部模板，而是只解析 `layout.html` + 当前页面模板：

```go
func render(w http.ResponseWriter, pageTmpl string, data interface{}) {
    t := template.Must(template.New("layout.html").Funcs(template.FuncMap{...}).ParseFS(templateFS, "templates/layout.html", pageTmpl))
    t.ExecuteTemplate(w, "layout.html", data)
}
```

**教训：** 使用 `ParseFS` 时，`{{define}}` 全局同名 = 覆盖。**必须分页解析。**

### 3.3 SQLite 死锁与 CSV 导出修复 ⚠️

**问题：** `handleExport()` 中遍历资产结果集时，对每条记录发起 `db.QueryRow()` 查询自定义字段名，而 `db.SetMaxOpenConns(1)` 限制下，第二个查询永远无法获得连接 → **死锁**。

**修复：**
- 先一次性查询所有自定义字段名，存到 slice 中
- 再一次性查询所有资产数据，读完整个结果集并关闭 `rows`
- 最后遍历内存中的数据拼 CSV 写入 response

**教训：** `SetMaxOpenConns(1)` 下，**永远不能在一个结果集未关闭时发起新查询**。如果必须查关联数据，分阶段读取+内存缓存。

### 3.4 字段管理

**功能：**
- 独立页面 `/fields`
- 内置字段（品牌/类型/状态）可管理预设值（增删预设选项）
- 自定义字段增删
- 字段预设值以 `sort_order` 排序
- 添加资产时品牌/类型/状态改为 `<select>` 下拉框

**数据库：** `field_presets` 表

**种子数据：**
- 品牌：Lenovo、Dell、HP、Apple、Huawei
- 类型：笔记本、台式机、服务器、显示器、打印机、网络设备、其他
- 状态：在库、已领用、已报废、维修中

### 3.5 用户管理

**功能：**
- 独立页面 `/users`
- 用户 CRUD（姓名、部门、电话、邮箱、角色、状态、密码）
- 用户名唯一（`UNIQUE` 约束）
- 使用人搜索组件（输入即搜，实时过滤用户列表）
- 用户增删改时自动同步到所有资产选择下拉中

**API：** `GET /api/users`、`POST /api/users`、`PUT /api/users/{id}`、`DELETE /api/users/{id}`、`GET /api/users/{id}/assets`

**密码存储：** SHA256 哈希

**默认管理员：** `admin / admin123`，自动创建（初始无用户时）

### 3.6 登录认证与权限

**功能：**
- 独立的登录页面 `/login`
- 基于 token（SHA256 随机串）+ Cookie 的 Session 管理
- Token 有效期 24 小时
- 所有 `/api/*` 路由和页面路由需要登录验证（`requireAuth` 中间件）
- 角色分两种：`admin`（管理员）和 `user`（普通用户）

**权限矩阵：**

| 功能 | admin | user |
|---|---|---|
| 资产查看/录入/编辑 | ✅ | ✅ |
| 操作记录查看 | ✅ | ✅ |
| 批量导入 | ✅ | ❌ |
| 字段管理 | ✅ | ❌ |
| 用户管理 | ✅ | ❌ |
| 设置页 | ✅ | ✅ |

**实现：**
- `requireAuth()` 包装器检查 Cookie `lasset_token`
- Token → Session 内存 map（程序重启后所有 token 失效，需重新登录）
- 页面路由用 `{{if .IsAdmin}}...{{end}}` 控制显示

### 3.7 修改密码

**功能：**
- 用户管理编辑弹窗中，管理员编辑用户时可填写新密码（留空不修改）
- 设置页面新增「修改管理员密码」功能（需验证旧密码）

**关键问题修复（admin 改密码后无法登录）：**
- **问题 1：** 编辑用户时前端没传 `active` 字段，Go 解码为默认 int `0` → admin 被停用
- **修复：** editUser() 中保留 `u.active` 到 form，PUT 请求带上 `active` 字段
- **问题 2：** 编辑用户时前端没传 `role` 字段，Go 解码为空字符串 → admin 角色被覆盖为空
- **修复：** `updateUser()` 中 `currentRole` 为空时从数据库读取原有角色保留

### 3.8 用户启停

**修改：**
- 编辑弹窗中加勾选框控制用户状态（启用/停用）
- admin 用户不可停用（复选框禁用并提示）

### 3.9 批量导入权限收紧

**修改：**
- 导航栏：`{{if .IsAdmin}}` 包住
- 页面 `/import`：非 admin 重定向到首页
- API `/api/assets/batch-import`：非 admin 返回 403

### 3.10 资产领用关联用户

**改动：**
- 资产详情页领用时，不再用自由文本输入框，改为搜索+下拉选择已注册用户
- 用户管理页点击用户名弹窗，显示该用户当前领用的所有设备

**API：** `GET /api/users/{id}/assets` — 查询 `current_user=用户名 AND status='已领用'`

### 3.11 企业名称配置

**功能：**
- 设置页面可配置企业名称
- 配置存储为 `data/config.json`（JSON 文件，非数据库）
- 新建资产不填编号时自动生成：`企业名-序号`（如「乐为科技-3」）
- 默认企业名称为 `PC`（兼容原有编号格式 `PC-1`）

**实现：** `loadConfig()` / `saveConfig()` + `AppConfig` 结构体 + API `/api/settings`

### 3.12 版权信息

- 源码头部注释（`main.go`）
- 所有页面底部：`L-Asset v1.0 © 2026 乐为爸爸. All rights reserved.`
- 登录页面单独展示

### 3.13 选项设置页面

- 导航栏右侧用户名旁加齿轮图标 ⚙️ 可点击进入设置
- 设置页包含：企业名称、修改管理员密码、CSV 导出、系统信息

### 3.14 Windows 打包

- 跨平台编译：`GOOS=windows GOARCH=amd64 go build -o output/l-asset.exe`
- `start.bat` 双击启动（自动打开浏览器）
- 输出目录：`/vol1/1000/WorkSpace/asset-manager/output/`

### 3.15 克隆功能修复（2026-06-30）

**问题：** 资产列表点击「克隆」后，弹出添加表单未填入原资产的数据。

**根因：** `getAsset` API 返回 `{"asset": {...}, "finance": {...}}` 结构，但 `cloneAsset()` 中直接从顶层取 `a.type`、`a.brand` 等字段，未通过 `a.asset` 访问。

**修复：** `cloneAsset()` 改为 `const a = resp.asset || resp` 解包。

### 3.16 资产编号格式修正（2026-06-30）

**问题：** 设置了企业名称后，自动生成的资产编号仍以 `PC-001` 格式创建，未按设计使用公司名+缩写+序号。

**修改：** 编号生成改为 `公司名-类型缩写-三位序号`（如 `乐乐-NB-001`），缩写取自 `field_presets` 表的 `abbr` 字段（字段管理页面可配置）。

### 3.17 资产状态编辑修复（2026-06-30）

**问题：** 
1. 编辑页面中状态字段为只读（`status_readonly`），添加时未选状态的话编辑时无法修改。
2. 即使修改了，后端 `updateAsset()` 中状态被 `currentStatus`（数据库旧值）覆盖，忽略前端传入的值。

**修复：**
- 编辑页状态改为下拉选择（`select`），使用 `status` 预设值。
- 后端优先使用前端传的 `status`，再根据 `current_user` 自动调整。

### 3.18 Windows 启动脚本改进（2026-06-30）

**修改：**
- `start.bat`：后台启动服务 + 打开浏览器，窗口自动退出，exe 继续运行。
- 新增 `start-silent.bat`：菜单式管理工具（启动/停止/查看状态）。

### 3.19 添加资产时自定义字段（2026-07-02）

**功能：**
- 新建/编辑资产时，用户自定义字段（如"操作系统"、"屏幕尺寸"）同时保存
- 前后端联动：前端表单包含自定义字段，后端接收 `custom_values` 数组

### 3.20 窗口托盘程序（2026-07-02）

**功能：**
- Windows 系统托盘程序 `l-asset-tray.exe`
- 托盘菜单：启动/停止/重启服务、打开网页、端口设置、打开 data 目录
- 服务进程由托盘程序管理（子进程方式）
- 日志输出到 `logs/l-asset-tray.log` 和 `logs/tray.log`

### 3.21 登录界面重构（2026-07-02）

**功能：**
- 登录页面重新设计，现代化 UI
- 托盘程序支持登录状态管理

### 3.22 全面代码审计与修复（2026-07-13）⭐

**背景：** 由 Hu（AI assistant）对全部源码（~3700 行 main.go + seed.go + tray/main.go）进行系统性代码审查，发现 20 个问题，修复 17 个（3 个按用户要求跳过）。

#### 🔴 严重问题修复

**3.22.1 报废/恢复资产丢失自定义字段值**
- **问题：** 报废时 `DELETE FROM assets` 触发 `custom_field_values` 的 `ON DELETE CASCADE`，自定义字段数据永久丢失。恢复时无法还原。
- **修复：** 
  - `scrapped_assets` 表新增 `custom_values TEXT` 列（JSON 格式存储）
  - 报废前查询自定义字段值 → JSON 序列化 → 写入 scrapped_assets
  - 恢复时解析 JSON → 重新插入 custom_field_values
  - 单个报废、批量报废均已修复
  - 自动迁移：`addColumnIfMissing("scrapped_assets", "custom_values", "TEXT DEFAULT ''")`

**3.22.2 批量领用无状态校验**
- **问题：** 批量 checkout 直接 UPDATE，不检查资产是否已领用/报废。可能重复领用。
- **修复：** 批量 checkout 前增加状态检查：`SELECT COUNT(*) ... WHERE status != '在库'`，有非在库资产时拒绝操作。

**3.22.3 导入导出丢失 field_options**
- **问题：** ZIP 导入的 `custom_fields` INSERT 遗漏 `field_options` 列，select/multi-select 类型字段选项丢失。
- **修复：** ZIP 导入的 `INSERT INTO custom_fields` 补上 `field_options` 列。

#### 🟠 高优先级修复

**3.22.4 XML 导出泄露密码哈希**
- **问题：** `exportXML()` 把用户密码哈希打入 XML。虽然无 salt 的 SHA256 安全性有限，但不应该导出。
- **修复：** 导出用户时不再读取 `password` 字段，导出后 `Password` 为空字符串。

**3.22.5 ZIP 备份导入覆盖 admin 密码**
- **问题：** `handleImportBackup` 中保留当前 admin 用户，但仍尝试从备份插入 admin → 可能覆盖密码。
- **修复：** 导入用户时跳过 `name='admin'` 的用户，保留当前管理员凭据。

**3.22.6 /api/users 无管理员检查**
- **问题：** 页面路由 `/users` 是 admin-only，但 API 路由 `/api/users` 的 GET 方法只用了 `requireAuth`，任何登录用户都能列出所有用户信息（含电话、邮箱）。
- **修复：** `handleUsers()` 改为所有方法都检查 admin 角色。

#### 🟡 中优先级修复

**3.22.7 资产编号自动生成竞态条件**
- **问题：** `createAsset` 中 `SELECT MAX(seq)` + `INSERT` 不在同一事务，并发可能产生重复编号。
- **修复：** 编号生成查询包裹在 `db.Begin()` / `Commit()` 事务中。

**3.22.8 无优雅退出**
- **问题：** 主进程直接 `http.ListenAndServe`，Ctrl+C 时 sessions 丢失，DB 不关闭。
- **修复：** 添加 `signal.Notify` 监听 `SIGINT`/`SIGTERM`，收到信号后 `db.Close()` + `srv.Close()`。

**3.22.9 错误信息泄露到客户端**
- **问题：** 大量 `jsonErr(w, err.Error(), 500)` 把原始 Go/SQLite 错误返回前端。
- **修复：** 新增 `logErr()` 函数替代，错误日志输出到服务器，前端只收到通用消息（如 "Internal server error"、"Parse error" 等）。

**3.22.10 删除资产时附件文件不清理**
- **问题：** `DELETE FROM assets` 触发级联删除 attachments 记录，但磁盘文件不删除，产生孤儿文件。
- **修复：** 删除前查询附件路径，从磁盘移除后再删除数据库记录。单个删除和批量删除均已修复。

**3.22.11 ensureDefaults ALTER TABLE 静默失败**
- **问题：** 数据库迁移用 `ALTER TABLE ADD COLUMN`，列已存在时静默报错，无法区分真正的错误。
- **修复：** 新增 `addColumnIfMissing()` 辅助函数，先用 `PRAGMA table_info` 检查列是否存在，存在则跳过。

#### 🟢 低优先级改进

**3.22.12 Cookie Secure flag**
- **修改：** Cookie 添加 `Secure: true`，`SameSite` 从 `Lax` 升级为 `Strict`。

**3.22.13 废弃 build tag**
- **修改：** `seed.go` 的 `// +build ignore` 改为 `//go:build ignore`（Go 1.17+ 标准）。

**3.22.14 删除未使用类型**
- **修改：** 删除 `main.go` 中未使用的 `XMLAttachment` 结构体定义。

**3.22.15 Tray 进程管理竞态**
- **修改：** `serverCmd.Store(cmd)` 移到 `cmd.Start()` 之前，避免极短生命周期的进程在 store 之前已完成。

**3.22.16 报废物 XML 导出缺自定义值**
- **修改：** XML/ZIP 导出时查询 `scrapped_assets.custom_values` JSON 列，解析后包含在导出中。导入时序列化回 JSON 存储。

**3.22.17 Windows GUI 构建**
- **修改：** 编译 Windows exe 时添加 `-ldflags "-H windowsgui"`，托盘程序不再弹出黑色命令行窗口。

#### ⏭️ 按用户要求跳过的项目
- ~~无登录限速~~ — 内部小团队系统，暂不添加
- ~~无 CSRF 保护~~ — `SameSite: Strict` Cookie + 内网环境已足够
- ~~SHA256 无盐~~ — 内部系统，权衡后保持简单方案

## 4. 架构决策

### 4.1 单体应用

选择单文件 Go 应用 + SQLite，因为：
- 零依赖部署（只有一个二进制文件）
- 无需 MySQL/PostgreSQL 服务器
- 数据备份 = 拷贝一个文件
- 启动即用，无需初始化

### 4.2 Session 存储

使用内存 map（`sync.Map`）而非数据库，因为：
- 轻量，无需查数据库
- 重启即全部失效（自动要求重新登录）
- 适用中小团队场景

### 4.3 模板渲染策略

拆分页面的 `render()` + 独立页面的 `renderStandalone()`：
- `render()`：解析 `layout.html` + 当前页，用 `ExecuteTemplate(w, "layout.html", data)` 渲染
- `renderStandalone()`：解析独立 HTML（如登录页），用 `Execute(w, nil)` 渲染

### 4.4 配置存储

JSON 文件（`data/config.json`）而非数据库表，原因：
- 配置只有少数 key-value，不值得建表
- 易读、易手动编辑
- 备份数据库时不影响 config 文件的重写

### 4.5 密码哈希

SHA256（非 bcrypt/argon2），原因：
- 简单快速
- 内部小团队系统，非面向互联网
- token 生成同样使用 SHA256

### 4.6 报废物自定义字段存储

使用 JSON 列（`custom_values TEXT`）而非关联表，因为：
- 报废物不会频繁修改自定义值
- 避免额外的 JOIN 查询复杂度
- JSON 格式与 XML 导出/导入天然兼容

### 4.7 数据库迁移策略

使用 `addColumnIfMissing()` 辅助函数：
- 启动时通过 `PRAGMA table_info` 检查列是否存在
- 不存在则执行 `ALTER TABLE ADD COLUMN`
- 所有迁移幂等，可安全重复运行

## 5. 数据库表结构

### `assets`

```sql
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
```

### `users`

```sql
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
```

### `field_presets`

```sql
CREATE TABLE IF NOT EXISTS field_presets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    field_key TEXT NOT NULL,
    field_value TEXT NOT NULL,
    sort_order INTEGER DEFAULT 0,
    abbr TEXT DEFAULT ''
);
```

### `custom_fields` & `custom_field_values`

```sql
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
```

### `transactions`

```sql
CREATE TABLE IF NOT EXISTS transactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    asset_id INTEGER,
    asset_tag TEXT DEFAULT '',
    action TEXT,
    operator TEXT,
    target_user TEXT DEFAULT '',
    notes TEXT DEFAULT '',
    created_at TEXT DEFAULT (datetime('now','localtime'))
);
```

### `attachments`

```sql
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
```

### `scrapped_assets`

```sql
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
    custom_values TEXT DEFAULT '',
    created_at TEXT DEFAULT (datetime('now','localtime')),
    updated_at TEXT DEFAULT (datetime('now','localtime'))
);
```

## 6. API 文档

### 公共（无需登录）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/login` | 登录，返回 token + user |
| POST | `/api/logout` | 登出，清除 session |

### 认证（需 Cookie `lasset_token`）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/me` | 获取当前登录用户信息 |
| GET | `/api/settings` | 获取设置（企业名称等） |
| PUT | `/api/settings` | 更新设置（仅 admin） |
| GET | `/api/assets` | 资产列表（分页、搜索、排序） |
| POST | `/api/assets` | 新增资产 |
| GET | `/api/assets/export` | 导出 CSV |
| GET | `/api/assets/template` | 下载导入模板 |
| POST | `/api/assets/batch-import` | CSV 批量导入（仅 admin） |
| GET | `/api/assets/{id}` | 资产详情 |
| PUT | `/api/assets/{id}` | 更新资产 |
| DELETE | `/api/assets/{id}` | 删除资产 |
| POST | `/api/assets/{id}/checkout` | 领用 |
| POST | `/api/assets/{id}/checkin` | 归还 |
| POST | `/api/assets/{id}/scrap` | 报废 |
| GET | `/api/users` | 用户列表（仅 admin） |
| POST | `/api/users` | 新增用户（仅 admin） |
| GET | `/api/users/{id}` | 用户详情 |
| GET | `/api/users/{id}/assets` | 用户领用的资产列表 |
| PUT | `/api/users/{id}` | 更新用户 |
| DELETE | `/api/users/{id}` | 删除用户 |
| GET | `/api/transactions` | 操作记录 |
| GET | `/api/default-fields` | 获取默认字段预设值 |
| GET | `/api/field-presets` | 获取字段预设列表 |
| GET | `/api/scrapped` | 报废资产列表 |
| GET | `/api/scrapped/{id}` | 报废资产详情 |
| POST | `/api/scrapped/{id}/restore` | 恢复报废资产 |
| GET | `/api/finance/summary` | 财务摘要（折旧计算） |
| GET | `/api/stats` | 统计数据 |
| POST | `/api/system/export-xml` | 导出 XML（仅 admin） |
| POST | `/api/system/import-xml` | 导入 XML（仅 admin） |
| POST | `/api/system/export-backup` | 导出 ZIP 备份（仅 admin） |
| POST | `/api/system/import-backup` | 导入 ZIP 备份（仅 admin） |

### 页面路由

| 路径 | 说明 | 权限 |
|---|---|---|
| `/login` | 登录页 | 公开 |
| `/` | 首页/概览 | 认证 |
| `/assets` | 资产列表 | 认证 |
| `/asset/{id}` | 资产详情 | 认证 |
| `/import` | 批量导入 | admin |
| `/fields` | 字段管理 | admin |
| `/users` | 用户管理 | admin |
| `/transactions` | 操作记录 | 认证 |
| `/scrapped` | 报废资产 | 认证 |
| `/scrapped/asset/{id}` | 报废资产详情 | 认证 |
| `/finance` | 财务管理 | 认证 |
| `/settings` | 设置 | 认证 |

## 7. 编译与部署

### Linux（NAS 运行）

```bash
cd /vol1/1000/WorkSpace/asset-manager
go build -o l-asset-new .
nohup ./l-asset-new > /tmp/lasset.log 2>&1 &
```

### Windows 打包

```bash
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o output/l-asset.exe .
cd tray && GOOS=windows GOARCH=amd64 CGO_ENABLED=1 go build -ldflags "-H windowsgui" -o ../output/l-asset-tray.exe .
```

### 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `LASSET_PORT` | `5678` | 监听端口 |
| `LASSET_DATA` | `./data` | 数据目录 |

### 启动方式

Linux：`nohup ./l-asset-new > /tmp/lasset.log 2>&1 &`  
Windows：双击 `l-asset-tray.exe`（系统托盘，无命令行窗口）

默认管理员账号：`admin / admin123`

## 8. 已知问题 / 待办

### 已知问题

1. **Tailwind CDN 警告** — 浏览器控制台显示 `cdn.tailwindcss.com should not be used in production`，功能和样式完全正常。如需移除，需自行托管 Tailwind CSS 文件或使用构建版本。
2. **XML/备份恢复时密码为空** — 导出不再包含密码哈希，导入时自动设置为默认密码 `123456`。管理员需在导入后手动修改密码。
3. **密码恢复** — 数据库直接改：`UPDATE users SET password='240be518fabd2724ddb6f04eeb1da5967448d7e831c08c8fa822809f74c720a9' WHERE name='admin'`（重置为 `admin123`）

### 待办（未来方向）

- [ ] systemd 服务自动启动
- [ ] Docker 容器化部署
- [ ] LDAP 用户同步
- [ ] 资产图片批量预览
- [ ] 审计日志导出
- [ ] 资产二维码标签打印
- [ ] Webhook 通知（资产变更时推送企业微信/钉钉）
- [ ] 登录限速（防暴力破解）
- [ ] CSRF token 保护
- [ ] 密码 bcrypt/argon2 哈希升级

---

*日志维护者：Hu（AI assistant）*  
*最后更新：2026-07-13 17:34 GMT+8*
