# 闲猫助手安装包（镜像内 `/app/helper/dist`）

发版流水线会把 `xianmao-helper-macos.zip`、`xianmao-helper-windows-*.zip|exe` 放到本目录后再构建镜像。

本地可手动：

```bash
cd market/desktop/xianmao-helper
bash scripts/pack-release.sh mac,win
cp -f dist/xianmao-helper-macos.zip dist/xianmao-helper-windows-*.zip dist/xianmao-helper-windows-setup.exe \
  ../../helper-packages/ 2>/dev/null || true
```
