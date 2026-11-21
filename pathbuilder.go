package gensvg

// PathBuilder: a chainable builder for SVG path data (the d attribute).
//
// A PathBuilder records the current point and the start of the current
// subpath as commands are appended, following SVG path semantics:
//
//   - The first command of a path must be a Move (MoveTo/MoveBy).
//   - Close draws back to the subpath start and makes it the current point;
//     drawing may continue from there without a new Move.
//   - Smooth curve commands reflect the previous control point about the
//     current point; when the previous command was not a matching curve,
//     the control point coincides with the current point.
//   - An arc with a zero radius is rendered as a straight line.
//   - An empty path (no commands) is not valid output.
//
// The first error encountered is recorded, reported by String, DrawPath
// and Err, and stops further commands from being appended, so a bad
// coordinate cannot silently corrupt the whole path.
//
// https://www.w3.org/TR/SVG11/paths.html

import (
	"fmt"
	"math"
	"strings"
)

// DefaultMaxPathCommands bounds the number of commands in one path,
// guarding against runaway loops producing unbounded path data.
const DefaultMaxPathCommands = 8192

// Path command kinds, used to implement smooth curve reflection.
const (
	pcNone  = iota // no previous curve control point
	pcCubic        // previous command was a cubic curve
	pcQuad         // previous command was a quadratic curve
)

// PathError locates a failure at a specific path command.
type PathError struct {
	Index int    // zero-based index of the offending command
	Cmd   string // command name, e.g. "LineTo"
	Err   error  // the underlying failure
}

// Error describes the failing command and its position in the path.
func (e *PathError) Error() string {
	return fmt.Sprintf("gensvg: path command %d (%s): %v", e.Index, e.Cmd, e.Err)
}

// Unwrap returns the underlying error.
func (e *PathError) Unwrap() error { return e.Err }

// PathBuilder builds SVG path data command by command.
type PathBuilder struct {
	d        strings.Builder
	decimals int
	cmds     int
	maxCmds  int
	curX     float64 // current point
	curY     float64
	startX   float64 // start of the current subpath
	startY   float64
	ctrlX    float64 // last curve control point (absolute)
	ctrlY    float64
	prev     int // pcNone, pcCubic or pcQuad
	started  bool
	err      error
}

// NewPathBuilder makes a path builder formatting coordinates with the
// given number of decimals, matching the rest of the library's output.
func NewPathBuilder(decimals int) *PathBuilder {
	return &PathBuilder{decimals: decimals, maxCmds: DefaultMaxPathCommands}
}

// NewPath makes a path builder using the canvas' decimal setting.
func (svg *SVG) NewPath() *PathBuilder {
	return NewPathBuilder(svg.Decimals)
}

// SetMaxCommands changes the command limit; a non-positive limit
// restores the default. It returns the builder for chaining.
func (pb *PathBuilder) SetMaxCommands(n int) *PathBuilder {
	if n <= 0 {
		n = DefaultMaxPathCommands
	}
	pb.maxCmds = n
	return pb
}

// Err returns the first error recorded by the builder, if any.
func (pb *PathBuilder) Err() error { return pb.err }

// Pos returns the current point of the path.
func (pb *PathBuilder) Pos() (x, y float64) { return pb.curX, pb.curY }

// SubpathStart returns the start point of the current subpath,
// which is where Close returns to.
func (pb *PathBuilder) SubpathStart() (x, y float64) { return pb.startX, pb.startY }

// Len returns the number of commands appended so far.
func (pb *PathBuilder) Len() int { return pb.cmds }

// fail records the first error, tagged with the command position.
func (pb *PathBuilder) fail(cmd string, err error) *PathBuilder {
	if pb.err == nil {
		pb.err = &PathError{Index: pb.cmds, Cmd: cmd, Err: err}
	}
	return pb
}

// ok reports whether another command may be appended, enforcing the
// command limit so a runaway loop cannot produce an unbounded path.
func (pb *PathBuilder) ok(cmd string) bool {
	if pb.err != nil {
		return false
	}
	if pb.cmds >= pb.maxCmds {
		pb.fail(cmd, fmt.Errorf("path too long: exceeds the limit of %d commands", pb.maxCmds))
		return false
	}
	return true
}

// finite verifies that every coordinate is a finite number.
func (pb *PathBuilder) finite(cmd string, v ...float64) bool {
	for _, n := range v {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			pb.fail(cmd, fmt.Errorf("non-finite coordinate %v", n))
			return false
		}
	}
	return true
}

// needMove enforces the SVG rule that a path begins with a moveto.
func (pb *PathBuilder) needMove(cmd string) bool {
	if !pb.started {
		pb.fail(cmd, fmt.Errorf("the first path command must be MoveTo or MoveBy"))
		return false
	}
	return true
}

// emit appends one command and updates the current point.
func (pb *PathBuilder) emit(s string, x, y float64) {
	if pb.cmds > 0 {
		pb.d.WriteByte(' ')
	}
	pb.d.WriteString(s)
	pb.curX, pb.curY = x, y
	pb.cmds++
}

// pair formats an absolute coordinate pair.
func (pb *PathBuilder) pair(x, y float64) string { return coord(x, y, pb.decimals) }

// MoveTo begins a new subpath at (x, y).
func (pb *PathBuilder) MoveTo(x, y float64) *PathBuilder {
	if !pb.ok("MoveTo") || !pb.finite("MoveTo", x, y) {
		return pb
	}
	pb.emit("M"+pb.pair(x, y), x, y)
	pb.startX, pb.startY = x, y
	pb.started = true
	pb.prev = pcNone
	return pb
}

// MoveBy begins a new subpath offset by (dx, dy) from the current point.
// As the first command it is relative to the origin, per SVG semantics.
func (pb *PathBuilder) MoveBy(dx, dy float64) *PathBuilder {
	if !pb.ok("MoveBy") || !pb.finite("MoveBy", dx, dy) {
		return pb
	}
	x, y := pb.curX+dx, pb.curY+dy
	pb.emit("m"+pb.pair(dx, dy), x, y)
	pb.startX, pb.startY = x, y
	pb.started = true
	pb.prev = pcNone
	return pb
}

// LineTo draws a straight line to (x, y).
func (pb *PathBuilder) LineTo(x, y float64) *PathBuilder {
	if !pb.ok("LineTo") || !pb.finite("LineTo", x, y) || !pb.needMove("LineTo") {
		return pb
	}
	pb.emit("L"+pb.pair(x, y), x, y)
	pb.prev = pcNone
	return pb
}

// LineBy draws a straight line offset by (dx, dy) from the current point.
func (pb *PathBuilder) LineBy(dx, dy float64) *PathBuilder {
	if !pb.ok("LineBy") || !pb.finite("LineBy", dx, dy) || !pb.needMove("LineBy") {
		return pb
	}
	pb.emit("l"+pb.pair(dx, dy), pb.curX+dx, pb.curY+dy)
	pb.prev = pcNone
	return pb
}

// QuadTo draws a quadratic Bezier curve with control point (cx, cy),
// ending at (x, y).
func (pb *PathBuilder) QuadTo(cx, cy, x, y float64) *PathBuilder {
	if !pb.ok("QuadTo") || !pb.finite("QuadTo", cx, cy, x, y) || !pb.needMove("QuadTo") {
		return pb
	}
	pb.emit("Q"+pb.pair(cx, cy)+" "+pb.pair(x, y), x, y)
	pb.ctrlX, pb.ctrlY = cx, cy
	pb.prev = pcQuad
	return pb
}

// QuadBy draws a quadratic Bezier curve with control point and end point
// given as offsets from the current point.
func (pb *PathBuilder) QuadBy(dcx, dcy, dx, dy float64) *PathBuilder {
	if !pb.ok("QuadBy") || !pb.finite("QuadBy", dcx, dcy, dx, dy) || !pb.needMove("QuadBy") {
		return pb
	}
	cx, cy := pb.curX+dcx, pb.curY+dcy
	pb.emit("q"+pb.pair(dcx, dcy)+" "+pb.pair(dx, dy), pb.curX+dx, pb.curY+dy)
	pb.ctrlX, pb.ctrlY = cx, cy
	pb.prev = pcQuad
	return pb
}

// SmoothQuadTo draws a quadratic Bezier curve to (x, y) whose control
// point is the reflection of the previous quadratic control point about
// the current point; after any other command it is the current point
// itself, per SVG semantics. The resolved control point is emitted as a
// plain Q command.
func (pb *PathBuilder) SmoothQuadTo(x, y float64) *PathBuilder {
	if !pb.ok("SmoothQuadTo") || !pb.finite("SmoothQuadTo", x, y) || !pb.needMove("SmoothQuadTo") {
		return pb
	}
	cx, cy := pb.curX, pb.curY
	if pb.prev == pcQuad {
		cx, cy = 2*pb.curX-pb.ctrlX, 2*pb.curY-pb.ctrlY
	}
	pb.emit("Q"+pb.pair(cx, cy)+" "+pb.pair(x, y), x, y)
	pb.ctrlX, pb.ctrlY = cx, cy
	pb.prev = pcQuad
	return pb
}

// SmoothQuadBy is SmoothQuadTo with the end point given as an offset
// from the current point.
func (pb *PathBuilder) SmoothQuadBy(dx, dy float64) *PathBuilder {
	if !pb.ok("SmoothQuadBy") || !pb.finite("SmoothQuadBy", dx, dy) || !pb.needMove("SmoothQuadBy") {
		return pb
	}
	return pb.SmoothQuadTo(pb.curX+dx, pb.curY+dy)
}

// CubicTo draws a cubic Bezier curve with control points (c1x, c1y) and
// (c2x, c2y), ending at (x, y).
func (pb *PathBuilder) CubicTo(c1x, c1y, c2x, c2y, x, y float64) *PathBuilder {
	if !pb.ok("CubicTo") || !pb.finite("CubicTo", c1x, c1y, c2x, c2y, x, y) || !pb.needMove("CubicTo") {
		return pb
	}
	pb.emit("C"+pb.pair(c1x, c1y)+" "+pb.pair(c2x, c2y)+" "+pb.pair(x, y), x, y)
	pb.ctrlX, pb.ctrlY = c2x, c2y
	pb.prev = pcCubic
	return pb
}

// CubicBy draws a cubic Bezier curve with control points and end point
// given as offsets from the current point.
func (pb *PathBuilder) CubicBy(dc1x, dc1y, dc2x, dc2y, dx, dy float64) *PathBuilder {
	if !pb.ok("CubicBy") || !pb.finite("CubicBy", dc1x, dc1y, dc2x, dc2y, dx, dy) || !pb.needMove("CubicBy") {
		return pb
	}
	c2x, c2y := pb.curX+dc2x, pb.curY+dc2y
	pb.emit("c"+pb.pair(dc1x, dc1y)+" "+pb.pair(dc2x, dc2y)+" "+pb.pair(dx, dy),
		pb.curX+dx, pb.curY+dy)
	pb.ctrlX, pb.ctrlY = c2x, c2y
	pb.prev = pcCubic
	return pb
}

// SmoothCubicTo draws a cubic Bezier curve to (x, y) whose first control
// point is the reflection of the previous cubic control point about the
// current point; after any other command it is the current point itself,
// per SVG semantics. The resolved control point is emitted as a plain C
// command.
func (pb *PathBuilder) SmoothCubicTo(c2x, c2y, x, y float64) *PathBuilder {
	if !pb.ok("SmoothCubicTo") || !pb.finite("SmoothCubicTo", c2x, c2y, x, y) || !pb.needMove("SmoothCubicTo") {
		return pb
	}
	c1x, c1y := pb.curX, pb.curY
	if pb.prev == pcCubic {
		c1x, c1y = 2*pb.curX-pb.ctrlX, 2*pb.curY-pb.ctrlY
	}
	pb.emit("C"+pb.pair(c1x, c1y)+" "+pb.pair(c2x, c2y)+" "+pb.pair(x, y), x, y)
	pb.ctrlX, pb.ctrlY = c2x, c2y
	pb.prev = pcCubic
	return pb
}

// SmoothCubicBy is SmoothCubicTo with control point and end point given
// as offsets from the current point.
func (pb *PathBuilder) SmoothCubicBy(dc2x, dc2y, dx, dy float64) *PathBuilder {
	if !pb.ok("SmoothCubicBy") || !pb.finite("SmoothCubicBy", dc2x, dc2y, dx, dy) || !pb.needMove("SmoothCubicBy") {
		return pb
	}
	return pb.SmoothCubicTo(pb.curX+dc2x, pb.curY+dc2y, pb.curX+dx, pb.curY+dy)
}

// ArcTo draws an elliptical arc to (x, y) with radii (rx, ry), x-axis
// rotation rot in degrees, and the large-arc and sweep flags. Radii must
// be non-negative; a zero radius draws a straight line, per SVG semantics.
func (pb *PathBuilder) ArcTo(rx, ry, rot float64, large, sweep bool, x, y float64) *PathBuilder {
	l, s := 0, 0
	if large {
		l = 1
	}
	if sweep {
		s = 1
	}
	return pb.arcTo("ArcTo", rx, ry, rot, l, s, x, y, false)
}

// ArcBy is ArcTo with the end point given as an offset from the current
// point.
func (pb *PathBuilder) ArcBy(rx, ry, rot float64, large, sweep bool, dx, dy float64) *PathBuilder {
	l, s := 0, 0
	if large {
		l = 1
	}
	if sweep {
		s = 1
	}
	return pb.arcTo("ArcBy", rx, ry, rot, l, s, dx, dy, true)
}

// ArcToFlags is ArcTo with the large-arc and sweep flags given as
// integers, which must be 0 or 1; any other value is an error.
func (pb *PathBuilder) ArcToFlags(rx, ry, rot float64, large, sweep int, x, y float64) *PathBuilder {
	return pb.arcTo("ArcToFlags", rx, ry, rot, large, sweep, x, y, false)
}

// arcTo validates and emits an elliptical arc command.
func (pb *PathBuilder) arcTo(cmd string, rx, ry, rot float64, large, sweep int, x, y float64, rel bool) *PathBuilder {
	if !pb.ok(cmd) || !pb.finite(cmd, rx, ry, rot, x, y) || !pb.needMove(cmd) {
		return pb
	}
	if rx < 0 || ry < 0 {
		return pb.fail(cmd, fmt.Errorf("negative arc radius (%g, %g)", rx, ry))
	}
	if large != 0 && large != 1 {
		return pb.fail(cmd, fmt.Errorf("invalid large-arc flag %d, must be 0 or 1", large))
	}
	if sweep != 0 && sweep != 1 {
		return pb.fail(cmd, fmt.Errorf("invalid sweep flag %d, must be 0 or 1", sweep))
	}
	if rx == 0 || ry == 0 {
		// Per SVG semantics a zero-radius arc is a straight line.
		if rel {
			return pb.LineBy(x-pb.curX, y-pb.curY)
		}
		return pb.LineTo(x, y)
	}
	d := pb.decimals
	ex, ey := x, y
	if rel {
		ex, ey = pb.curX+x, pb.curY+y
	}
	letter := "A"
	if rel {
		letter = "a"
	}
	pb.emit(fmt.Sprintf("%s%s %.*f %d %d %s", letter, pb.pair(rx, ry), d, rot, large, sweep, pb.pair(x, y)), ex, ey)
	pb.prev = pcNone
	return pb
}

// Close closes the current subpath: it draws back to the subpath start
// and makes that the current point, so drawing may continue from there.
func (pb *PathBuilder) Close() *PathBuilder {
	if !pb.ok("Close") || !pb.needMove("Close") {
		return pb
	}
	pb.emit("Z", pb.startX, pb.startY)
	pb.prev = pcNone
	return pb
}

// String returns the path data, or the first error recorded. A path with
// no commands is an error, since an empty d attribute is not valid SVG.
func (pb *PathBuilder) String() (string, error) {
	if pb.err != nil {
		return "", pb.err
	}
	if pb.cmds == 0 {
		return "", &PathError{Index: 0, Cmd: "String", Err: fmt.Errorf("empty path: no commands were added")}
	}
	return pb.d.String(), nil
}

// DrawPath writes the builder's path to the canvas using the existing
// Path method, with optional style, returning any builder error.
func (svg *SVG) DrawPath(pb *PathBuilder, s ...string) error {
	d, err := pb.String()
	if err != nil {
		return err
	}
	svg.Path(d, s...)
	return nil
}
