package lock

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// Default background color (dark green)
const DefaultBackgroundColor = 0x1a3d2e

// loadBackgroundImage loads an image file and returns the decoded image.
func loadBackgroundImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open image: %w", err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	return img, nil
}

// createBackgroundPixmap creates an X11 pixmap from an image, scaled to fit the screen.
func createBackgroundPixmap(conn *xgb.Conn, screen *xproto.ScreenInfo, img image.Image) (xproto.Pixmap, error) {
	screenWidth := int(screen.WidthInPixels)
	screenHeight := int(screen.HeightInPixels)

	// Create pixmap
	pid, err := xproto.NewPixmapId(conn)
	if err != nil {
		return 0, err
	}

	err = xproto.CreatePixmapChecked(
		conn,
		screen.RootDepth,
		pid,
		xproto.Drawable(screen.Root),
		uint16(screenWidth),
		uint16(screenHeight),
	).Check()
	if err != nil {
		return 0, err
	}

	// Create GC for drawing
	gc, err := xproto.NewGcontextId(conn)
	if err != nil {
		xproto.FreePixmap(conn, pid)
		return 0, err
	}
	defer xproto.FreeGC(conn, gc)

	err = xproto.CreateGCChecked(conn, gc, xproto.Drawable(pid), 0, nil).Check()
	if err != nil {
		xproto.FreePixmap(conn, pid)
		return 0, err
	}

	// Get image bounds
	bounds := img.Bounds()
	imgWidth := bounds.Dx()
	imgHeight := bounds.Dy()

	// Calculate scaling to cover the screen (maintaining aspect ratio)
	scaleX := float64(screenWidth) / float64(imgWidth)
	scaleY := float64(screenHeight) / float64(imgHeight)
	scale := scaleX
	if scaleY > scaleX {
		scale = scaleY
	}

	scaledWidth := int(float64(imgWidth) * scale)
	scaledHeight := int(float64(imgHeight) * scale)

	// Calculate offset to center the image
	offsetX := (scaledWidth - screenWidth) / 2
	offsetY := (scaledHeight - screenHeight) / 2

	// Convert image to X11 format and draw in chunks
	// X11 PutImage has size limits, so we draw in rows
	rowHeight := 64 // Process 64 rows at a time
	for startY := 0; startY < screenHeight; startY += rowHeight {
		endY := startY + rowHeight
		if endY > screenHeight {
			endY = screenHeight
		}
		chunkHeight := endY - startY

		// Create pixel data for this chunk
		data := make([]byte, screenWidth*chunkHeight*4)

		for y := startY; y < endY; y++ {
			for x := 0; x < screenWidth; x++ {
				// Map screen coordinates back to image coordinates
				imgX := int(float64(x+offsetX) / scale)
				imgY := int(float64(y+offsetY) / scale)

				var r, g, b uint8
				if imgX >= 0 && imgX < imgWidth && imgY >= 0 && imgY < imgHeight {
					c := img.At(bounds.Min.X+imgX, bounds.Min.Y+imgY)
					r32, g32, b32, _ := c.RGBA()
					r = uint8(r32 >> 8)
					g = uint8(g32 >> 8)
					b = uint8(b32 >> 8)
				}

				// X11 uses BGRX format (blue, green, red, padding)
				idx := ((y - startY) * screenWidth + x) * 4
				data[idx] = b
				data[idx+1] = g
				data[idx+2] = r
				data[idx+3] = 0
			}
		}

		// Put the image chunk
		xproto.PutImage(
			conn,
			xproto.ImageFormatZPixmap,
			xproto.Drawable(pid),
			gc,
			uint16(screenWidth),
			uint16(chunkHeight),
			0,
			int16(startY),
			0,
			screen.RootDepth,
			data,
		)
	}

	return pid, nil
}
