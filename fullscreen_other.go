//go:build !darwin

package main

// enableNativeFullscreen is a no-op outside macOS; native fullscreen is a
// macOS traffic-light (green button) concept only.
func enableNativeFullscreen() {}
