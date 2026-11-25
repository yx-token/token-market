# 闲猫助手（xianmao-helper）

处理 `xianmao://one-click` 协议：用短时 ticket 向市场换 Key，自动下载安装对应 Agent、写入连接配置并打开客户端（不再依赖 CC Switch）。

## 支持系统

| 系统 | 要求 | 安装包 |
|------|------|--------|
| macOS | 11+（Intel / Apple Silicon） | `.zip` / `.dmg` |
| Windows | 10 / 11 x64 | NSIS 安装包 `.exe`（由 `windows-latest` CI 产出） |
| Linux | x64（可选） | `.AppImage` |

## 站点地址

正式安装包**固定**连接 `https://idlicat.com`，不要求（也不允许）用户填写网站地址。启动时会清掉历史缓存的局域网 IP。

本地联调：启动前设置环境变量覆盖：

```bash
export XIANMAO_SITE=http://127.0.0.1:8080
npm start
```

## 开发

本机需 Node 18+。首次：

```bash
cd market/desktop/xianmao-helper
npm install
npm start
```

注册协议后，可用：

```text
xianmao://one-click?ticket=...&client=codex&os=macos&api=http://127.0.0.1:8080
```

`ticket` 由已登录买家调用 `POST /api/buyer/one-click/tickets` 签发；助手调用 `POST /api/one-click/exchange` 一次性换取密钥（协议里从不带 Key）。

## 打包（上线下载）

```bash
# mac + win（推荐）
bash scripts/pack-release.sh mac,win

# 或分别
npm run dist:mac
npm run dist:win
```

产物（下载 API 认这些稳定文件名）：

- `dist/xianmao-helper-macos.zip`
- `dist/xianmao-helper-windows-setup.exe`（Windows 安装包；请在 Windows 本机 / CI `windows-latest` 打包）
- `dist/xianmao-helper-windows-x64.zip`（可选便携包）
- 浏览器下载名：`闲猫助手.exe` / `闲猫助手.zip`
- `dist/xianmao-helper-linux-x64.AppImage`（可选）

站点接口：

- `GET /api/one-click/helper/download/macos`
- `GET /api/one-click/helper/download/windows`
- `GET /api/one-click/helper/download/linux`

## 与网页的关系

- 未装助手：网页下载对应系统安装包，用户打开安装；装好后自动探测连接。
- 本机开发：`POST /api/one-click/helper/launch` 可直接拉起开发版助手。
- 已装助手：网页唤起 `xianmao://`；助手负责装 Agent、写配置、打开。
