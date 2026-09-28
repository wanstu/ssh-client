# v0.4.0-rc.2 Release Candidate

候选日期：2026-09-28

本候选版只修正 v0.4.0-rc.1 RC 验收中发现的发布问题，不新增 SSH 业务功能。

## 修正

- Wails Desktop Kit 从 `v0.9.0` 升级到 `v0.10.0`。
- CI / Release 开启 `windows-installer: true`，Windows Release 应同时产出：
  - Portable EXE
  - ZIP
  - NSIS Setup EXE
  - 各资产对应的 SHA256
- 修正 `scripts/build.ps1` 独立运行时的版本元数据准备：
  - 普通本地构建使用 `dev / 0.0.0`。
  - 可通过 `APP_VERSION` 验证发布版本。
  - GitHub Actions 继续使用 workflow 注入的 tag 版本。
  - 本地临时修改的 build metadata 在构建后恢复，不污染工作区。

## 已验证

- `go test ./...`
- `go vet ./...`
- `node --check cmd/ssh-client-desktop/frontend/app.js`
- 本地 Windows build
- 发布型本地 Windows build：
  - `APP_VERSION=v0.4.0-rc.2`
  - FileVersion: `0.4.0`
  - ProductVersion: `0.4.0-rc.2`

## RC 验收重点

延续 v0.4.0-rc.1 的真实桌面/SSH 验收，并额外确认 Windows Setup 安装、覆盖安装、卸载及 Release checksum。
