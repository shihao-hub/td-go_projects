## 源码阅读

### 接口设计

```python
def cmd_try(key:str, wait:int=0)->str:
    """
    """

def cmd_hold(key:str, wait:int=0,ppid:int=0)->str:
    """持锁模式，返回 "LOCKED <pid>" 后常驻阻塞
    主过程链路：
        1. 拿到 or 创建锁（实则是文件），接着判断是否被其他进程持有
        2. 未被持有则返回锁，被持有则睡眠 100ms 随后继续取锁（有超时时间），有任何错误就打印错误
        3. 拿到锁后写入持有者信息
        判活父进程：
            4. 创建 os.Signal 并通过 goroutine 每秒判断父进程是否还活着（存在风险）
            5. 通过 os.Stdin 阻塞判断父进程是否还活着
    """
```
