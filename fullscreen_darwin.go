//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>
#import <dispatch/dispatch.h>

// dshEnableNativeFullscreen opts the main window into macOS native fullscreen so
// the green traffic-light button toggles fullscreen instead of the default
// "zoom"/maximise behaviour.
//
// Wails v2 only sets NSWindowCollectionBehaviorFullScreenPrimary in
// applicationDidFinishLaunching when the window *starts* in fullscreen
// (options.App.Fullscreen). A normally-started window keeps the default
// collection behaviour, so its green button never enters fullscreen. This
// mirrors that same assignment, minus the toggleFullScreen: call.
static void dshEnableNativeFullscreen(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSApplication *app = [NSApplication sharedApplication];
        NSWindow *window = [app mainWindow];
        if (window == nil) {
            NSArray *windows = [app windows];
            if (windows.count > 0) {
                window = [windows firstObject];
            }
        }
        if (window != nil) {
            NSWindowCollectionBehavior behaviour = [window collectionBehavior];
            behaviour |= NSWindowCollectionBehaviorFullScreenPrimary;
            [window setCollectionBehavior:behaviour];
        }
    });
}
*/
import "C"

// enableNativeFullscreen opts the main window into macOS native fullscreen so
// the green traffic-light button toggles fullscreen. It dispatches to the main
// queue (AppKit is main-thread only) and is a no-op if no window exists yet.
func enableNativeFullscreen() {
	C.dshEnableNativeFullscreen()
}
