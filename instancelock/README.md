# instancelock

通用单实例检测工具（基于文件锁，key 相互隔离）。以 exe 形式作为"通用函数"分发：任意语言的宿主程序 spawn 本工具即可获得单实例保证，无需各自实现锁库。

## 输出协议（v1，冻结）

stdout 永远是单行合法 JSON（`--pretty` 时缩进）；stderr 只放人读警告；错误码体系只有 `bad_args` / `internal` 两种。

```json
// 成功包络
{"ok":true,"data":...}
// 失败包络
{"ok":false,"error":{"code":"bad_args|internal","message":"..."}}
```

### 退出码

| 码 | 含义 |
|---|---|
| 0 | 成功（含 help / version / list） |
| 3 | key 已被其他实例持有（**业务结果**，`ok:true`，非错误） |
| 1 | 参数或系统错误 |

### 各命令输出

**version**
```json
{"ok":true,"data":{"name":"instancelock","version":"1.0.0","protocol":1}}
```

**try**（快照检查，查完即走不持锁）
```json
// 空闲（exit 0）
{"ok":true,"data":{"key":"com.example.app","free":true}}
// 被持有（exit 3）；holder 信息缺失时为各字段零值
{"ok":true,"data":{"key":"com.example.app","free":false,"holder":{"pid":123,"host":"PC","started":"2026-01-01T12:00:00Z"}}}
```

**hold**（持锁模式：输出锁 JSON 后常驻阻塞，替宿主持有锁）
```json
// 拿锁成功（exit 0，输出后阻塞，宿主退出后释放）
{"ok":true,"data":{"key":"com.example.app","held":true,"pid":4567}}
// 被持有：同 try 的 free=false 形状（exit 3）
```

**list**
```json
{"ok":true,"data":{"entries":[{"key":"K","held":true,"pid":123,"host":"H","started":"...","file":"x-abc.lock"}]}}
```
空列表输出 `"entries":[]`（恒为数组，不为 null）。

**help / -h / --help / 无参数**：`{"ok":true,"data":{"usage":"...","commands":[...]}}`，exit 0。

### 宿主项目接入（hold 模式）

1. 启动时 spawn：`instancelock hold --key my-app [--ppid <宿主PID>]`
   （不给 `--ppid` 时保持子进程 stdin 管道不断开，宿主死后管道 EOF 自动释放锁）
2. 读子进程 stdout 的 JSON 行：`data.held=true` → 继续启动；退出码 3 → 宿主自行退出
3. 宿主无论正常退出、崩溃还是被 kill，锁均由操作系统兜底释放，不会死锁

## 冻结清单（版本纪律）

以下任何一项变更都构成 breaking change（major bump）：

1. **锁文件路径算法** `PathOf`（sha256 取前 12 位 hex + key sanitize）——**永不变更**：新旧版本混用时路径规则改变会导致互不可见对方的锁，单实例保证静默失效
2. **锁文件内容格式** `key=/pid=/host=/started=`（新增行向后兼容，属 minor）
3. **退出码语义**（0 / 3 / 1）
4. **stdout JSON schema**：字段删除/改名/语义变更 = major；新增可选字段 = minor
5. **tag 前缀** `instancelock/`

`version` 命令的 `protocol` 字段随 stdout 协议 breaking 变更 bump。

## 下载与发布

发布产物托管在 GitHub Releases（monorepo `td-go_projects`，tag 形如 `instancelock/vX.Y.Z`）：

```
https://github.com/shihao-hub/td-go_projects/releases/download/instancelock/v1.0.0/instancelock-windows-amd64.exe
```

- 下载后用同目录附件 `SHA256SUMS.txt` 校验完整性
- 本地构建：`scripts/build.ps1 [-Version X.Y.Z]`（默认 dev，产物在 `build/`）
- 发布新版本：`scripts/release.ps1 -Version X.Y.Z [-Notes "..."]`（构建 → tag → push → gh release create 一条龙）
- 宿主接入时建议启动后先跑 `instancelock version` 校验 `protocol` 字段，避免混用不兼容版本

## 源码阅读

### 接口设计

```python
def cmd_try(key:str, wait:int=0)->str:
    """
    """

def cmd_hold(key:str, wait:int=0,ppid:int=0)->str:
    """持锁模式，返回 {"ok":true,"data":{"held":true,"pid":N}} 后常驻阻塞
    主过程链路：
        1. 拿到 or 创建锁（实则是文件），接着判断是否被其他进程持有
        2. 未被持有则返回锁，被持有则睡眠 100ms 随后继续取锁（有超时时间），有任何错误就打印错误
        3. 拿到锁后写入持有者信息
        判活父进程：
            4. 创建 os.Signal 并通过 goroutine 每秒判断父进程是否还活着（存在风险）
            5. 通过 os.Stdin 阻塞判断父进程是否还活着
    """
```

### 锁语义

- Windows：`LockFileEx`（按句柄归属，字节区间锁）
- 其他平台：`flock`（按 open file description 归属）
- 两者对"同进程不同句柄重复加锁"均冲突，进程退出由 OS 兜底释放
