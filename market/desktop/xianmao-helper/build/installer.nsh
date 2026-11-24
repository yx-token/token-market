; electron-builder 顺序：先 addDesktopLink / addStartMenuLink，再跑 customInstall。
; 因此这里不能 Delete 主快捷方式，否则装完桌面没有图标。
!macro customInstall
  ; 只清覆盖安装留下的重复快捷方式
  Delete "$DESKTOP\闲猫助手 (2).lnk"
  Delete "$DESKTOP\闲猫助手 (3).lnk"
  Delete "$SMPROGRAMS\闲猫助手 (2).lnk"
  Delete "$SMPROGRAMS\卸载闲猫助手.lnk"
  ; 开始菜单显式卸载入口
  CreateShortCut "$SMPROGRAMS\卸载闲猫助手.lnk" "$INSTDIR\Uninstall 闲猫助手.exe"
  ; 再确保桌面 / 开始菜单有主快捷方式（防止 keepShortcuts 等边界没建上）
  CreateShortCut "$DESKTOP\闲猫助手.lnk" "$appExe"
  CreateShortCut "$SMPROGRAMS\闲猫助手.lnk" "$appExe"
!macroend

!macro customUnInstall
  Delete "$DESKTOP\闲猫助手.lnk"
  Delete "$SMPROGRAMS\闲猫助手.lnk"
  Delete "$SMPROGRAMS\卸载闲猫助手.lnk"
!macroend
