//go:build windows

package windows

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32                  = windows.NewLazySystemDLL("ole32.dll")
	pCreateStreamOnHGlobal = ole32.NewProc("CreateStreamOnHGlobal")
)

type screenshotResult struct {
	data []byte
	err  error
}

func (f *Frontend) TakeScreenshot() ([]byte, error) {
	ch := make(chan screenshotResult, 1)
	var handler *capturePreviewHandler

	f.mainWindow.Invoke(func() {
		controller := f.chromium.GetController()
		if controller == nil {
			ch <- screenshotResult{err: fmt.Errorf("screenshot: webview controller not available")}
			return
		}

		wv, err := controller.GetCoreWebView2()
		if err != nil {
			ch <- screenshotResult{err: fmt.Errorf("screenshot: %w", err)}
			return
		}

		stream, err := createMemStream()
		if err != nil {
			ch <- screenshotResult{err: fmt.Errorf("screenshot: create stream: %w", err)}
			return
		}

		handler = newCaptureHandler(func(errCode uintptr) {
			defer releaseStream(stream)

			if windows.Handle(errCode) != windows.S_OK {
				ch <- screenshotResult{err: fmt.Errorf("screenshot: capture failed: %08x", errCode)}
				return
			}

			resetStream(stream)
			data, err := readStream(stream)
			if err != nil {
				ch <- screenshotResult{err: fmt.Errorf("screenshot: read stream: %w", err)}
				return
			}
			ch <- screenshotResult{data: data}
		})

		hr := callCapturePreview(unsafe.Pointer(wv), 0, stream, handler)
		if windows.Handle(hr) != windows.S_OK {
			releaseStream(stream)
			ch <- screenshotResult{err: fmt.Errorf("screenshot: CapturePreview call failed: %08x", hr)}
		}
	})

	result := <-ch
	runtime.KeepAlive(handler)
	return result.data, result.err
}

// IStream vtable offsets
const (
	istreamVtblRead = 3 // ISequentialStream::Read (QI=0, AddRef=1, Release=2, Read=3)
	istreamVtblSeek = 5 // IStream::Seek (Write=4, Seek=5)
	istreamVtblStat = 8 // IStream::Stat (SetSize=6, CopyTo=7, Stat=8)
)

func createMemStream() (uintptr, error) {
	var stream uintptr
	hr, _, _ := pCreateStreamOnHGlobal.Call(0, 1, uintptr(unsafe.Pointer(&stream)))
	if windows.Handle(hr) != windows.S_OK {
		return 0, fmt.Errorf("CreateStreamOnHGlobal failed: %08x", hr)
	}
	return stream, nil
}

func releaseStream(stream uintptr) {
	vtbl := *(*uintptr)(unsafe.Pointer(stream))
	release := *(*uintptr)(unsafe.Pointer(vtbl + 2*unsafe.Sizeof(uintptr(0)))) // Release = index 2
	syscall.SyscallN(release, stream)
}

func resetStream(stream uintptr) {
	vtbl := *(*uintptr)(unsafe.Pointer(stream))
	seek := *(*uintptr)(unsafe.Pointer(vtbl + istreamVtblSeek*unsafe.Sizeof(uintptr(0))))
	var newPos int64
	syscall.SyscallN(seek, stream, 0, 0 /* STREAM_SEEK_SET */, uintptr(unsafe.Pointer(&newPos)))
}

type statstg struct {
	pwcsName          *uint16
	dwType            uint32
	cbSize            int64
	mtime, ctime, atime [8]byte
	grfMode           uint32
	grfLocksSupported uint32
	clsid             [16]byte
	grfStateBits      uint32
	reserved          uint32
}

func readStream(stream uintptr) ([]byte, error) {
	vtbl := *(*uintptr)(unsafe.Pointer(stream))
	stat := *(*uintptr)(unsafe.Pointer(vtbl + istreamVtblStat*unsafe.Sizeof(uintptr(0))))
	read := *(*uintptr)(unsafe.Pointer(vtbl + istreamVtblRead*unsafe.Sizeof(uintptr(0))))

	var st statstg
	hr, _, _ := syscall.SyscallN(stat, stream, uintptr(unsafe.Pointer(&st)), 1 /* STATFLAG_NONAME */)
	if windows.Handle(hr) != windows.S_OK {
		return nil, fmt.Errorf("IStream::Stat failed: %08x", hr)
	}

	if st.cbSize <= 0 {
		return nil, fmt.Errorf("stream is empty")
	}

	buf := make([]byte, st.cbSize)
	var totalRead int
	for totalRead < len(buf) {
		var n uint32
		chunkSize := len(buf) - totalRead
		if chunkSize > 1<<20 {
			chunkSize = 1 << 20
		}
		syscall.SyscallN(read, stream,
			uintptr(unsafe.Pointer(&buf[totalRead])),
			uintptr(chunkSize),
			uintptr(unsafe.Pointer(&n)))
		if n == 0 {
			break
		}
		totalRead += int(n)
	}

	if totalRead == 0 {
		return nil, fmt.Errorf("no data read from stream")
	}

	return buf[:totalRead], nil
}

// CapturePreview vtable index on ICoreWebView2
// IUnknown(3) + 27 methods before CapturePreview = index 30
const capturePreviewVtblIndex = 30

func callCapturePreview(wv unsafe.Pointer, imageFormat uintptr, stream uintptr, handler *capturePreviewHandler) uintptr {
	wvPtr := uintptr(wv)
	vtbl := *(*uintptr)(unsafe.Pointer(wvPtr))
	method := *(*uintptr)(unsafe.Pointer(vtbl + capturePreviewVtblIndex*unsafe.Sizeof(uintptr(0))))
	hr, _, _ := syscall.SyscallN(method, wvPtr, imageFormat, stream, uintptr(unsafe.Pointer(handler)))
	return hr
}

type capturePreviewHandler struct {
	vtbl     *capturePreviewHandlerVtbl
	ref      int32
	callback func(errorCode uintptr)
}

type capturePreviewHandlerVtbl struct {
	queryInterface uintptr
	addRef         uintptr
	release        uintptr
	invoke         uintptr
}

var capturePreviewHandlerVtblInstance *capturePreviewHandlerVtbl

func init() {
	capturePreviewHandlerVtblInstance = &capturePreviewHandlerVtbl{
		queryInterface: syscall.NewCallback(captureHandlerQueryInterface),
		addRef:         syscall.NewCallback(captureHandlerAddRef),
		release:        syscall.NewCallback(captureHandlerRelease),
		invoke:         syscall.NewCallback(captureHandlerInvoke),
	}
}

func newCaptureHandler(callback func(errorCode uintptr)) *capturePreviewHandler {
	return &capturePreviewHandler{
		vtbl:     capturePreviewHandlerVtblInstance,
		ref:      1,
		callback: callback,
	}
}

func captureHandlerQueryInterface(this uintptr, refiid uintptr, object *uintptr) uintptr {
	*object = this
	return uintptr(windows.S_OK)
}

func captureHandlerAddRef(this uintptr) uintptr {
	return 1
}

func captureHandlerRelease(this uintptr) uintptr {
	return 0
}

func captureHandlerInvoke(this uintptr, errorCode uintptr) uintptr {
	handler := (*capturePreviewHandler)(unsafe.Pointer(this))
	if handler.callback != nil {
		handler.callback(errorCode)
	}
	return uintptr(windows.S_OK)
}
