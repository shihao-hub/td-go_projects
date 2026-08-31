# zread-tray

系统托盘常驻管理 **zread browse**：一键为选中目录启动/重启 zread 文档服务，
自动拉起浏览器阅读仓库生成的 wiki。

## 功能

- 启动时检测工作区是否已有 zread 文档，无文档可直接进入生成流程
- 托盘菜单：切换工作区 / 重启 zread / 查看日志 / 退出
- zread 异常退出自动重启，退出时结束子进程
- 日志落在 `%TEMP%\zread-tray.log`

## 用法

```
zread-tray.exe                     # 当前目录作为工作区
zread-tray.exe -dir D:\repo        # 指定工作区
zread-tray.exe -port 3456          # 服务端口
zread-tray.exe -generate           # 无文档时直接启动生成流程
zread-tray.exe -no-tray            # 控制台模式
```

## 依赖

先安装 zread CLI：

```
npm install -g zread
```
