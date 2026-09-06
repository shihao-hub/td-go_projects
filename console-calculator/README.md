# console-calculator

控制台四则运算计算器，手写词法分析 + 表达式求值，功能详见 [console-calculator.md](console-calculator.md)。

## 构建

```powershell
.\build.ps1    # 等价于：go build -trimpath -ldflags "-s -w" -o console-calculator.exe .
```

控制台程序（不加 `-H windowsgui`），产物在项目根，可从任意目录执行脚本。
