package operator

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/chromedp/cdproto/input"
	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// ConnectCDP establishes a CDP WebSocket connection for a session.
// Called after a provider returns a ConnectURL.
func ConnectCDP(parentCtx context.Context, session *BrowserSession) error {
	if session.ConnectURL == "" {
		return fmt.Errorf("session %s has no CDP connect URL", session.ID)
	}

	// chromedp requires explicit port in WebSocket URLs — add default ports if missing
	connectURL := ensureWSPort(session.ConnectURL)
	log.Printf("🔌 Connecting to CDP: %s", connectURL)

	// chromedp.NewRemoteAllocator connects to an existing browser via WebSocket.
	// NoModifyURL prevents chromedp from fetching /json/version (which fails for
	// cloud providers like Steel/Browserbase that expose a direct WebSocket endpoint).
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(parentCtx, connectURL, chromedp.NoModifyURL)

	// Create a chromedp context from the remote allocator
	cdpCtx, cdpCancel := chromedp.NewContext(allocCtx)

	// Verify connection works with a simple evaluation.
	// IMPORTANT: We must use cdpCtx (not a derived timeout context) for the first
	// chromedp.Run call, because chromedp allocates the browser WebSocket using the
	// provided context. If we used a timeout context, canceling it after verification
	// would kill the browser's WebSocket connection, causing all subsequent commands
	// to fail with "context canceled".
	verifyDone := make(chan error, 1)
	go func() {
		var result interface{}
		verifyDone <- chromedp.Run(cdpCtx, chromedp.Evaluate("1+1", &result))
	}()

	select {
	case err := <-verifyDone:
		if err != nil {
			cdpCancel()
			allocCancel()
			return fmt.Errorf("CDP connection verification failed: %w", err)
		}
	case <-time.After(10 * time.Second):
		cdpCancel()
		allocCancel()
		return fmt.Errorf("CDP connection verification timed out after 10s")
	}

	// Store combined cancel
	session.SetCDP(cdpCtx, func() {
		cdpCancel()
		allocCancel()
	})

	log.Printf("✅ CDP connected to session %s", session.ID)
	return nil
}

// ExecuteCDPCommand runs a browser action via CDP WebSocket.
// Returns results in the same format as REST commands for transparent routing.
func ExecuteCDPCommand(ctx context.Context, session *BrowserSession, cmdType string, params map[string]interface{}) (map[string]interface{}, error) {
	cdpCtx := session.GetCDPContext()
	if cdpCtx == nil {
		return nil, fmt.Errorf("session %s has no active CDP context", session.ID)
	}

	switch cmdType {
	case "screenshot":
		return cdpScreenshot(cdpCtx, params)
	case "click":
		return cdpClick(cdpCtx, params, input.Left, 1)
	case "right_click":
		return cdpClick(cdpCtx, params, input.Right, 1)
	case "middle_click":
		return cdpClick(cdpCtx, params, input.Middle, 1)
	case "double_click":
		return cdpClick(cdpCtx, params, input.Left, 2)
	case "triple_click":
		return cdpClick(cdpCtx, params, input.Left, 3)
	case "mouse_move":
		return cdpMouseMove(cdpCtx, params)
	case "mouse_down":
		return cdpMouseDown(cdpCtx, params)
	case "mouse_up":
		return cdpMouseUp(cdpCtx, params)
	case "left_click_drag":
		return cdpDrag(cdpCtx, params)
	case "type":
		return cdpType(cdpCtx, params)
	case "key":
		return cdpKey(cdpCtx, params)
	case "hold_key":
		return cdpHoldKey(cdpCtx, params)
	case "scroll":
		return cdpScroll(cdpCtx, params)
	case "navigate":
		return cdpNavigate(cdpCtx, params)
	default:
		return nil, fmt.Errorf("unsupported CDP command: %s", cmdType)
	}
}

func cdpScreenshot(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	fullPage, _ := params["full_page"].(bool)

	var buf []byte
	var err error

	if fullPage {
		quality := 80
		if q, ok := params["quality"].(int); ok {
			quality = q
		}
		err = chromedp.Run(ctx, chromedp.FullScreenshot(&buf, quality))
	} else {
		err = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			var captureErr error
			buf, captureErr = cdppage.CaptureScreenshot().
				WithFormat(cdppage.CaptureScreenshotFormatPng).
				Do(ctx)
			return captureErr
		}))
	}
	if err != nil {
		return nil, fmt.Errorf("CDP screenshot failed: %w", err)
	}

	b64 := base64.StdEncoding.EncodeToString(buf)
	return map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"image":  b64,
			"format": "png",
		},
	}, nil
}

func cdpClick(ctx context.Context, params map[string]interface{}, button input.MouseButton, clickCount int) (map[string]interface{}, error) {
	x := toFloat64(params["x"])
	y := toFloat64(params["y"])

	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		// Move mouse to position first
		if err := input.DispatchMouseEvent(input.MouseMoved, x, y).Do(ctx); err != nil {
			return err
		}
		// Press
		p := input.DispatchMouseEvent(input.MousePressed, x, y).
			WithButton(button).
			WithClickCount(int64(clickCount))
		if err := p.Do(ctx); err != nil {
			return err
		}
		// Release
		r := input.DispatchMouseEvent(input.MouseReleased, x, y).
			WithButton(button).
			WithClickCount(int64(clickCount))
		return r.Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("CDP click failed: %w", err)
	}
	return map[string]interface{}{"success": true}, nil
}

func cdpMouseMove(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	x := toFloat64(params["x"])
	y := toFloat64(params["y"])

	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return input.DispatchMouseEvent(input.MouseMoved, x, y).Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("CDP mouse move failed: %w", err)
	}
	return map[string]interface{}{"success": true}, nil
}

func cdpMouseDown(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	x := toFloat64(params["x"])
	y := toFloat64(params["y"])
	button := input.Left
	if b, ok := params["button"].(string); ok && b == "right" {
		button = input.Right
	}

	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return input.DispatchMouseEvent(input.MousePressed, x, y).
			WithButton(button).
			WithClickCount(1).
			Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("CDP mouse down failed: %w", err)
	}
	return map[string]interface{}{"success": true}, nil
}

func cdpMouseUp(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	x := toFloat64(params["x"])
	y := toFloat64(params["y"])
	button := input.Left
	if b, ok := params["button"].(string); ok && b == "right" {
		button = input.Right
	}

	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return input.DispatchMouseEvent(input.MouseReleased, x, y).
			WithButton(button).
			WithClickCount(1).
			Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("CDP mouse up failed: %w", err)
	}
	return map[string]interface{}{"success": true}, nil
}

func cdpDrag(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	startX := toFloat64(params["start_x"])
	startY := toFloat64(params["start_y"])
	endX := toFloat64(params["end_x"])
	endY := toFloat64(params["end_y"])

	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		// Move to start
		if err := input.DispatchMouseEvent(input.MouseMoved, startX, startY).Do(ctx); err != nil {
			return err
		}
		// Press at start
		if err := input.DispatchMouseEvent(input.MousePressed, startX, startY).
			WithButton(input.Left).
			WithClickCount(1).
			Do(ctx); err != nil {
			return err
		}
		// Move to end (with intermediate steps for smoother drag)
		steps := 10
		for i := 1; i <= steps; i++ {
			ratio := float64(i) / float64(steps)
			mx := startX + (endX-startX)*ratio
			my := startY + (endY-startY)*ratio
			if err := input.DispatchMouseEvent(input.MouseMoved, mx, my).
				WithButton(input.Left).
				Do(ctx); err != nil {
				return err
			}
		}
		// Release at end
		return input.DispatchMouseEvent(input.MouseReleased, endX, endY).
			WithButton(input.Left).
			WithClickCount(1).
			Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("CDP drag failed: %w", err)
	}
	return map[string]interface{}{"success": true}, nil
}

func cdpType(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	text, _ := params["text"].(string)
	if text == "" {
		return map[string]interface{}{"success": true}, nil
	}

	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		// Use InsertText for efficiency — inserts text as if typed
		return input.InsertText(text).Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("CDP type failed: %w", err)
	}
	return map[string]interface{}{"success": true}, nil
}

func cdpKey(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	keyStr, _ := params["key"].(string)
	if keyStr == "" {
		return nil, fmt.Errorf("empty key parameter")
	}

	modifiers, keyDef, _ := ParseKeyCombo(keyStr)

	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		// Key down
		down := input.DispatchKeyEvent(input.KeyDown).
			WithKey(keyDef.Key).
			WithCode(keyDef.Code).
			WithWindowsVirtualKeyCode(int64(keyDef.WindowsVirtualKeyCode)).
			WithNativeVirtualKeyCode(int64(keyDef.WindowsVirtualKeyCode)).
			WithModifiers(modifiers)
		if keyDef.Text != "" {
			down = down.WithText(keyDef.Text)
		}
		if err := down.Do(ctx); err != nil {
			return err
		}

		// Key up
		up := input.DispatchKeyEvent(input.KeyUp).
			WithKey(keyDef.Key).
			WithCode(keyDef.Code).
			WithWindowsVirtualKeyCode(int64(keyDef.WindowsVirtualKeyCode)).
			WithNativeVirtualKeyCode(int64(keyDef.WindowsVirtualKeyCode)).
			WithModifiers(modifiers)
		return up.Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("CDP key failed: %w", err)
	}
	return map[string]interface{}{"success": true}, nil
}

func cdpHoldKey(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	keyStr, _ := params["key"].(string)
	if keyStr == "" {
		return nil, fmt.Errorf("empty key parameter for hold_key")
	}

	_, keyDef, _ := ParseKeyCombo(keyStr)

	// Determine modifier flag for the held key
	var holdModifier input.Modifier
	switch keyDef.Key {
	case "Control":
		holdModifier = input.ModifierCtrl
	case "Alt":
		holdModifier = input.ModifierAlt
	case "Shift":
		holdModifier = input.ModifierShift
	case "Meta":
		holdModifier = input.ModifierMeta
	}

	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		// Press the hold key
		if err := input.DispatchKeyEvent(input.KeyDown).
			WithKey(keyDef.Key).
			WithCode(keyDef.Code).
			WithModifiers(holdModifier).
			Do(ctx); err != nil {
			return err
		}

		// Perform sub-action if specified
		if subAction, ok := params["action"].(string); ok {
			x := toFloat64(params["x"])
			y := toFloat64(params["y"])
			switch subAction {
			case "left_click", "click":
				if err := input.DispatchMouseEvent(input.MousePressed, x, y).
					WithButton(input.Left).
					WithClickCount(1).
					WithModifiers(holdModifier).
					Do(ctx); err != nil {
					return err
				}
				if err := input.DispatchMouseEvent(input.MouseReleased, x, y).
					WithButton(input.Left).
					WithModifiers(holdModifier).
					Do(ctx); err != nil {
					return err
				}
			}
		}

		// Release the hold key
		return input.DispatchKeyEvent(input.KeyUp).
			WithKey(keyDef.Key).
			WithCode(keyDef.Code).
			Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("CDP hold_key failed: %w", err)
	}
	return map[string]interface{}{"success": true}, nil
}

func cdpScroll(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	x := toFloat64(params["x"])
	y := toFloat64(params["y"])
	deltaX := toFloat64(params["delta_x"])
	deltaY := toFloat64(params["delta_y"])

	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return input.DispatchMouseEvent(input.MouseWheel, x, y).
			WithDeltaX(deltaX).
			WithDeltaY(deltaY).
			Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("CDP scroll failed: %w", err)
	}
	return map[string]interface{}{"success": true}, nil
}

func cdpNavigate(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	navURL, _ := params["url"].(string)
	if navURL == "" {
		return nil, fmt.Errorf("empty URL for navigate")
	}

	// Use page.Navigate directly instead of chromedp.Navigate.
	// chromedp.Navigate waits for the page load lifecycle event, which can
	// fail with "context canceled" on remote browser providers (Steel, Browserbase).
	// page.Navigate just sends the navigation command without blocking on load.
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, errText, _, err := cdppage.Navigate(navURL).Do(ctx)
		if err == nil && errText != "" {
			return fmt.Errorf("navigation error: %s", errText)
		}
		return err
	}))
	if err != nil {
		return nil, fmt.Errorf("CDP navigate failed: %w", err)
	}
	return map[string]interface{}{"success": true, "url": navURL}, nil
}

// toFloat64 converts interface{} to float64 for CDP coordinate parameters
func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}

// ensureWSPort adds default port to WebSocket URLs if missing.
// chromedp fails with "missing port in address" for URLs like wss://host?params
func ensureWSPort(wsURL string) string {
	parsed, err := url.Parse(wsURL)
	if err != nil {
		return wsURL
	}
	if parsed.Port() == "" {
		switch parsed.Scheme {
		case "wss":
			parsed.Host = parsed.Hostname() + ":443"
		case "ws":
			parsed.Host = parsed.Hostname() + ":80"
		}
		return parsed.String()
	}
	return wsURL
}
