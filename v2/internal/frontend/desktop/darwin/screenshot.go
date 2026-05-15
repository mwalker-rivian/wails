//go:build darwin

package darwin

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework Cocoa -framework WebKit
#import <Foundation/Foundation.h>
#import "WailsContext.h"

#include <stdlib.h>

extern void TakeScreenshot(void *inctx, const char *outputPath);
*/
import "C"

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"
)

var screenshotResponse = make(chan screenshotResult, 1)

type screenshotResult struct {
	path string
	err  string
}

//export processScreenshotResponse
func processScreenshotResponse(cpath *C.char, cerr *C.char) {
	screenshotResponse <- screenshotResult{
		path: C.GoString(cpath),
		err:  C.GoString(cerr),
	}
}

func (f *Frontend) TakeScreenshot() ([]byte, error) {
	tmpDir := os.TempDir()
	tmpFile := filepath.Join(tmpDir, "wails_screenshot.png")

	cpath := C.CString(tmpFile)
	C.TakeScreenshot(f.mainWindow.context, cpath)
	C.free(unsafe.Pointer(cpath))

	result := <-screenshotResponse

	if result.err != "" {
		return nil, fmt.Errorf("screenshot failed: %s", result.err)
	}

	data, err := os.ReadFile(result.path)
	if err != nil {
		return nil, fmt.Errorf("reading screenshot: %w", err)
	}

	os.Remove(result.path)
	return data, nil
}
