package lock

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// TextLocker implements a screen locker that requires typing a specific text to unlock.
type TextLocker struct {
	conn   *xgb.Conn
	screen *xproto.ScreenInfo
	window xproto.Window
	font   xproto.Font

	targetText string
	typedText  string
	message    string

	mu       sync.Mutex
	running  bool
	stopChan chan struct{}

	// Keyboard mapping
	minKeycode xproto.Keycode
	maxKeycode xproto.Keycode
	keysyms    []xproto.Keysym
}

// TextLockConfig holds configuration for the text-based locker.
type TextLockConfig struct {
	// TargetText is the text that must be typed to unlock.
	TargetText string
	// Message is displayed above the target text.
	Message string
}

// NewTextLocker creates a new text-based locker.
func NewTextLocker(cfg TextLockConfig) (*TextLocker, error) {
	if cfg.TargetText == "" {
		return nil, fmt.Errorf("target text cannot be empty")
	}
	if cfg.Message == "" {
		cfg.Message = "Type the following text to unlock:"
	}

	return &TextLocker{
		targetText: cfg.TargetText,
		message:    cfg.Message,
		stopChan:   make(chan struct{}),
	}, nil
}

// NewTextLockerFromFile creates a text locker with target text loaded from a file.
func NewTextLockerFromFile(filePath string) (*TextLocker, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read text file: %w", err)
	}

	text := strings.TrimSpace(string(content))
	if text == "" {
		return nil, fmt.Errorf("text file is empty")
	}

	return NewTextLocker(TextLockConfig{
		TargetText: text,
	})
}

// Lock activates the screen lock.
// It blocks until the user types the correct text and presses Enter.
func (tl *TextLocker) Lock() error {
	tl.mu.Lock()
	if tl.running {
		tl.mu.Unlock()
		return fmt.Errorf("locker is already running")
	}
	tl.running = true
	tl.stopChan = make(chan struct{})
	tl.typedText = ""
	tl.mu.Unlock()

	defer func() {
		tl.mu.Lock()
		tl.running = false
		tl.mu.Unlock()
	}()

	// Connect to X server
	conn, err := xgb.NewConn()
	if err != nil {
		return fmt.Errorf("failed to connect to X server: %w", err)
	}
	defer conn.Close()
	tl.conn = conn

	// Get the setup info and default screen
	setup := xproto.Setup(conn)
	if len(setup.Roots) == 0 {
		return fmt.Errorf("no screens found")
	}
	tl.screen = &setup.Roots[0]

	// Get keyboard mapping
	tl.minKeycode = setup.MinKeycode
	tl.maxKeycode = setup.MaxKeycode
	if err := tl.loadKeyboardMapping(); err != nil {
		return fmt.Errorf("failed to load keyboard mapping: %w", err)
	}

	// Load a large font
	font, err := loadLargeFont(conn)
	if err != nil {
		return fmt.Errorf("failed to load font: %w", err)
	}
	tl.font = font
	defer closeFont(conn, tl.font)

	// Create the lock window
	if err := tl.createWindow(); err != nil {
		return fmt.Errorf("failed to create window: %w", err)
	}

	// Grab keyboard and pointer
	if err := tl.grabInputs(); err != nil {
		tl.destroyWindow()
		return fmt.Errorf("failed to grab inputs: %w", err)
	}

	// Run the lock loop
	return tl.runLoop()
}

// Stop terminates the lock screen.
func (tl *TextLocker) Stop() {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	if tl.running {
		close(tl.stopChan)
	}
}

// IsRunning returns whether the locker is currently active.
func (tl *TextLocker) IsRunning() bool {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	return tl.running
}

// LockWithText is a convenience function that locks until the given text is typed.
func LockWithText(text string) error {
	locker, err := NewTextLocker(TextLockConfig{
		TargetText: text,
	})
	if err != nil {
		return err
	}
	return locker.Lock()
}

// LockWithTextFile is a convenience function that locks until text from a file is typed.
func LockWithTextFile(filePath string) error {
	locker, err := NewTextLockerFromFile(filePath)
	if err != nil {
		return err
	}
	return locker.Lock()
}

func (tl *TextLocker) loadKeyboardMapping() error {
	count := byte(tl.maxKeycode - tl.minKeycode + 1)
	reply, err := xproto.GetKeyboardMapping(tl.conn, tl.minKeycode, count).Reply()
	if err != nil {
		return err
	}
	tl.keysyms = reply.Keysyms
	return nil
}

func (tl *TextLocker) createWindow() error {
	wid, err := xproto.NewWindowId(tl.conn)
	if err != nil {
		return err
	}
	tl.window = wid

	mask := uint32(xproto.CwBackPixel | xproto.CwOverrideRedirect | xproto.CwEventMask)
	values := []uint32{
		0x1a1a2e, // Background color (dark blue)
		1,        // Override redirect
		xproto.EventMaskExposure | xproto.EventMaskKeyPress | xproto.EventMaskStructureNotify,
	}

	err = xproto.CreateWindowChecked(
		tl.conn,
		tl.screen.RootDepth,
		tl.window,
		tl.screen.Root,
		0, 0,
		tl.screen.WidthInPixels, tl.screen.HeightInPixels,
		0,
		xproto.WindowClassInputOutput,
		tl.screen.RootVisual,
		mask,
		values,
	).Check()
	if err != nil {
		return err
	}

	if err := xproto.MapWindowChecked(tl.conn, tl.window).Check(); err != nil {
		return err
	}

	if err := xproto.ConfigureWindowChecked(
		tl.conn,
		tl.window,
		xproto.ConfigWindowStackMode,
		[]uint32{xproto.StackModeAbove},
	).Check(); err != nil {
		return err
	}

	return nil
}

func (tl *TextLocker) destroyWindow() {
	if tl.window != 0 {
		xproto.DestroyWindow(tl.conn, tl.window)
		tl.window = 0
	}
}

func (tl *TextLocker) grabInputs() error {
	kbGrab, err := xproto.GrabKeyboard(
		tl.conn,
		true,
		tl.screen.Root,
		xproto.TimeCurrentTime,
		xproto.GrabModeAsync,
		xproto.GrabModeAsync,
	).Reply()
	if err != nil {
		return fmt.Errorf("keyboard grab failed: %w", err)
	}
	if kbGrab.Status != xproto.GrabStatusSuccess {
		return fmt.Errorf("keyboard grab unsuccessful: status %d", kbGrab.Status)
	}

	ptrGrab, err := xproto.GrabPointer(
		tl.conn,
		true,
		tl.screen.Root,
		0,
		xproto.GrabModeAsync,
		xproto.GrabModeAsync,
		tl.window,
		xproto.CursorNone,
		xproto.TimeCurrentTime,
	).Reply()
	if err != nil {
		xproto.UngrabKeyboard(tl.conn, xproto.TimeCurrentTime)
		return fmt.Errorf("pointer grab failed: %w", err)
	}
	if ptrGrab.Status != xproto.GrabStatusSuccess {
		xproto.UngrabKeyboard(tl.conn, xproto.TimeCurrentTime)
		return fmt.Errorf("pointer grab unsuccessful: status %d", ptrGrab.Status)
	}

	return nil
}

func (tl *TextLocker) ungrabInputs() {
	xproto.UngrabKeyboard(tl.conn, xproto.TimeCurrentTime)
	xproto.UngrabPointer(tl.conn, xproto.TimeCurrentTime)
}

func (tl *TextLocker) runLoop() error {
	defer tl.ungrabInputs()
	defer tl.destroyWindow()

	// Initial draw
	tl.drawScreen()

	for {
		select {
		case <-tl.stopChan:
			return nil
		default:
			ev, err := tl.conn.WaitForEvent()
			if err != nil {
				continue
			}
			if ev == nil {
				continue
			}

			switch e := ev.(type) {
			case xproto.KeyPressEvent:
				if tl.handleKeyPress(e) {
					return nil // Successfully unlocked
				}
				tl.drawScreen()
			case xproto.ExposeEvent:
				tl.drawScreen()
			}
		}
	}
}

func (tl *TextLocker) handleKeyPress(e xproto.KeyPressEvent) bool {
	keysym := tl.keycodeToKeysym(e.Detail, e.State)

	// Check for special keys
	switch keysym {
	case 0xff0d, 0xff8d: // Return, KP_Enter
		// Check if typed text matches target
		return tl.typedText == tl.targetText
	case 0xff08, 0xffff: // BackSpace, Delete
		if len(tl.typedText) > 0 {
			// Remove last character (handle UTF-8)
			runes := []rune(tl.typedText)
			tl.typedText = string(runes[:len(runes)-1])
		}
		return false
	case 0xff1b: // Escape - clear all typed text
		tl.typedText = ""
		return false
	}

	// Convert keysym to character
	if char := tl.keysymToChar(keysym); char != 0 {
		tl.typedText += string(char)
	}

	return false
}

func (tl *TextLocker) keycodeToKeysym(keycode xproto.Keycode, state uint16) xproto.Keysym {
	if keycode < tl.minKeycode || keycode > tl.maxKeycode {
		return 0
	}

	// Each keycode has multiple keysyms (normal, shift, etc.)
	// Keysyms per keycode is typically 4 or more
	keysymsPerKeycode := len(tl.keysyms) / int(tl.maxKeycode-tl.minKeycode+1)
	if keysymsPerKeycode == 0 {
		return 0
	}

	idx := int(keycode-tl.minKeycode) * keysymsPerKeycode

	// Check shift state (bit 0 of state)
	shiftPressed := (state & xproto.ModMaskShift) != 0
	// Check caps lock (bit 1 of state)
	capsLock := (state & xproto.ModMaskLock) != 0

	// Get base keysym
	keysym := tl.keysyms[idx]
	if keysymsPerKeycode > 1 {
		shiftedKeysym := tl.keysyms[idx+1]

		// For letters, caps lock inverts the shift behavior
		isLetter := (keysym >= 'a' && keysym <= 'z') || (keysym >= 'A' && keysym <= 'Z')
		if isLetter {
			if shiftPressed != capsLock {
				if shiftedKeysym != 0 {
					keysym = shiftedKeysym
				} else if keysym >= 'a' && keysym <= 'z' {
					keysym = keysym - 'a' + 'A'
				}
			}
		} else if shiftPressed && shiftedKeysym != 0 {
			keysym = shiftedKeysym
		}
	}

	return keysym
}

func (tl *TextLocker) keysymToChar(keysym xproto.Keysym) rune {
	// ASCII printable characters
	if keysym >= 0x20 && keysym <= 0x7e {
		return rune(keysym)
	}

	// Latin-1 supplement
	if keysym >= 0xa0 && keysym <= 0xff {
		return rune(keysym)
	}

	// Unicode keysyms (0x01000000 + unicode codepoint)
	if keysym >= 0x01000000 {
		return rune(keysym - 0x01000000)
	}

	return 0
}

func (tl *TextLocker) drawScreen() {
	gc, err := xproto.NewGcontextId(tl.conn)
	if err != nil {
		return
	}
	defer xproto.FreeGC(tl.conn, gc)

	// Clear screen
	xproto.ClearArea(tl.conn, false, tl.window, 0, 0, 0, 0)

	// White text with large font
	err = xproto.CreateGCChecked(
		tl.conn,
		gc,
		xproto.Drawable(tl.window),
		xproto.GcForeground|xproto.GcFont,
		[]uint32{0xffffff, uint32(tl.font)},
	).Check()
	if err != nil {
		return
	}

	screenWidth := int(tl.screen.WidthInPixels)
	screenHeight := int(tl.screen.HeightInPixels)
	charWidth := CharWidth
	lineHeight := LineHeight

	// Calculate text wrapping width (80% of screen)
	maxLineWidth := int(float64(screenWidth) * 0.8)
	maxCharsPerLine := maxLineWidth / charWidth
	if maxCharsPerLine < 20 {
		maxCharsPerLine = 20
	}

	// Wrap target text into lines
	targetLines := wrapText(tl.targetText, maxCharsPerLine)
	typedLines := wrapText(tl.typedText, maxCharsPerLine)

	// Calculate starting Y position
	totalLines := 1 + len(targetLines) + 2 + len(typedLines) + 2 // message + target + gap + typed + status
	startY := (screenHeight - totalLines*lineHeight) / 2
	if startY < lineHeight {
		startY = lineHeight
	}

	y := startY

	// Draw message
	tl.drawCenteredText(gc, tl.message, y, screenWidth, charWidth)
	y += lineHeight * 2

	// Draw target text (in gray)
	xproto.ChangeGC(tl.conn, gc, xproto.GcForeground, []uint32{0x888888})
	for _, line := range targetLines {
		tl.drawCenteredText(gc, line, y, screenWidth, charWidth)
		y += lineHeight
	}
	y += lineHeight

	// Draw separator
	xproto.ChangeGC(tl.conn, gc, xproto.GcForeground, []uint32{0x444444})
	tl.drawCenteredText(gc, strings.Repeat("-", maxCharsPerLine), y, screenWidth, charWidth)
	y += lineHeight * 2

	// Draw typed text with color coding
	for lineIdx, typedLine := range typedLines {
		targetLine := ""
		if lineIdx < len(targetLines) {
			targetLine = targetLines[lineIdx]
		}
		tl.drawColoredLine(gc, typedLine, targetLine, y, screenWidth, charWidth)
		y += lineHeight
	}

	// Draw cursor on current line
	if len(typedLines) == 0 {
		xproto.ChangeGC(tl.conn, gc, xproto.GcForeground, []uint32{0x00ff00})
		tl.drawCenteredText(gc, "_", y, screenWidth, charWidth)
		y += lineHeight
	}

	y += lineHeight

	// Draw status
	xproto.ChangeGC(tl.conn, gc, xproto.GcForeground, []uint32{0x888888})
	progress := fmt.Sprintf("Progress: %d / %d characters", len(tl.typedText), len(tl.targetText))
	tl.drawCenteredText(gc, progress, y, screenWidth, charWidth)
	y += lineHeight

	if tl.typedText == tl.targetText {
		xproto.ChangeGC(tl.conn, gc, xproto.GcForeground, []uint32{0x00ff00})
		tl.drawCenteredText(gc, "Text matches! Press Enter to unlock.", y, screenWidth, charWidth)
	} else {
		xproto.ChangeGC(tl.conn, gc, xproto.GcForeground, []uint32{0x666666})
		tl.drawCenteredText(gc, "Type the text above to unlock. Press Escape to clear.", y, screenWidth, charWidth)
	}
}

func (tl *TextLocker) drawCenteredText(gc xproto.Gcontext, text string, y, screenWidth, charWidth int) {
	x := (screenWidth - len(text)*charWidth) / 2
	if x < 10 {
		x = 10
	}
	tl.drawText(gc, text, x, y)
}

func (tl *TextLocker) drawText(gc xproto.Gcontext, text string, x, y int) {
	if len(text) == 0 {
		return
	}
	// X11 ImageText8 has a limit of 255 characters
	for len(text) > 0 {
		chunk := text
		if len(chunk) > 255 {
			chunk = text[:255]
		}
		xproto.ImageText8(
			tl.conn,
			byte(len(chunk)),
			xproto.Drawable(tl.window),
			gc,
			int16(x), int16(y),
			chunk,
		)
		text = text[len(chunk):]
		x += len(chunk) * CharWidth // Advance x position
	}
}

func (tl *TextLocker) drawColoredLine(gc xproto.Gcontext, typed, target string, y, screenWidth, charWidth int) {
	x := (screenWidth - max(len(typed), len(target))*charWidth) / 2
	if x < 10 {
		x = 10
	}

	typedRunes := []rune(typed)
	targetRunes := []rune(target)

	for i, r := range typedRunes {
		var color uint32
		if i < len(targetRunes) && r == targetRunes[i] {
			color = 0x00ff00 // Green for correct
		} else {
			color = 0xff0000 // Red for incorrect
		}
		xproto.ChangeGC(tl.conn, gc, xproto.GcForeground, []uint32{color})
		xproto.ImageText8(
			tl.conn,
			1,
			xproto.Drawable(tl.window),
			gc,
			int16(x), int16(y),
			string(r),
		)
		x += charWidth
	}

	// Draw cursor at end
	xproto.ChangeGC(tl.conn, gc, xproto.GcForeground, []uint32{0x00ff00})
	xproto.ImageText8(
		tl.conn,
		1,
		xproto.Drawable(tl.window),
		gc,
		int16(x), int16(y),
		"_",
	)
}

func wrapText(text string, maxWidth int) []string {
	if len(text) == 0 {
		return nil
	}

	var lines []string
	// First split by newlines
	paragraphs := strings.Split(text, "\n")

	for _, para := range paragraphs {
		if len(para) == 0 {
			lines = append(lines, "")
			continue
		}

		// Wrap each paragraph
		for len(para) > maxWidth {
			// Try to break at a space
			breakPoint := maxWidth
			for i := maxWidth; i > maxWidth/2; i-- {
				if para[i] == ' ' {
					breakPoint = i
					break
				}
			}
			lines = append(lines, para[:breakPoint])
			para = strings.TrimLeft(para[breakPoint:], " ")
		}
		if len(para) > 0 {
			lines = append(lines, para)
		}
	}

	return lines
}
