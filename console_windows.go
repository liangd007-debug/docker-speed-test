package main

import (
	"errors"
	"net"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleOutputCP  = kernel32.NewProc("GetConsoleOutputCP")
	procSetConsoleOutputCP  = kernel32.NewProc("SetConsoleOutputCP")
	procGetConsoleMode      = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode      = kernel32.NewProc("SetConsoleMode")
	procGetStdHandle        = kernel32.NewProc("GetStdHandle")
)

const (
	stdInputHandle  = uint32(0xFFFFFFF6) // -10
	stdOutputHandle = uint32(0xFFFFFFF5) // -11
	enableVTProcessing = 0x0004
)

// outEnc 控制台编码器：nil = UTF-8，否则按控制台代码页转码
var outEnc *encoding.Encoder

// origCP 进程首次看到的控制台代码页（只记一次，供 auto 模式用）
var origCP uint32

// initConsole(mode) 决定输出编码：
//   - "utf8"(默认): 输出 UTF-8 并把控制台钉到 65001 —— cmd/PowerShell/Windows
//     Terminal/git-bash/管道捕获 全都不乱码（重要：GBK 输出在 UTF-8 终端必乱码）
//   - "gbk": 输出 GBK 并钉 936（无控制台时退化 UTF-8；个别老终端用）
//   - "auto": 按进程启动时的原代码页（旧行为）
func initConsole(mode string) {
	if c, _, _ := procGetConsoleOutputCP.Call(); c != 0 && origCP == 0 {
		origCP = uint32(c)
	}
	cp := origCP
	switch strings.ToLower(mode) {
	case "gbk":
		if cp == 0 { // 没有控制台（管道/重定向）→ 一律 UTF-8
			outEnc = nil
			return
		}
		procSetConsoleOutputCP.Call(936)
		outEnc = simplifiedchinese.GBK.NewEncoder()
	case "auto":
		switch cp {
		case 0, 65001:
			outEnc = nil
		case 936:
			outEnc = simplifiedchinese.GBK.NewEncoder()
		case 950:
			outEnc = traditionalchinese.Big5.NewEncoder()
		case 932:
			outEnc = japanese.ShiftJIS.NewEncoder()
		case 949:
			outEnc = korean.EUCKR.NewEncoder()
		default:
			procSetConsoleOutputCP.Call(65001)
			outEnc = nil
		}
	default: // utf8
		if cp != 0 {
			procSetConsoleOutputCP.Call(65001)
		}
		outEnc = nil
	}
}

// enableColor 打开 Windows 10+ 控制台 VT(ANSI) 色彩支持，失败返回 false
func enableColor() bool {
	h, _, _ := procGetStdHandle.Call(uintptr(stdOutputHandle))
	if h == 0 {
		return false
	}
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return false
	}
	r, _, _ = procSetConsoleMode.Call(h, uintptr(mode|enableVTProcessing))
	return r != 0
}

// encode 把 UTF-8 字符串按控制台编码转成可写字节
func encode(s string) []byte {
	if outEnc == nil {
		return []byte(s)
	}
	b, err := outEnc.Bytes([]byte(s))
	if err != nil {
		// 含编码不了的字符(如 emoji)：逐字符替换为 '?'
		out := make([]byte, 0, len(s))
		for _, r := range s {
			one, e := outEnc.Bytes([]byte(string(r)))
			if e != nil || len(one) == 0 {
				out = append(out, '?')
			} else {
				out = append(out, one...)
			}
		}
		return out
	}
	return b
}

// isTerminal 判断标准句柄是否接在真实控制台上
func isTerminal(hid uint32) bool {
	h, _, _ := procGetStdHandle.Call(uintptr(hid))
	if h == 0 {
		return false
	}
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode)))
	return r != 0
}

func hasConsole() bool    { return isTerminal(stdOutputHandle) }
func stdinIsConsole() bool { return isTerminal(stdInputHandle) }

// classifyNetErr 网络错误 -> 中文短语
func classifyNetErr(err error) string {
	if err == nil {
		return ""
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "DNS解析失败"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return "连接被拒"
		}
		if errors.Is(err, syscall.ECONNRESET) {
			return "连接被重置"
		}
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "timeout") || strings.Contains(s, "deadline exceeded"):
		return "连接/读取超时"
	case strings.Contains(s, "no such host"):
		return "DNS解析失败"
	case strings.Contains(s, "connection refused"):
		return "连接被拒"
	case strings.Contains(s, "certificate") || strings.Contains(s, "x509"):
		return "证书错误"
	case strings.Contains(s, "EOF"):
		return "连接中断(EOF)"
	case strings.Contains(s, "context canceled"):
		return "请求被中断"
	}
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}
