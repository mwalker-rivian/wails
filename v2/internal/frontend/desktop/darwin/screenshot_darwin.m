#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import "WailsContext.h"

extern void processScreenshotResponse(const char* path, const char* error);

void TakeScreenshot(void *inctx, const char *outputPath) {
    WailsContext *ctx = (__bridge WailsContext*) inctx;
    NSString *nsPath = [NSString stringWithUTF8String:outputPath];

    dispatch_async(dispatch_get_main_queue(), ^{
        WKWebView *webView = ctx.webview;
        if (webView == nil) {
            processScreenshotResponse("", "webview not available");
            return;
        }

        WKSnapshotConfiguration *config = [[WKSnapshotConfiguration alloc] init];
        config.afterScreenUpdates = YES;

        CGFloat scale = webView.window.backingScaleFactor;
        if (scale < 1.0) scale = [[NSScreen mainScreen] backingScaleFactor];
        if (scale < 1.0) scale = 2.0;
        config.snapshotWidth = [NSNumber numberWithDouble:webView.bounds.size.width * scale];

        [webView takeSnapshotWithConfiguration:config completionHandler:^(NSImage *image, NSError *error) {
            [config release];

            if (error != nil || image == nil) {
                const char *errMsg = error ? [[error localizedDescription] UTF8String] : "snapshot returned nil image";
                processScreenshotResponse("", errMsg);
                return;
            }

            NSData *tiffData = [image TIFFRepresentation];
            if (tiffData == nil) {
                processScreenshotResponse("", "failed to get TIFF data from snapshot");
                return;
            }
            NSBitmapImageRep *rep = [[NSBitmapImageRep alloc] initWithData:tiffData];
            NSData *pngData = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
            [rep release];

            BOOL ok = [pngData writeToFile:nsPath atomically:YES];
            if (!ok) {
                processScreenshotResponse("", "failed to write PNG file");
                return;
            }

            processScreenshotResponse([nsPath UTF8String], "");
        }];
    });
}
