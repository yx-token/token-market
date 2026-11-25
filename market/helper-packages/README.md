# 闲猫助手安装包（打进镜像）

发版流水线会把下列文件放到本目录后再构建镜像：

- `xianmao-helper-macos.zip`
- `xianmao-helper-windows-setup.exe`（Windows 安装包，必选）

浏览器下载时另存为「闲猫助手.zip / 闲猫助手.exe」。

本地手动放入：

```bash
cd market/desktop/xianmao-helper
# macOS
bash scripts/pack-release.sh mac
# Windows（须在 Windows 本机/CI）
bash scripts/pack-release.sh win
cp -f dist/xianmao-helper-macos.zip dist/xianmao-helper-windows-setup.exe \
  ../../helper-packages/
```

本目录只保留 `.gitkeep` 与本说明；安装包不进 Git。
