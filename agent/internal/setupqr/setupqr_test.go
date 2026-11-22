package setupqr

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSetupURLPutsTokenInFragment(t *testing.T) {
	const token = "abcDEF-123_xyz789"
	got, err := SetupURL("192.168.1.50", token)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://192.168.1.50/ui/setup#token=" + token
	if got != want {
		t.Fatalf("setup URL = %q, want %q", got, want)
	}
	if strings.Contains(got, "?token=") || strings.Contains(got, "&token=") {
		t.Fatal("the token is in the query string, not the fragment")
	}
	page, err := PageURL("192.168.1.50")
	if err != nil {
		t.Fatal(err)
	}
	if page != "https://192.168.1.50/ui/setup" {
		t.Fatalf("page URL = %q", page)
	}
	if _, err := SetupURL("", token); err == nil {
		t.Fatal("an empty lan address produced a setup URL")
	}
}

func TestGroupedTokenRoundTrips(t *testing.T) {
	const token = "abcDEF-123_xyz789QRS-TUV"
	grouped := GroupToken(token)
	if grouped == token {
		t.Fatal("the token was not grouped")
	}
	if strings.ReplaceAll(grouped, " ", "") != token {
		t.Fatalf("grouped token = %q", grouped)
	}
	if NormalizeToken("  "+grouped+"\n") != token {
		t.Fatal("the grouped token did not normalize to the exact token")
	}
}

func TestShortCodeIsSeparateSingleFormAndNormalized(t *testing.T) {
	reader := bytes.NewReader([]byte{0, 1, 2, 3, 4})
	code, err := NewShortCode(reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 8 {
		t.Fatalf("short code length = %d, want 8", len(code))
	}
	if strings.ContainsAny(code, "ILOU") {
		t.Fatalf("short code contains an ambiguous character: %q", code)
	}
	const token = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	if strings.HasPrefix(token, code) || strings.Contains(token, code) {
		t.Fatal("the short code is a prefix or truncation of the setup token")
	}

	grouped := FormatShortCode(strings.ToLower(code))
	if !strings.Contains(grouped, "-") {
		t.Fatalf("formatted short code = %q", grouped)
	}
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	expires := now.Add(time.Minute)
	hash := HashShortCode(code)
	if !ShortCodeMatches(" "+strings.ToLower(grouped)+" ", hash, expires, now) {
		t.Fatal("the formatted short code did not match")
	}
	if ShortCodeMatches(code, hash, expires, expires) {
		t.Fatal("an expired short code matched")
	}
	if ShortCodeMatches("WRONG123", hash, expires, now) {
		t.Fatal("a different short code matched")
	}
}

func TestFormatBlockKeepsRawTokenOutOfThePrintedLines(t *testing.T) {
	const token = "abcDEF-123_xyz789"
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	block := Block{
		PageURL:   "https://192.168.1.50/ui/setup",
		Token:     token,
		ShortCode: "ABCD2345",
		Expires:   now.Add(10 * time.Minute),
		Now:       now,
	}
	output := FormatBlock(block, "<QR>\n")
	if strings.Contains(output, token) {
		t.Fatal("the raw token appears as one contiguous string")
	}
	if !strings.Contains(output, GroupToken(token)) {
		t.Fatal("the grouped token is missing")
	}
	if !strings.Contains(output, "ABCD-2345") {
		t.Fatal("the formatted short code is missing")
	}
	if strings.Contains(output, "#token=") {
		t.Fatal("the printed block repeats the fragment")
	}
}

func TestRenderQRRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("qrencode"); err != nil {
		t.Skip("qrencode is not installed")
	}
	if _, err := exec.LookPath("zbarimg"); err != nil {
		t.Skip("zbarimg is not installed (test-only dependency)")
	}
	const token = "abcDEF-123_xyz789QRS-TUV"
	payload, err := SetupURL("192.168.1.50", token)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RenderQR(&output, payload); err != nil {
		t.Fatal(err)
	}
	image, width, height := ansiQRImage(t, output.String())
	command := exec.Command("zbarimg", "-q", "--raw", "-")
	command.Stdin = bytes.NewReader(image)
	decoded, err := command.Output()
	if err != nil {
		t.Fatalf("zbarimg could not decode the rendered QR (%dx%d): %v", width, height, err)
	}
	if strings.TrimSpace(string(decoded)) != payload {
		t.Fatalf("decoded QR = %q, want %q", strings.TrimSpace(string(decoded)), payload)
	}
}

// ansiQRImage turns qrencode's ANSI terminal output into a PGM image. Each
// module is two character cells wide so it looks square in a terminal.
func ansiQRImage(t *testing.T, output string) ([]byte, int, int) {
	t.Helper()
	var rows [][]byte
	for _, line := range strings.Split(output, "\n") {
		if line == "" || !strings.Contains(line, "\x1b[") {
			continue
		}
		var pixels []byte
		for index := 0; index < len(line); {
			if line[index] == '\x1b' {
				end := strings.IndexByte(line[index:], 'm')
				if end < 0 {
					t.Fatalf("unterminated ANSI sequence in %q", line)
				}
				switch line[index+2 : index+end] {
				case "47":
					// White background is a light QR module.
				case "40":
					// Black background is a dark QR module.
				case "0":
				default:
					t.Fatalf("unexpected ANSI sequence in %q", line)
				}
				code := line[index+2 : index+end]
				index += end + 1
				for index < len(line) && line[index] == ' ' {
					if code == "40" {
						pixels = append(pixels, 0)
					} else {
						pixels = append(pixels, 255)
					}
					index++
				}
				continue
			}
			if line[index] != ' ' {
				t.Fatalf("unexpected character %q in ANSI QR output", line[index])
			}
			index++
		}
		if len(pixels) == 0 {
			continue
		}
		if len(pixels)%2 != 0 {
			t.Fatalf("ANSI QR row has an odd number of cells: %d", len(pixels))
		}
		row := make([]byte, len(pixels)/2)
		for index := range row {
			if pixels[2*index] != pixels[2*index+1] {
				t.Fatalf("ANSI QR row module is not two cells wide")
			}
			row[index] = pixels[2*index]
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		t.Fatal("ANSI QR output had no rows")
	}
	width := len(rows[0])
	for _, row := range rows {
		if len(row) != width {
			t.Fatalf("ANSI QR rows have different widths")
		}
	}
	const scale = 4
	var image bytes.Buffer
	fmt.Fprintf(&image, "P5\n%d %d\n255\n", width*scale, len(rows)*scale)
	for _, row := range rows {
		for repeat := 0; repeat < scale; repeat++ {
			for _, pixel := range row {
				for pixelRepeat := 0; pixelRepeat < scale; pixelRepeat++ {
					image.WriteByte(pixel)
				}
			}
		}
	}
	return image.Bytes(), width, len(rows)
}
