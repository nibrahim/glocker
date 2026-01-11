package lock

import (
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// Font size constants
const (
	// CharWidth is the approximate width of a character in the large font
	CharWidth = 10
	// LineHeight is the height of a line in the large font
	LineHeight = 20
)

// Large font patterns to try, in order of preference
var largeFontPatterns = []string{
	"-misc-fixed-medium-r-normal--20-200-75-75-c-100-iso8859-1",
	"10x20",
	"-*-fixed-medium-r-*-*-20-*-*-*-*-*-*-*",
	"-*-fixed-*-*-*-*-18-*-*-*-*-*-*-*",
	"9x15bold",
	"9x15",
	"fixed",
}

// loadLargeFont attempts to load a large font, trying multiple patterns.
// Returns the font ID and nil error on success, or 0 and an error if no font could be loaded.
func loadLargeFont(conn *xgb.Conn) (xproto.Font, error) {
	fid, err := xproto.NewFontId(conn)
	if err != nil {
		return 0, err
	}

	for _, pattern := range largeFontPatterns {
		err = xproto.OpenFontChecked(conn, fid, uint16(len(pattern)), pattern).Check()
		if err == nil {
			return fid, nil
		}
	}

	// If all patterns fail, return the last error
	return 0, err
}

// closeFont closes a font resource.
func closeFont(conn *xgb.Conn, font xproto.Font) {
	if font != 0 {
		xproto.CloseFont(conn, font)
	}
}
