//go:build windows

package platform

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"duoreader/internal/app"
	webview "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

var user = windows.NewLazySystemDLL("user32.dll")
var enumMonitors = user.NewProc("EnumDisplayMonitors")
var getMonitorInfo = user.NewProc("GetMonitorInfoW")
var setPos = user.NewProc("SetWindowPos")
var setStyle = user.NewProc("SetWindowLongPtrW")
var getRect = user.NewProc("GetWindowRect")
var showWindow = user.NewProc("ShowWindow")
var callProc = user.NewProc("CallWindowProcW")

type rect struct{ Left, Top, Right, Bottom int32 }
type monitorInfo struct {
	Size       uint32
	Rect, Work rect
	Flags      uint32
	Device     [32]uint16
}
type monitor struct {
	Name   string
	Bounds rect
}

func monitors() []monitor {
	var result []monitor
	cb := syscall.NewCallback(func(h, dc, p, data uintptr) uintptr {
		info := monitorInfo{}
		info.Size = uint32(unsafe.Sizeof(info))
		if ok, _, _ := getMonitorInfo.Call(h, uintptr(unsafe.Pointer(&info))); ok != 0 {
			result = append(result, monitor{windows.UTF16ToString(info.Device[:]), info.Rect})
		}
		return 1
	})
	enumMonitors.Call(0, 0, cb, 0)
	sort.Slice(result, func(i, j int) bool {
		var a, b int
		fmt.Sscanf(strings.TrimPrefix(result[i].Name, `\\.\DISPLAY`), "%d", &a)
		fmt.Sscanf(strings.TrimPrefix(result[j].Name, `\\.\DISPLAY`), "%d", &b)
		return a < b
	})
	return result
}

type nativeWindow struct {
	view     webview.WebView
	index    int
	full     bool
	saved    rect
	url      string
	lastRole string
}

var procMu sync.Mutex
var windowsByHandle = map[uintptr]*nativeWindow{}
var oldProcs = map[uintptr]uintptr{}

func subclass(hwnd, msg, wp, lp uintptr) uintptr {
	procMu.Lock()
	n := windowsByHandle[hwnd]
	old := oldProcs[hwnd]
	procMu.Unlock()
	if n != nil {
		switch msg {
		case 0x007E: // WM_DISPLAYCHANGE
			n.view.Dispatch(func() { n.place(false) })
		case 0x02E0: // WM_DPICHANGED
			suggested := *(*rect)(unsafe.Pointer(lp))
			setPos.Call(hwnd, 0, uintptr(suggested.Left), uintptr(suggested.Top), uintptr(suggested.Right-suggested.Left), uintptr(suggested.Bottom-suggested.Top), 0x14)
			if n.full {
				n.view.Dispatch(func() { n.place(false) })
			}
		}
	}
	r, _, _ := callProc.Call(old, hwnd, msg, wp, lp)
	return r
}
func (n *nativeWindow) place(toggle bool) {
	mons := monitors()
	if len(mons) == 0 {
		return
	}
	h := uintptr(n.view.Window())
	role := "combined"
	if len(mons) > 1 {
		if n.index == 0 {
			role = "left"
		} else {
			role = "right"
		}
	}
	if n.index == 1 && len(mons) < 2 {
		showWindow.Call(h, 0)
		return
	}
	showWindow.Call(h, 5)
	if role != n.lastRole {
		n.lastRole = role
		n.view.Navigate(n.url + "&native=1&side=" + role)
	}
	if toggle {
		n.full = !n.full
		if n.full {
			getRect.Call(h, uintptr(unsafe.Pointer(&n.saved)))
		}
	}
	if n.full {
		m := mons[min(n.index, len(mons)-1)].Bounds
		setStyle.Call(h, ^uintptr(15), 0x90000000) // GWL_STYLE / WS_POPUP|WS_VISIBLE
		setPos.Call(h, 0, uintptr(m.Left), uintptr(m.Top), uintptr(m.Right-m.Left), uintptr(m.Bottom-m.Top), 0x34)
	} else if toggle {
		setStyle.Call(h, ^uintptr(15), 0x10CF0000)
		r := n.saved
		setPos.Call(h, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), 0x34)
	} else {
		m := mons[min(n.index, len(mons)-1)].Bounds
		setPos.Call(h, 0, uintptr(m.Left+24), uintptr(m.Top+24), uintptr(min(int32(1200), m.Right-m.Left-48)), uintptr(min(int32(900), m.Bottom-m.Top-48)), 0x14)
	}
}
func Run(url, data string, s *app.Server) error {
	// Configure before creating any HWND. Use an embedded manifest for release packaging too.
	if p := user.NewProc("SetProcessDpiAwarenessContext"); p.Find() == nil {
		p.Call(^uintptr(3))
	}
	count := 2
	ready := make(chan *nativeWindow, 2)
	done := make(chan struct{}, 2)
	for i := 0; i < count; i++ {
		go func(index int) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			ole := windows.NewLazySystemDLL("ole32.dll")
			ole.NewProc("CoInitializeEx").Call(0, 2)
			defer ole.NewProc("CoUninitialize").Call()
			v := webview.NewWithOptions(webview.WebViewOptions{AutoFocus: true, DataPath: filepath.Join(data, fmt.Sprintf("webview-%d", index)), WindowOptions: webview.WindowOptions{Title: fmt.Sprintf("Duo Reader — экран %d", index+1), Width: 1100, Height: 850}})
			if v == nil {
				ready <- nil
				done <- struct{}{}
				return
			}
			n := &nativeWindow{view: v, index: index, url: url}
			h := uintptr(v.Window())
			callback := syscall.NewCallback(subclass)
			procMu.Lock()
			windowsByHandle[h] = n
			old, _, _ := setStyle.Call(h, ^uintptr(3), callback)
			oldProcs[h] = old
			procMu.Unlock()
			n.place(false)
			ready <- n
			v.Run()
			done <- struct{}{}
		}(i)
	}
	var views []*nativeWindow
	for i := 0; i < count; i++ {
		if n := <-ready; n != nil {
			views = append(views, n)
		}
	}
	if len(views) == 0 {
		return fmt.Errorf("не удалось создать WebView2; установите Microsoft Edge WebView2 Runtime")
	}
	s.SetFullscreen(func(exit bool) {
		for _, n := range views {
			n := n
			n.view.Dispatch(func() {
				if !exit || n.full {
					n.place(true)
				}
			})
		}
	})
	defer s.SetFullscreen(nil)
	<-done
	for _, n := range views {
		n.view.Dispatch(func() { n.view.Destroy(); n.view.Terminate() })
	}
	return nil
}
