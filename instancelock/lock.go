package lock

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var ErrHeld = errors.New("held by another instance")

type Lock struct {
	f    *os.File
	Path string
}

type Info struct {
	Key     string
	PID     int
	Host    string
	Started time.Time
}

type Entry struct {
	Path string
	Held bool
	Info Info
}

func PathOf(key string) string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	sum := sha256.Sum256([]byte(key))
	h := hex.EncodeToString(sum[:])[:12]
	return filepath.Join(dir, "instancelock", sanitize(key)+"-"+h+".lock")
}

func sanitize(key string) string {
	var b strings.Builder
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	s := b.String()
	if s == "" {
		s = "key"
	}
	if len(s) > 24 {
		s = s[:24]
	}
	return s
}

func Try(key string) (*Lock, error) {
	p := PathOf(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockHandle(f); err != nil {
		f.Close()
		return nil, err
	}
	return &Lock{f: f, Path: p}, nil
}

func TryWait(key string, timeout time.Duration) (*Lock, error) {
	deadline := time.Now().Add(timeout)
	for {
		lk, err := Try(key)
		if err == nil {
			return lk, nil
		}
		if !errors.Is(err, ErrHeld) {
			return nil, err
		}
		if timeout <= 0 || !time.Now().Before(deadline) {
			return nil, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (l *Lock) WriteInfo(key string) error {
	host, _ := os.Hostname()
	content := fmt.Sprintf("key=%s\npid=%d\nhost=%s\nstarted=%s\n",
		key, os.Getpid(), host, time.Now().Format(time.RFC3339))
	if err := l.f.Truncate(0); err != nil {
		return err
	}
	_, err := l.f.WriteAt([]byte(content), 0)
	return err
}

func (l *Lock) Close() error {
	if l.f == nil {
		return nil
	}
	err1 := unlockHandle(l.f)
	err2 := l.f.Close()
	l.f = nil
	if err1 != nil {
		return err1
	}
	return err2
}

func ReadInfo(path string) (Info, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Info{}, err
	}
	var info Info
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "key="); ok {
			info.Key = v
		} else if v, ok := strings.CutPrefix(line, "pid="); ok {
			info.PID, _ = strconv.Atoi(v)
		} else if v, ok := strings.CutPrefix(line, "host="); ok {
			info.Host = v
		} else if v, ok := strings.CutPrefix(line, "started="); ok {
			info.Started, _ = time.Parse(time.RFC3339, v)
		}
	}
	return info, nil
}

func List() ([]Entry, error) {
	dir := filepath.Dir(PathOf("x"))
	paths, err := filepath.Glob(filepath.Join(dir, "*.lock"))
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, p := range paths {
		e := Entry{Path: p}
		if info, err := ReadInfo(p); err == nil {
			e.Info = info
		}
		f, err := os.OpenFile(p, os.O_RDWR, 0o600)
		if err == nil {
			if lockHandle(f) == nil {
				_ = unlockHandle(f)
				_ = f.Close()
				entries = append(entries, e)
				continue
			}
			_ = f.Close()
			e.Held = true
		}
		entries = append(entries, e)
	}
	return entries, nil
}
