// Package desktop defines the narrow desktop-host bridge. Domain, repository,
// application-service, HTTP-handler and templ packages must not import Wails
// (spec §4.2). A Wails webview loads the same Chi/templ application; this
// package holds only the optional OS capabilities the host may expose.
package desktop

import "context"

// Capabilities describes the host, matching the session capability document.
type Capabilities struct {
	Desktop           bool `json:"desktop"`
	NativeFileDialogs bool `json:"native_file_dialogs"`
	RevealFile        bool `json:"reveal_file"`
	LocalGPU          bool `json:"local_gpu"`
	LANAccess         bool `json:"lan_access"`
}

// Bridge is the optional desktop capability surface. Essential production
// actions always use HTTP routes; the bridge only adds OS conveniences.
type Bridge interface {
	// Capabilities reports what this host can do.
	Capabilities() Capabilities
	// RevealFile reveals an already-authorized project asset in the OS.
	RevealFile(ctx context.Context, path string) error
	// OpenExternal opens a URL in the user's browser.
	OpenExternal(ctx context.Context, url string) error
	// PreventSleep asks the OS to keep the machine awake during long renders.
	PreventSleep(ctx context.Context, enable bool) error
}

// BrowserBridge is the default host when running as an ordinary web app: no
// native capabilities are claimed, and the same routes serve every feature.
type BrowserBridge struct{}

// Capabilities returns the neutral browser host capabilities.
func (BrowserBridge) Capabilities() Capabilities { return Capabilities{Desktop: false} }

// RevealFile is unsupported in browser mode.
func (BrowserBridge) RevealFile(context.Context, string) error { return ErrUnsupported }

// OpenExternal is unnecessary in browser mode (navigation handles it).
func (BrowserBridge) OpenExternal(context.Context, string) error { return ErrUnsupported }

// PreventSleep is unsupported in browser mode.
func (BrowserBridge) PreventSleep(context.Context, bool) error { return ErrUnsupported }

// ErrUnsupported is returned when a capability is unavailable on this host.
var ErrUnsupported = errUnsupported{}

type errUnsupported struct{}

func (errUnsupported) Error() string { return "desktop: capability not supported by this host" }
