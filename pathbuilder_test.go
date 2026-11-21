package gensvg

import (
	"bytes"
	"encoding/xml"
	"math"
	"strconv"
	"strings"
	"testing"
)

// fixedPath builds the fixed mixed absolute/relative path used by several tests.
func fixedPath(pb *PathBuilder) *PathBuilder {
	return pb.MoveTo(10, 10).
		LineBy(30, 0).
		LineTo(40, 50).
		CubicTo(50, 60, 60, 60, 70, 50).
		SmoothCubicTo(80, 40, 90, 50).
		QuadBy(10, 10, 20, 0).
		SmoothQuadTo(130, 50).
		ArcTo(15, 15, 0, false, true, 160, 50).
		ArcBy(10, 10, 0, true, false, 20, 10)
}

const fixedD = "M10.00,10.00" +
	" l30.00,0.00" +
	" L40.00,50.00" +
	" C50.00,60.00 60.00,60.00 70.00,50.00" +
	" C80.00,40.00 80.00,40.00 90.00,50.00" +
	" q10.00,10.00 20.00,0.00" +
	" Q120.00,40.00 130.00,50.00" +
	" A15.00,15.00 0.00 0 1 160.00,50.00" +
	" a10.00,10.00 0.00 1 0 20.00,10.00"

// TestPathBuilderMixed checks the d attribute and tracked positions of a
// fixed path combining absolute and relative commands.
func TestPathBuilderMixed(t *testing.T) {
	pb := fixedPath(NewPathBuilder(2))
	d, err := pb.String()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != fixedD {
		t.Errorf("d mismatch:\n got %s\nwant %s", d, fixedD)
	}
	if x, y := pb.Pos(); x != 180 || y != 60 {
		t.Errorf("Pos = (%g, %g), want (180, 60)", x, y)
	}
	if x, y := pb.SubpathStart(); x != 10 || y != 10 {
		t.Errorf("SubpathStart = (%g, %g), want (10, 10)", x, y)
	}
	// Closing returns to the subpath start, and drawing may continue.
	pb.Close()
	if x, y := pb.Pos(); x != 10 || y != 10 {
		t.Errorf("Pos after Close = (%g, %g), want (10, 10)", x, y)
	}
	d, err = pb.String()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != fixedD+" Z" {
		t.Errorf("closed d mismatch:\n got %s\nwant %s", d, fixedD+" Z")
	}
}

// svgPoint is a 2D point for the reference parser.
type svgPoint struct{ x, y float64 }

// parseSVGPath is a reference interpreter for SVG path data, following
// https://www.w3.org/TR/SVG11/paths.html. It returns the final current
// point and the start of the last subpath.
func parseSVGPath(t *testing.T, d string) (cur, start svgPoint) {
	t.Helper()
	tokens := tokenizePath(t, d)
	i := 0
	next := func() float64 {
		if i >= len(tokens) {
			t.Fatalf("truncated path data %q", d)
		}
		tk := tokens[i]
		i++
		n, err := strconv.ParseFloat(tk, 64)
		if err != nil {
			t.Fatalf("bad number %q in path data: %v", tk, err)
		}
		return n
	}
	var cmd byte
	for i < len(tokens) {
		tk := tokens[i]
		if len(tk) == 1 && (tk[0] >= 'A' && tk[0] <= 'Z' || tk[0] >= 'a' && tk[0] <= 'z') {
			cmd = tk[0]
			i++
		}
		rel := cmd >= 'a' && cmd <= 'z'
		abs := func(x, y float64) (float64, float64) {
			if rel {
				return cur.x + x, cur.y + y
			}
			return x, y
		}
		switch cmd {
		case 'M', 'm':
			x, y := abs(next(), next())
			cur, start = svgPoint{x, y}, svgPoint{x, y}
			// subsequent coordinate pairs are implicit linetos
			if cmd == 'M' {
				cmd = 'L'
			} else {
				cmd = 'l'
			}
		case 'L', 'l':
			x, y := abs(next(), next())
			cur = svgPoint{x, y}
		case 'C', 'c':
			next()
			next()
			next()
			next()
			x, y := abs(next(), next())
			cur = svgPoint{x, y}
		case 'Q', 'q':
			next()
			next()
			x, y := abs(next(), next())
			cur = svgPoint{x, y}
		case 'A', 'a':
			next()
			next()
			next()
			large, sweep := next(), next()
			if large != 0 && large != 1 {
				t.Fatalf("invalid large-arc flag %g", large)
			}
			if sweep != 0 && sweep != 1 {
				t.Fatalf("invalid sweep flag %g", sweep)
			}
			x, y := abs(next(), next())
			cur = svgPoint{x, y}
		case 'Z', 'z':
			cur = start
		default:
			t.Fatalf("unsupported command %q", string(cmd))
		}
	}
	return cur, start
}

// tokenizePath splits path data into command letters and numbers.
func tokenizePath(t *testing.T, d string) []string {
	t.Helper()
	var tokens []string
	isSep := func(c byte) bool { return c == ' ' || c == ',' || c == '\t' || c == '\n' }
	for i := 0; i < len(d); {
		c := d[i]
		switch {
		case isSep(c):
			i++
		case c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z':
			tokens = append(tokens, string(c))
			i++
		default:
			j := i
			if d[j] == '-' || d[j] == '+' {
				j++
			}
			for j < len(d) && (d[j] >= '0' && d[j] <= '9' || d[j] == '.' || d[j] == 'e' || d[j] == 'E' ||
				((d[j] == '-' || d[j] == '+') && (d[j-1] == 'e' || d[j-1] == 'E'))) {
				j++
			}
			if j == i {
				t.Fatalf("bad character %q in path data %q", string(c), d)
			}
			tokens = append(tokens, d[i:j])
			i = j
		}
	}
	return tokens
}

// TestPathBuilderParsed verifies the builder's output with a standard SVG
// path data interpretation of the canvas output.
func TestPathBuilderParsed(t *testing.T) {
	var buf bytes.Buffer
	svg := New(&buf)
	pb := fixedPath(svg.NewPath()).Close()
	if err := svg.DrawPath(pb, `fill="none"`, "stroke:black"); err != nil {
		t.Fatalf("DrawPath: %v", err)
	}

	// the canvas output must be a well-formed path element
	var el struct {
		XMLName xml.Name
		D       string `xml:"d,attr"`
		Fill    string `xml:"fill,attr"`
		Style   string `xml:"style,attr"`
	}
	if err := xml.Unmarshal(buf.Bytes(), &el); err != nil {
		t.Fatalf("output is not well-formed XML: %v\n%s", err, buf.String())
	}
	if el.D != fixedD+" Z" {
		t.Errorf("parsed d mismatch:\n got %s\nwant %s", el.D, fixedD+" Z")
	}
	if el.Fill != "none" || el.Style != "stroke:black" {
		t.Errorf("style mismatch: fill=%q style=%q", el.Fill, el.Style)
	}

	// a standard interpretation of the path data ends at the subpath
	// start (10,10) after the close, matching the builder's tracking
	cur, start := parseSVGPath(t, el.D)
	if cur != (svgPoint{10, 10}) || start != (svgPoint{10, 10}) {
		t.Errorf("parsed endpoints = cur(%g,%g) start(%g,%g), want (10,10)", cur.x, cur.y, start.x, start.y)
	}
	bx, by := pb.Pos()
	if bx != cur.x || by != cur.y {
		t.Errorf("builder Pos (%g,%g) disagrees with parsed current point (%g,%g)", bx, by, cur.x, cur.y)
	}

	// without the close, the standard interpretation ends at (180,60)
	d, err := fixedPath(NewPathBuilder(2)).String()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cur, _ = parseSVGPath(t, d)
	if cur != (svgPoint{180, 60}) {
		t.Errorf("parsed endpoint = (%g,%g), want (180,60)", cur.x, cur.y)
	}
}

// TestPathBuilderSmooth verifies smooth control point reflection and its
// reset after a non-curve command, per SVG semantics.
func TestPathBuilderSmooth(t *testing.T) {
	// after a cubic, the smooth control point is the reflection of the
	// previous second control point about the current point
	d, err := NewPathBuilder(2).
		MoveTo(0, 0).
		CubicTo(10, 10, 20, 10, 30, 0).
		SmoothCubicTo(50, -10, 60, 0).
		String()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "M0.00,0.00 C10.00,10.00 20.00,10.00 30.00,0.00 C40.00,-10.00 50.00,-10.00 60.00,0.00"
	if d != want {
		t.Errorf("smooth cubic:\n got %s\nwant %s", d, want)
	}

	// after a non-curve command the control point is the current point
	d, err = NewPathBuilder(2).
		MoveTo(0, 0).
		LineTo(10, 0).
		SmoothQuadTo(20, 0).
		String()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want = "M0.00,0.00 L10.00,0.00 Q10.00,0.00 20.00,0.00"
	if d != want {
		t.Errorf("smooth quad after line:\n got %s\nwant %s", d, want)
	}
}

// TestPathBuilderCloseRedraw verifies that after Close the current point
// is the subpath start and relative drawing continues from there.
func TestPathBuilderCloseRedraw(t *testing.T) {
	pb := NewPathBuilder(2).
		MoveTo(0, 0).
		LineTo(10, 0).
		Close().
		LineBy(0, 5)
	d, err := pb.String()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "M0.00,0.00 L10.00,0.00 Z l0.00,5.00"
	if d != want {
		t.Errorf("close and redraw:\n got %s\nwant %s", d, want)
	}
	if x, y := pb.Pos(); x != 0 || y != 5 {
		t.Errorf("Pos = (%g, %g), want (0, 5)", x, y)
	}
}

// TestPathBuilderZeroRadius verifies a zero-radius arc becomes a line.
func TestPathBuilderZeroRadius(t *testing.T) {
	d, err := NewPathBuilder(2).MoveTo(0, 0).ArcTo(0, 5, 0, false, false, 10, 10).String()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "M0.00,0.00 L10.00,10.00"; d != want {
		t.Errorf("zero radius arc:\n got %s\nwant %s", d, want)
	}
}

// TestPathBuilderErrors verifies that misuse produces locatable errors.
func TestPathBuilderErrors(t *testing.T) {
	cases := []struct {
		name    string
		build   func() *PathBuilder
		index   int
		cmd     string
		message string
	}{
		{"first command not a move", func() *PathBuilder { return NewPathBuilder(2).LineTo(1, 1) }, 0, "LineTo", "first path command"},
		{"close without move", func() *PathBuilder { return NewPathBuilder(2).Close() }, 0, "Close", "first path command"},
		{"non-finite coordinate", func() *PathBuilder { return NewPathBuilder(2).MoveTo(math.NaN(), 0) }, 0, "MoveTo", "non-finite"},
		{"infinite coordinate", func() *PathBuilder { return NewPathBuilder(2).MoveTo(0, 0).LineTo(math.Inf(1), 0) }, 1, "LineTo", "non-finite"},
		{"negative radius", func() *PathBuilder { return NewPathBuilder(2).MoveTo(0, 0).ArcTo(-5, 5, 0, false, false, 1, 1) }, 1, "ArcTo", "negative arc radius"},
		{"bad large-arc flag", func() *PathBuilder { return NewPathBuilder(2).MoveTo(0, 0).ArcToFlags(5, 5, 0, 2, 0, 1, 1) }, 1, "ArcToFlags", "invalid large-arc flag"},
		{"bad sweep flag", func() *PathBuilder { return NewPathBuilder(2).MoveTo(0, 0).ArcToFlags(5, 5, 0, 0, -1, 1, 1) }, 1, "ArcToFlags", "invalid sweep flag"},
		{"path too long", func() *PathBuilder {
			return NewPathBuilder(2).SetMaxCommands(3).MoveTo(0, 0).LineTo(1, 1).LineTo(2, 2).LineTo(3, 3)
		}, 3, "LineTo", "path too long"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pb := c.build()
			d, err := pb.String()
			if err == nil {
				t.Fatalf("expected error, got d=%q", d)
			}
			pe, ok := err.(*PathError)
			if !ok {
				t.Fatalf("error is %T, want *PathError", err)
			}
			if pe.Index != c.index || pe.Cmd != c.cmd {
				t.Errorf("error location = (%d, %s), want (%d, %s)", pe.Index, pe.Cmd, c.index, c.cmd)
			}
			if !strings.Contains(err.Error(), c.message) {
				t.Errorf("error %q does not mention %q", err, c.message)
			}
			// the error is sticky: further commands change nothing
			before := pb.Len()
			pb.MoveTo(0, 0).LineTo(1, 1)
			if pb.Len() != before {
				t.Errorf("commands were appended after the error")
			}
			// DrawPath reports the error and writes nothing
			var buf bytes.Buffer
			if derr := New(&buf).DrawPath(pb); derr == nil || buf.Len() != 0 {
				t.Errorf("DrawPath err=%v, wrote %d bytes", derr, buf.Len())
			}
		})
	}
}

// TestPathBuilderEmpty verifies an empty path is rejected.
func TestPathBuilderEmpty(t *testing.T) {
	if _, err := NewPathBuilder(2).String(); err == nil || !strings.Contains(err.Error(), "empty path") {
		t.Errorf("empty path: err = %v", err)
	}
	var buf bytes.Buffer
	if err := New(&buf).DrawPath(NewPathBuilder(2)); err == nil || buf.Len() != 0 {
		t.Errorf("DrawPath on empty path: err=%v, wrote %d bytes", err, buf.Len())
	}
}

// TestPathBuilderOldPath verifies the existing Path method is unchanged
// and composes with the builder.
func TestPathBuilderOldPath(t *testing.T) {
	var buf bytes.Buffer
	svg := New(&buf)
	svg.Path("M10,10 L20,20", "stroke:red")
	if !strings.Contains(buf.String(), `<path d="M10,10 L20,20"`) {
		t.Errorf("old Path output changed: %s", buf.String())
	}
	d, err := NewPathBuilder(2).MoveTo(10, 10).LineTo(20, 20).String()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	buf.Reset()
	svg.Path(d, "stroke:red")
	if !strings.Contains(buf.String(), `<path d="M10.00,10.00 L20.00,20.00"`) {
		t.Errorf("builder data via old Path: %s", buf.String())
	}
}
