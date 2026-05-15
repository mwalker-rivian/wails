//go:build linux

package linux

/*
#cgo linux pkg-config: gtk+-3.0
#cgo !webkit2_41 pkg-config: webkit2gtk-4.0
#cgo webkit2_41 pkg-config: webkit2gtk-4.1

#include <gtk/gtk.h>
#include <webkit2/webkit2.h>
#include <cairo/cairo.h>
#include <stdlib.h>

extern void processLinuxScreenshotResponse(const char* path, const char* error);

typedef struct ScreenshotData {
    void *webview;
    char *outputPath;
} ScreenshotData;

static void snapshotReadyCallback(GObject *source_object, GAsyncResult *res, gpointer user_data) {
    ScreenshotData *data = (ScreenshotData *)user_data;
    GError *error = NULL;

    cairo_surface_t *surface = webkit_web_view_get_snapshot_finish(
        WEBKIT_WEB_VIEW(data->webview), res, &error);

    if (error != NULL) {
        processLinuxScreenshotResponse("", error->message);
        g_error_free(error);
        free(data->outputPath);
        free(data);
        return;
    }

    cairo_status_t status = cairo_surface_write_to_png(surface, data->outputPath);
    cairo_surface_destroy(surface);

    if (status != CAIRO_STATUS_SUCCESS) {
        processLinuxScreenshotResponse("", cairo_status_to_string(status));
    } else {
        processLinuxScreenshotResponse(data->outputPath, "");
    }

    free(data->outputPath);
    free(data);
}

static void takeWebViewSnapshot(void *webview, const char *outputPath) {
    ScreenshotData *data = malloc(sizeof(ScreenshotData));
    data->webview = webview;
    data->outputPath = strdup(outputPath);

    webkit_web_view_get_snapshot(
        WEBKIT_WEB_VIEW(webview),
        WEBKIT_SNAPSHOT_REGION_VISIBLE,
        WEBKIT_SNAPSHOT_OPTIONS_NONE,
        NULL,
        snapshotReadyCallback,
        data);
}
*/
import "C"

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"
)

type linuxScreenshotResult struct {
	path string
	err  string
}

var linuxScreenshotResponse = make(chan linuxScreenshotResult, 1)

//export processLinuxScreenshotResponse
func processLinuxScreenshotResponse(cpath *C.char, cerr *C.char) {
	linuxScreenshotResponse <- linuxScreenshotResult{
		path: C.GoString(cpath),
		err:  C.GoString(cerr),
	}
}

func (f *Frontend) TakeScreenshot() ([]byte, error) {
	tmpFile := filepath.Join(os.TempDir(), "wails_screenshot.png")

	cpath := C.CString(tmpFile)

	invokeOnMainThread(func() {
		C.takeWebViewSnapshot(f.mainWindow.webview, cpath)
		C.free(unsafe.Pointer(cpath))
	})

	result := <-linuxScreenshotResponse

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
