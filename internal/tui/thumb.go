package tui

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/iutui/internal/data"
)

var (
	thumbCache    = make(map[string]string)
	thumbCacheMu  sync.RWMutex
	fetchingMap   = make(map[string]bool)
	fetchingMu    sync.Mutex
	preparingMap  = make(map[string]bool)
	prepareMu     sync.Mutex
	prepareSem    = make(chan struct{}, 4)
	kittyUploaded = make(map[uint32]bool)
	kittyUploadMu sync.Mutex
	ttyWriteMu    sync.Mutex
)

// kittyDiacritics maps indices 0..63 to Kitty's official row/column combining diacritics for U+10EEEE placeholders.
var kittyDiacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F,
	0x0346, 0x034A, 0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357,
	0x035B, 0x0363, 0x0364, 0x0365, 0x0366, 0x0367, 0x0368, 0x0369,
	0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F, 0x0483, 0x0484,
	0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
	0x0598, 0x0599, 0x059A, 0x059B, 0x059C, 0x059D, 0x059E, 0x059F,
	0x05A0, 0x05A1, 0x05A3, 0x05A4, 0x05A5, 0x05A6, 0x05A7, 0x05A8,
	0x05A9, 0x05AA, 0x05AB, 0x05AC, 0x05AD, 0x05AE, 0x05AF, 0x05C4,
}

// isKittyGraphicsSupported returns true when running inside Kitty or Ghostty terminal.
func isKittyGraphicsSupported() bool {
	if os.Getenv("YT_PREVIEW") == "chafa" || os.Getenv("YT_PREVIEW") == "none" {
		return false
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("TERM") == "xterm-kitty" || os.Getenv("TERM_PROGRAM") == "ghostty" {
		return true
	}
	return false
}

// cleanThumbID strips playlist/mix prefixes like "RD" so YouTube's i.ytimg.com returns a valid thumbnail.
func cleanThumbID(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "RD") && len(id) > 7 {
		return strings.TrimPrefix(id, "RD")
	}
	return id
}

// GetThumbnailFile returns the path to a cached thumbnail image (preferring 16:9 HD).
func GetThumbnailFile(id string) string {
	cleanID := cleanThumbID(id)
	if cleanID == "" {
		return ""
	}
	cacheDir, _ := data.DefaultPaths()
	thumbsDir := filepath.Join(cacheDir, "thumbs")
	for _, q := range []string{"maxresdefault", "mqdefault", "hqdefault"} {
		f := filepath.Join(thumbsDir, fmt.Sprintf("%s_%s.jpg", cleanID, q))
		if info, err := os.Stat(f); err == nil && info.Size() > 1000 {
			return f
		}
	}
	return ""
}

// FetchThumbnailAsync triggers a background download for missing thumbnails using maxresdefault (1280x720)
// and falls back to mqdefault (320x180). Both are true 16:9 and lack the 45px black letterbox bars of hqdefault.
func FetchThumbnailAsync(id string) {
	cleanID := cleanThumbID(id)
	if len(cleanID) < 5 {
		return
	}
	fetchingMu.Lock()
	if fetchingMap[cleanID] {
		fetchingMu.Unlock()
		return
	}
	fetchingMap[cleanID] = true
	fetchingMu.Unlock()

	go func() {
		cacheDir, _ := data.DefaultPaths()
		thumbsDir := filepath.Join(cacheDir, "thumbs")
		_ = os.MkdirAll(thumbsDir, 0755)

		// 1. Try maxresdefault (1280x720 16:9, crystal clear HD)
		targetMax := filepath.Join(thumbsDir, fmt.Sprintf("%s_maxresdefault.jpg", cleanID))
		urlMax := fmt.Sprintf("https://i.ytimg.com/vi/%s/maxresdefault.jpg", cleanID)
		cmdMax := exec.Command("curl", "-sfL", "--max-time", "4", "-o", targetMax, urlMax)
		if err := cmdMax.Run(); err != nil || isFileTooSmall(targetMax, 3000) {
			_ = os.Remove(targetMax)
			// 2. Fallback to mqdefault (320x180 16:9, clean without black bars)
			targetMq := filepath.Join(thumbsDir, fmt.Sprintf("%s_mqdefault.jpg", cleanID))
			urlMq := fmt.Sprintf("https://i.ytimg.com/vi/%s/mqdefault.jpg", cleanID)
			cmdMq := exec.Command("curl", "-sfL", "--max-time", "4", "-o", targetMq, urlMq)
			_ = cmdMq.Run()
		}

		fetchingMu.Lock()
		delete(fetchingMap, cleanID)
		fetchingMu.Unlock()
	}()
}

func isFileTooSmall(path string, minBytes int64) bool {
	info, err := os.Stat(path)
	return err != nil || info.Size() < minBytes
}

// RenderThumbnailANSI renders real pixel HD images inside cards via Kitty Unicode Placeholders (when running
// in Kitty/Ghostty), or falls back to truecolor subpixel quadrant blocks for other terminals.
func RenderThumbnailANSI(id string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	useKitty := isKittyGraphicsSupported()
	cacheKey := fmt.Sprintf("%s_%dx%d_k=%v", id, width, height, useKitty)

	thumbCacheMu.RLock()
	cached, ok := thumbCache[cacheKey]
	thumbCacheMu.RUnlock()
	if ok {
		return cached
	}

	file := GetThumbnailFile(id)
	if file == "" {
		FetchThumbnailAsync(id)
		return renderEmptyThumb(width, height)
	}

	if useKitty && width <= len(kittyDiacritics) && height <= len(kittyDiacritics) {
		prepareKittyAsync(cacheKey, id, file, width, height)
		return renderEmptyThumb(width, height)
	}

	ansiStr, err := renderHighDefBlocks(file, width, height)
	if err != nil || ansiStr == "" {
		return renderEmptyThumb(width, height)
	}

	thumbCacheMu.Lock()
	thumbCache[cacheKey] = ansiStr
	thumbCacheMu.Unlock()

	return ansiStr
}

func prepareKittyAsync(cacheKey, id, file string, width, height int) {
	prepareMu.Lock()
	if preparingMap[cacheKey] {
		prepareMu.Unlock()
		return
	}
	preparingMap[cacheKey] = true
	prepareMu.Unlock()

	go func() {
		prepareSem <- struct{}{}
		defer func() {
			<-prepareSem
			prepareMu.Lock()
			delete(preparingMap, cacheKey)
			prepareMu.Unlock()
		}()

		if kittyStr, err := renderKittyPlaceholder(id, file, width, height); err == nil && kittyStr != "" {
			thumbCacheMu.Lock()
			thumbCache[cacheKey] = kittyStr
			thumbCacheMu.Unlock()
		}
	}()
}

func videoImageID(id string, cols, rows int) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(fmt.Sprintf("%s_%dx%d", cleanThumbID(id), cols, rows)))
	// 24-bit non-zero ID (1 .. 16,777,215) so it fits in 24-bit RGB foreground color
	val := h.Sum32() & 0xFFFFFF
	if val == 0 {
		val = 1
	}
	return val
}

// renderKittyPlaceholder uploads the downscaled PNG (320x180) once to /dev/tty using Kitty Graphics Protocol
// and returns a grid of U+10EEEE Unicode placeholder cells that Lipgloss can safely wrap with borders.
func renderKittyPlaceholder(id, path string, cols, rows int) (string, error) {
	if cols <= 0 || rows <= 0 || cols > len(kittyDiacritics) || rows > len(kittyDiacritics) {
		return "", fmt.Errorf("invalid kitty dimensions %dx%d", cols, rows)
	}
	imgID := videoImageID(id, cols, rows)

	kittyUploadMu.Lock()
	uploaded := kittyUploaded[imgID]
	kittyUploadMu.Unlock()

	if !uploaded {
		if err := uploadKittyImage(imgID, path, cols, rows); err != nil {
			return "", err
		}
		kittyUploadMu.Lock()
		kittyUploaded[imgID] = true
		kittyUploadMu.Unlock()
	}

	return formatKittyPlaceholders(imgID, cols, rows), nil
}

func formatKittyPlaceholders(imgID uint32, cols, rows int) string {
	rColor := (imgID >> 16) & 0xFF
	gColor := (imgID >> 8) & 0xFF
	bColor := imgID & 0xFF

	var lines []string
	for r := 0; r < rows; r++ {
		var sb strings.Builder
		fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm", rColor, gColor, bColor)
		rowDia := kittyDiacritics[r]
		for c := 0; c < cols; c++ {
			colDia := kittyDiacritics[c]
			sb.WriteRune(0x10EEEE)
			sb.WriteRune(rowDia)
			sb.WriteRune(colDia)
			sb.WriteRune(kittyDiacritics[0]) // MSB of 24-bit image ID = 0
		}
		sb.WriteString("\x1b[39m")
		lines = append(lines, sb.String())
	}

	return strings.Join(lines, "\n")
}

// RenderBrandLogo renders a crisp antialiased YouTube play-badge + wordmark PNG
// via Kitty Unicode placeholders for Ghostty/Kitty, or returns "" when unsupported.
func RenderBrandLogo(cols, rows int) string {
	if !isKittyGraphicsSupported() || cols <= 0 || rows <= 0 || cols > len(kittyDiacritics) || rows > len(kittyDiacritics) {
		return ""
	}
	cacheKey := fmt.Sprintf("__brand_logo_%dx%d", cols, rows)
	thumbCacheMu.RLock()
	cached, ok := thumbCache[cacheKey]
	thumbCacheMu.RUnlock()
	if ok {
		return cached
	}

	const brandImgID uint32 = 0x495554 // "IUT"
	kittyUploadMu.Lock()
	uploaded := kittyUploaded[brandImgID]
	kittyUploadMu.Unlock()

	if !uploaded {
		pngBytes, err := renderYouTubeBrandPNG()
		if err != nil {
			return ""
		}
		if err := transmitKittyPNG(brandImgID, pngBytes, cols, rows); err != nil {
			return ""
		}
		kittyUploadMu.Lock()
		kittyUploaded[brandImgID] = true
		kittyUploadMu.Unlock()
	}

	out := formatKittyPlaceholders(brandImgID, cols, rows)
	thumbCacheMu.Lock()
	thumbCache[cacheKey] = out
	thumbCacheMu.Unlock()
	return out
}

//go:embed logo.png
var embeddedBrandLogoPNG []byte

func renderYouTubeBrandPNG() ([]byte, error) {
	if len(embeddedBrandLogoPNG) == 0 {
		return nil, fmt.Errorf("empty embedded brand logo")
	}
	return embeddedBrandLogoPNG, nil
}

// uploadKittyImage decodes the JPEG, scales it cleanly to 320x180 PNG in memory, and sends it to /dev/tty
// using Kitty's APC graphics sequence with U=1 (virtual placement for Unicode placeholders).
func uploadKittyImage(imgID uint32, path string, cols, rows int) error {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_ = tty.Close()

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	srcImg, _, err := image.Decode(f)
	if err != nil {
		return err
	}

	// Scale to 480x270 (16:9 HD thumbnail) with bilinear filtering so cards look crisp in Ghostty/Kitty.
	const dstW, dstH = 480, 270
	bounds := srcImg.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return fmt.Errorf("empty image")
	}

	cropY0 := bounds.Min.Y
	cropH := srcH
	if float64(srcW)/float64(srcH) < 1.45 {
		targetH := int(float64(srcW) * 9.0 / 16.0)
		if targetH > 0 && targetH < srcH {
			cropY0 += (srcH - targetH) / 2
			cropH = targetH
		}
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < dstH; y++ {
		fy := float64(y) * float64(cropH-1) / float64(dstH-1)
		y0 := cropY0 + int(fy)
		y1 := y0 + 1
		if y1 >= cropY0+cropH {
			y1 = y0
		}
		wy := fy - float64(int(fy))
		for x := 0; x < dstW; x++ {
			fx := float64(x) * float64(srcW-1) / float64(dstW-1)
			x0 := bounds.Min.X + int(fx)
			x1 := x0 + 1
			if x1 >= bounds.Max.X {
				x1 = x0
			}
			wx := fx - float64(int(fx))

			r00, g00, b00, a00 := srcImg.At(x0, y0).RGBA()
			r10, g10, b10, a10 := srcImg.At(x1, y0).RGBA()
			r01, g01, b01, a01 := srcImg.At(x0, y1).RGBA()
			r11, g11, b11, a11 := srcImg.At(x1, y1).RGBA()

			bilerp := func(c00, c10, c01, c11 uint32) uint8 {
				top := float64(c00)*(1-wx) + float64(c10)*wx
				bot := float64(c01)*(1-wx) + float64(c11)*wx
				return uint8((top*(1-wy) + bot*wy) / 257.0)
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: bilerp(r00, r10, r01, r11),
				G: bilerp(g00, g10, g01, g11),
				B: bilerp(b00, b10, b01, b11),
				A: bilerp(a00, a10, a01, a11),
			})
		}
	}

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, dst); err != nil {
		return err
	}
	return transmitKittyPNG(imgID, pngBuf.Bytes(), cols, rows)
}

func transmitKittyPNG(imgID uint32, pngBytes []byte, cols, rows int) error {
	b64 := base64.StdEncoding.EncodeToString(pngBytes)

	var out bytes.Buffer
	const chunkSize = 4096
	for i := 0; i < len(b64); i += chunkSize {
		end := i + chunkSize
		more := 1
		if end >= len(b64) {
			end = len(b64)
			more = 0
		}
		chunk := b64[i:end]
		if i == 0 {
			// a=T (transmit & display), U=1 (unicode virtual placement), f=100 (PNG), q=2 (quiet), C=1 (no cursor move)
			fmt.Fprintf(&out, "\x1b_Ga=T,U=1,q=2,f=100,i=%d,c=%d,r=%d,C=1,m=%d;%s\x1b\\",
				imgID, cols, rows, more, chunk)
		} else {
			fmt.Fprintf(&out, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
	}

	ttyWriteMu.Lock()
	defer ttyWriteMu.Unlock()

	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer tty.Close()

	_, err = tty.Write(out.Bytes())
	return err
}

// renderHighDefBlocks decodes the JPEG in pure Go, performs area-averaged subpixel downsampling
// with contrast/unsharp boost, and renders crisp 24-bit truecolor half-block / quadrant cells.
func renderHighDefBlocks(path string, cols, rows int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return "", err
	}

	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return "", fmt.Errorf("invalid image dimensions")
	}

	// Automatically crop 4:3 letterbox bars if image is 4:3 (e.g. 480x360 hqdefault)
	cropY0 := bounds.Min.Y
	cropH := srcH
	aspect := float64(srcW) / float64(srcH)
	if aspect < 1.45 {
		targetH := int(float64(srcW) * 9.0 / 16.0)
		if targetH > 0 && targetH < srcH {
			cropY0 += (srcH - targetH) / 2
			cropH = targetH
		}
	}

	subW := cols * 2
	subH := rows * 2
	grid := make([][3]float64, subW*subH)

	for sy := 0; sy < subH; sy++ {
		y0 := cropY0 + (sy*cropH)/subH
		y1 := cropY0 + ((sy+1)*cropH)/subH
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for sx := 0; sx < subW; sx++ {
			x0 := bounds.Min.X + (sx*srcW)/subW
			x1 := bounds.Min.X + ((sx+1)*srcW)/subW
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var rSum, gSum, bSum, count float64
			stepY := 1
			if y1-y0 > 4 {
				stepY = (y1 - y0) / 3
			}
			stepX := 1
			if x1-x0 > 4 {
				stepX = (x1 - x0) / 3
			}
			for y := y0; y < y1; y += stepY {
				for x := x0; x < x1; x += stepX {
					r, g, b, _ := img.At(x, y).RGBA()
					rSum += float64(r >> 8)
					gSum += float64(g >> 8)
					bSum += float64(b >> 8)
					count++
				}
			}
			if count > 0 {
				grid[sy*subW+sx] = [3]float64{rSum / count, gSum / count, bSum / count}
			}
		}
	}

	sharpened := make([][3]uint8, subW*subH)
	const amount = 0.38
	for sy := 0; sy < subH; sy++ {
		for sx := 0; sx < subW; sx++ {
			center := grid[sy*subW+sx]
			var nSum [3]float64
			var nCount float64
			if sy > 0 {
				p := grid[(sy-1)*subW+sx]
				nSum[0] += p[0]
				nSum[1] += p[1]
				nSum[2] += p[2]
				nCount++
			}
			if sy < subH-1 {
				p := grid[(sy+1)*subW+sx]
				nSum[0] += p[0]
				nSum[1] += p[1]
				nSum[2] += p[2]
				nCount++
			}
			if sx > 0 {
				p := grid[sy*subW+(sx-1)]
				nSum[0] += p[0]
				nSum[1] += p[1]
				nSum[2] += p[2]
				nCount++
			}
			if sx < subW-1 {
				p := grid[sy*subW+(sx+1)]
				nSum[0] += p[0]
				nSum[1] += p[1]
				nSum[2] += p[2]
				nCount++
			}
			var out [3]uint8
			for c := 0; c < 3; c++ {
				val := center[c]
				if nCount > 0 {
					blur := nSum[c] / nCount
					val = center[c] + amount*(center[c]-blur)
				}
				val = (val-128.0)*1.08 + 128.0
				out[c] = clampByte(val)
			}
			sharpened[sy*subW+sx] = out
		}
	}

	var lines []string
	for row := 0; row < rows; row++ {
		var sb strings.Builder
		for col := 0; col < cols; col++ {
			tl := sharpened[(row*2)*subW+(col*2)]
			tr := sharpened[(row*2)*subW+(col*2+1)]
			bl := sharpened[(row*2+1)*subW+(col*2)]
			br := sharpened[(row*2+1)*subW+(col*2+1)]

			glyph, fg, bg := bestQuadrant(tl, tr, bl, br)
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm%s",
				fg.R, fg.G, fg.B, bg.R, bg.G, bg.B, glyph)
		}
		sb.WriteString("\x1b[0m")
		lines = append(lines, sb.String())
	}

	return strings.Join(lines, "\n"), nil
}

var quadrantGlyphs = [16]string{
	" ", "▗", "▖", "▄",
	"▝", "▐", "▞", "▟",
	"▘", "▚", "▌", "▙",
	"▀", "▜", "▛", "█",
}

func bestQuadrant(tl, tr, bl, br [3]uint8) (string, color.RGBA, color.RGBA) {
	pixels := [4][3]float64{
		{float64(tl[0]), float64(tl[1]), float64(tl[2])},
		{float64(tr[0]), float64(tr[1]), float64(tr[2])},
		{float64(bl[0]), float64(bl[1]), float64(bl[2])},
		{float64(br[0]), float64(br[1]), float64(br[2])},
	}
	bits := [4]int{8, 4, 2, 1}

	bestErr := 1e18
	bestMask := 12
	var bestFG, bestBG [3]float64

	for mask := 1; mask <= 7; mask++ {
		var fgSum, bgSum [3]float64
		var fgCnt, bgCnt float64
		for i := 0; i < 4; i++ {
			if (mask & bits[i]) != 0 {
				fgSum[0] += pixels[i][0]
				fgSum[1] += pixels[i][1]
				fgSum[2] += pixels[i][2]
				fgCnt++
			} else {
				bgSum[0] += pixels[i][0]
				bgSum[1] += pixels[i][1]
				bgSum[2] += pixels[i][2]
				bgCnt++
			}
		}
		fgAvg := [3]float64{fgSum[0] / fgCnt, fgSum[1] / fgCnt, fgSum[2] / fgCnt}
		bgAvg := [3]float64{bgSum[0] / bgCnt, bgSum[1] / bgCnt, bgSum[2] / bgCnt}

		var errSum float64
		for i := 0; i < 4; i++ {
			target := bgAvg
			if (mask & bits[i]) != 0 {
				target = fgAvg
			}
			dr := pixels[i][0] - target[0]
			dg := pixels[i][1] - target[1]
			db := pixels[i][2] - target[2]
			errSum += 2.0*dr*dr + 4.0*dg*dg + 1.0*db*db
		}

		if mask == 3 {
			errSum *= 0.92
		}

		if errSum < bestErr {
			bestErr = errSum
			bestMask = mask
			bestFG = fgAvg
			bestBG = bgAvg
		}
	}

	if bestMask == 3 {
		bestMask = 12
		bestFG, bestBG = bestBG, bestFG
	}

	fg := color.RGBA{R: clampByte(bestFG[0]), G: clampByte(bestFG[1]), B: clampByte(bestFG[2]), A: 255}
	bg := color.RGBA{R: clampByte(bestBG[0]), G: clampByte(bestBG[1]), B: clampByte(bestBG[2]), A: 255}
	return quadrantGlyphs[bestMask], fg, bg
}

func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func renderEmptyThumb(width, height int) string {
	var lines []string
	bgStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Background(lipgloss.Color("#161925"))
	accentStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#BD93F9")).
		Background(lipgloss.Color("#161925")).
		Bold(true)

	for i := 0; i < height; i++ {
		if i == height/2 {
			icon := "▶ "
			text := "YouTube 16:9"
			totalLen := 2 + len(text)
			leftPad := (width - totalLen) / 2
			if leftPad < 0 {
				leftPad = 0
			}
			rightPad := width - leftPad - totalLen
			if rightPad < 0 {
				rightPad = 0
			}
			line := bgStyle.Render(strings.Repeat(" ", leftPad)) +
				accentStyle.Render(icon) +
				bgStyle.Render(text+strings.Repeat(" ", rightPad))
			lines = append(lines, line)
		} else {
			lines = append(lines, bgStyle.Render(strings.Repeat(" ", width)))
		}
	}
	return strings.Join(lines, "\n")
}
