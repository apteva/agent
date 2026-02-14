package operator

import (
	"strings"

	"github.com/chromedp/cdproto/input"
)

// KeyDef defines a CDP key with all fields needed for DispatchKeyEvent
type KeyDef struct {
	Key                  string // DOM key value (e.g., "Enter", "ArrowUp", "a")
	Code                 string // DOM code value (e.g., "Enter", "ArrowUp", "KeyA")
	KeyCode              int    // Legacy keyCode
	Text                 string // Character to insert (empty for non-printable keys)
	WindowsVirtualKeyCode int   // Windows virtual key code
}

// keyMap maps Claude's key names to CDP key definitions
var keyMap = map[string]KeyDef{
	// Navigation
	"Return":    {Key: "Enter", Code: "Enter", KeyCode: 13, Text: "\r", WindowsVirtualKeyCode: 13},
	"Enter":     {Key: "Enter", Code: "Enter", KeyCode: 13, Text: "\r", WindowsVirtualKeyCode: 13},
	"Tab":       {Key: "Tab", Code: "Tab", KeyCode: 9, Text: "\t", WindowsVirtualKeyCode: 9},
	"Escape":    {Key: "Escape", Code: "Escape", KeyCode: 27, WindowsVirtualKeyCode: 27},
	"space":     {Key: " ", Code: "Space", KeyCode: 32, Text: " ", WindowsVirtualKeyCode: 32},
	"Space":     {Key: " ", Code: "Space", KeyCode: 32, Text: " ", WindowsVirtualKeyCode: 32},

	// Editing
	"BackSpace": {Key: "Backspace", Code: "Backspace", KeyCode: 8, WindowsVirtualKeyCode: 8},
	"Backspace": {Key: "Backspace", Code: "Backspace", KeyCode: 8, WindowsVirtualKeyCode: 8},
	"Delete":    {Key: "Delete", Code: "Delete", KeyCode: 46, WindowsVirtualKeyCode: 46},
	"Insert":    {Key: "Insert", Code: "Insert", KeyCode: 45, WindowsVirtualKeyCode: 45},

	// Arrow keys
	"Up":    {Key: "ArrowUp", Code: "ArrowUp", KeyCode: 38, WindowsVirtualKeyCode: 38},
	"Down":  {Key: "ArrowDown", Code: "ArrowDown", KeyCode: 40, WindowsVirtualKeyCode: 40},
	"Left":  {Key: "ArrowLeft", Code: "ArrowLeft", KeyCode: 37, WindowsVirtualKeyCode: 37},
	"Right": {Key: "ArrowRight", Code: "ArrowRight", KeyCode: 39, WindowsVirtualKeyCode: 39},

	// Page navigation
	"Home":     {Key: "Home", Code: "Home", KeyCode: 36, WindowsVirtualKeyCode: 36},
	"End":      {Key: "End", Code: "End", KeyCode: 35, WindowsVirtualKeyCode: 35},
	"Page_Up":  {Key: "PageUp", Code: "PageUp", KeyCode: 33, WindowsVirtualKeyCode: 33},
	"PageUp":   {Key: "PageUp", Code: "PageUp", KeyCode: 33, WindowsVirtualKeyCode: 33},
	"Page_Down": {Key: "PageDown", Code: "PageDown", KeyCode: 34, WindowsVirtualKeyCode: 34},
	"PageDown":  {Key: "PageDown", Code: "PageDown", KeyCode: 34, WindowsVirtualKeyCode: 34},

	// Function keys
	"F1":  {Key: "F1", Code: "F1", KeyCode: 112, WindowsVirtualKeyCode: 112},
	"F2":  {Key: "F2", Code: "F2", KeyCode: 113, WindowsVirtualKeyCode: 113},
	"F3":  {Key: "F3", Code: "F3", KeyCode: 114, WindowsVirtualKeyCode: 114},
	"F4":  {Key: "F4", Code: "F4", KeyCode: 115, WindowsVirtualKeyCode: 115},
	"F5":  {Key: "F5", Code: "F5", KeyCode: 116, WindowsVirtualKeyCode: 116},
	"F6":  {Key: "F6", Code: "F6", KeyCode: 117, WindowsVirtualKeyCode: 117},
	"F7":  {Key: "F7", Code: "F7", KeyCode: 118, WindowsVirtualKeyCode: 118},
	"F8":  {Key: "F8", Code: "F8", KeyCode: 119, WindowsVirtualKeyCode: 119},
	"F9":  {Key: "F9", Code: "F9", KeyCode: 120, WindowsVirtualKeyCode: 120},
	"F10": {Key: "F10", Code: "F10", KeyCode: 121, WindowsVirtualKeyCode: 121},
	"F11": {Key: "F11", Code: "F11", KeyCode: 122, WindowsVirtualKeyCode: 122},
	"F12": {Key: "F12", Code: "F12", KeyCode: 123, WindowsVirtualKeyCode: 123},

	// Modifiers (standalone)
	"Shift_L":   {Key: "Shift", Code: "ShiftLeft", KeyCode: 16, WindowsVirtualKeyCode: 16},
	"Shift_R":   {Key: "Shift", Code: "ShiftRight", KeyCode: 16, WindowsVirtualKeyCode: 16},
	"Control_L": {Key: "Control", Code: "ControlLeft", KeyCode: 17, WindowsVirtualKeyCode: 17},
	"Control_R": {Key: "Control", Code: "ControlRight", KeyCode: 17, WindowsVirtualKeyCode: 17},
	"Alt_L":     {Key: "Alt", Code: "AltLeft", KeyCode: 18, WindowsVirtualKeyCode: 18},
	"Alt_R":     {Key: "Alt", Code: "AltRight", KeyCode: 18, WindowsVirtualKeyCode: 18},
	"Meta_L":    {Key: "Meta", Code: "MetaLeft", KeyCode: 91, WindowsVirtualKeyCode: 91},
	"Meta_R":    {Key: "Meta", Code: "MetaRight", KeyCode: 92, WindowsVirtualKeyCode: 92},
	"Super_L":   {Key: "Meta", Code: "MetaLeft", KeyCode: 91, WindowsVirtualKeyCode: 91},
}

// modifierNames maps modifier key names to CDP modifier flags
var modifierNames = map[string]input.Modifier{
	"ctrl":    input.ModifierCtrl,
	"control": input.ModifierCtrl,
	"alt":     input.ModifierAlt,
	"shift":   input.ModifierShift,
	"meta":    input.ModifierMeta,
	"cmd":     input.ModifierMeta,
	"command": input.ModifierMeta,
	"super":   input.ModifierMeta,
}

// ParseKeyCombo parses a key string like "ctrl+a", "Return", or "shift+Tab"
// into CDP modifier flags and a key definition.
func ParseKeyCombo(keyStr string) (input.Modifier, KeyDef, bool) {
	parts := strings.Split(keyStr, "+")
	var modifiers input.Modifier

	// Extract modifiers
	keyPart := parts[len(parts)-1]
	for _, part := range parts[:len(parts)-1] {
		if mod, ok := modifierNames[strings.ToLower(strings.TrimSpace(part))]; ok {
			modifiers |= mod
		}
	}

	keyPart = strings.TrimSpace(keyPart)

	// Look up the key in our map
	if def, ok := keyMap[keyPart]; ok {
		return modifiers, def, true
	}

	// Single character keys
	if len(keyPart) == 1 {
		ch := keyPart[0]
		code := "Key" + strings.ToUpper(keyPart)
		keyCode := int(strings.ToUpper(keyPart)[0])

		// For digits, use Digit prefix
		if ch >= '0' && ch <= '9' {
			code = "Digit" + keyPart
			keyCode = int(ch)
		}

		return modifiers, KeyDef{
			Key:                   keyPart,
			Code:                  code,
			KeyCode:               keyCode,
			Text:                  keyPart,
			WindowsVirtualKeyCode: keyCode,
		}, true
	}

	// Unknown key — return as-is
	return modifiers, KeyDef{Key: keyPart, Code: keyPart}, false
}
