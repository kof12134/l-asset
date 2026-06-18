# L-Asset 资产管理系统 — 项目日志

**作者：** 乐为爸爸  
**技术顾问：** Hu（AI assistant）  
**编程语言：** Go 1.21+（编译期 Go 1.26.4）  
**数据库：** SQLite（通过 modernc.org/sqlite 纯 Go 驱动）  
**前端：** Alpine.js 3.x + Tailwind CSS（CDN）  
**当前源码：** `/vol1/1000/WorkSpace/asset-manager/main.go`（~1750 行）  
**更新日期：** 2026-06-16

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
- 数据导出（CSV）

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
    sort_order INTEGER DEFAULT 0
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
| GET | `/api/users` | 用户列表 |
| POST | `/api/users` | 新增用户 |
| GET | `/api/users/{id}` | 用户详情 |
| GET | `/api/users/{id}/assets` | 用户领用的资产列表 |
| PUT | `/api/users/{id}` | 更新用户 |
| DELETE | `/api/users/{id}` | 删除用户 |
| GET | `/api/transactions` | 操作记录 |
| GET | `/api/default-fields` | 获取默认字段预设值 |
| GET | `/api/field-presets` | 获取字段预设列表 |

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
GOOS=windows GOARCH=amd64 go build -o output/l-asset.exe .
```

### 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `LASSET_PORT` | `5678` | 监听端口 |
| `LASSET_DATA` | `./data` | 数据目录 |

### 启动方式

Linux：`nohup ./l-asset-new > /tmp/lasset.log 2>&1 &`  
Windows：双击 `start.bat`

默认管理员账号：`admin / admin123`

## 8. 已知问题 / 待办

### 已知问题

1. **Tailwind CDN 警告** — 浏览器控制台显示 `cdn.tailwindcss.com should not be used in production`，功能和样式完全正常。如需移除，需自行托管 Tailwind CSS 文件或使用构建版本。
2. **admin 密码在测试过程中多次被覆盖** — 根本原因是前端编辑用户时 `role` 和 `active` 字段缺失。已修复：后端做空值保留处理。
3. **密码恢复** — 数据库直接改：`UPDATE users SET password='240be518fabd2724ddb6f04eeb1da5967448d7e831c08c8fa822809f74c720a9' WHERE name='admin'`（重置为 `admin123`）

### 待办（未来方向）

- [ ] systemd 服务自动启动
- [ ] Docker 容器化部署
- [ ] 资产图片上传
- [ ] 审计日志导出
- [ ] LDAP 用户同步
- [ ] 资产二维码标签打印

---

*日志维护者：Hu（AI assistant）*  
*最后更新：2026-06-16 23:45 GMT+8*
