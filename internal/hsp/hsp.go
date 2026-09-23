// Package hsp reads and writes HereSphere profile (.hsp) files, version 7.
//
// A file is an Unreal compressed archive: a header of little-endian int64s
// (package tag, chunk size, total compressed and raw size, then one
// compressed/raw pair per chunk) followed by the zlib-compressed chunks.
// Profiles are small, so in practice there is one chunk and the header is
// 48 bytes. The raw payload is described by Profile.
package hsp

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
	"unicode/utf16"
)

// Version is the only profile version this package reads and writes.
const Version = 7

const (
	packageTag = 0x9E2A83C1
	chunkSize  = 131072
	headerSize = 48
	// maxRawSize bounds the decompressed payload of a file being decoded.
	maxRawSize = 16 << 20
)

// ErrInvalid is wrapped by every error Decode returns for malformed input.
var ErrInvalid = errors.New("invalid HereSphere profile")

// Projection modes.
const (
	ProjectionPerspective        uint8 = 0
	ProjectionEquirectangular    uint8 = 1
	ProjectionFisheye            uint8 = 2
	ProjectionEquirectangular360 uint8 = 3
	ProjectionCubemap            uint8 = 4
	ProjectionEquiangularCubemap uint8 = 5
)

// Stereo modes.
const (
	StereoMono       uint8 = 0
	StereoSideBySide uint8 = 1
	StereoTopBottom  uint8 = 2
)

// Background types.
const (
	BackgroundGlobal      uint8 = 0
	BackgroundColor       uint8 = 1
	BackgroundPassthrough uint8 = 2
)

// Mask types.
const (
	MaskNone        uint8 = 0
	MaskChromaKey   uint8 = 1
	MaskAlphaPacked uint8 = 2
)

// Vec2 is an Unreal FVector2D.
type Vec2 struct{ X, Y float32 }

// Vec3 is an Unreal FVector.
type Vec3 struct{ X, Y, Z float32 }

// Vec4 is an Unreal FVector4.
type Vec4 struct{ X, Y, Z, W float32 }

// Rotator is an Unreal FRotator, in degrees.
type Rotator struct{ Pitch, Yaw, Roll float32 }

// Color is an Unreal FLinearColor.
type Color struct{ R, G, B, A float32 }

// Times are Unreal ticks of 100 ns: FDateTime counts from 0001-01-01 UTC,
// FTimespan is a duration.

// Tag is one HereSphere tag.
type Tag struct {
	Name   string
	Rating float32
	Start  int64
	End    int64
	Track  uint32
}

// FormatKey sets projection, stereo mode and screen framing.
type FormatKey struct {
	Time        int64
	Projection  uint8
	Stereo      uint8
	EyeSwap     bool
	ForceMono   bool
	FlipFB      bool
	FlipLR      bool
	FlipUD      bool
	AspectType  uint8
	Aspect      float32
	Zoom        Vec2
	Pan         Vec2
	Orientation uint8
}

// LensKey sets the lens model and field of view.
type LensKey struct {
	Time       int64
	TrueLens   string
	ExportLens string
	TrueCal    Vec4
	ExportCal  Vec4
	TrueFOV    float32
	ExportFOV  float32
}

// StitchKey adjusts stereo stitching.
type StitchKey struct {
	Time                              int64
	Shift, Scale, Shear, Flare, Slant Vec4
}

// AlignmentKey places the screen.
type AlignmentKey struct {
	Time     int64
	Position Vec3
	Rotation Rotator
}

// OrientationKey rotates the video.
type OrientationKey struct {
	Time     int64
	Rotation Rotator
}

// OriginKey moves the viewing origin.
type OriginKey struct {
	Time   int64
	Origin Vec3
}

// MotionKey sets the motion distance.
type MotionKey struct {
	Time     int64
	Distance float32
}

// AutoFocusKey configures auto focus.
type AutoFocusKey struct {
	Time     int64
	Rotation Rotator
	Focal    float32
	Min      float32
	Max      float32
	Speed    float32
}

// SyncKey offsets playback timing.
type SyncKey struct {
	Time            int64
	StartingOffset  int64
	TicksPerHourAdj int64
}

// TransitionKey sets the transition between keyframes.
type TransitionKey struct {
	Time     int64
	Type     uint8
	Duration int64
}

// ImageKey adjusts colour and sharpness.
type ImageKey struct {
	Time                             int64
	Sharpness, Exposure, Temperature float32
	Tint                             float32
	Saturation, Contrast, Gamma      Vec4
	Gain, Offset                     Vec4
	Shadows, Midtones, Highlights    float32
}

// AudioKey adjusts audio offset and volume.
type AudioKey struct {
	Time   int64
	Offset float32
	Volume float32
}

// EnvironmentKey sets background and mask.
type EnvironmentKey struct {
	Time            int64
	Background      uint8
	BackgroundColor Color
	BackgroundName  string
	Mask            uint8
	Opacity         float32
	ChromaKeys      [6]Color
	Despill         [2]Color
	Light           Color
	AlphaCoords     Color
}

// Profile is the payload of a version 7 profile.
type Profile struct {
	Version     uint32
	ID          string
	Title       string
	Description string

	DateAdded, DateReleased, DateLastPlayed, DateEdited int64
	Duration, Resume, ABStart, ABEnd                    int64

	PlayCount     uint32
	Comments      uint32
	Favorites     uint32
	IsFavorite    bool
	AverageRating float32
	AudioTrack    uint32
	Tags          []Tag

	Format      []FormatKey
	Lens        []LensKey
	Stitch      []StitchKey
	Alignment   []AlignmentKey
	Orientation []OrientationKey
	Origin      []OriginKey
	Motion      []MotionKey
	AutoFocus   []AutoFocusKey
	Sync        []SyncKey
	Transition  []TransitionKey
	Image       []ImageKey
	Audio       []AudioKey
	Environment []EnvironmentKey
}

// ticksPerSecond is the number of 100 ns ticks in a second.
const ticksPerSecond = 10_000_000

// unixEpochTicks is 1970-01-01 in FDateTime ticks.
const unixEpochTicks = 621355968000000000

// TimespanTicks converts seconds to FTimespan ticks.
func TimespanTicks(seconds float64) int64 {
	return int64(math.Round(seconds * ticksPerSecond))
}

// DateTicks converts t to FDateTime ticks; the zero time is 0.
func DateTicks(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()*ticksPerSecond + int64(t.Nanosecond())/100 + unixEpochTicks
}

// Decode parses a complete profile file.
func Decode(data []byte) (*Profile, error) {
	raw, err := decompress(data)
	if err != nil {
		return nil, err
	}
	r := &reader{b: raw}
	p := r.profile()
	if r.err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, r.err)
	}
	if r.off != len(raw) {
		return nil, fmt.Errorf("%w: %d unread payload bytes", ErrInvalid, len(raw)-r.off)
	}
	return p, nil
}

func decompress(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("%w: %d bytes is too short", ErrInvalid, len(data))
	}
	le := binary.LittleEndian
	tag, size := int64(le.Uint64(data)), int64(le.Uint64(data[8:]))
	totalC, totalU := int64(le.Uint64(data[16:])), int64(le.Uint64(data[24:]))
	if tag != packageTag {
		return nil, fmt.Errorf("%w: bad package tag %#x", ErrInvalid, tag)
	}
	if size <= 0 || totalU <= 0 || totalU > maxRawSize || totalC <= 0 {
		return nil, fmt.Errorf("%w: bad sizes (chunk %d, compressed %d, raw %d)", ErrInvalid, size, totalC, totalU)
	}
	n := int((totalU + size - 1) / size)
	off := 32 + 16*n
	if len(data) < off {
		return nil, fmt.Errorf("%w: header of %d chunks is truncated", ErrInvalid, n)
	}
	raw := make([]byte, 0, totalU)
	var sumC int64
	for i := range n {
		c, u := int64(le.Uint64(data[32+16*i:])), int64(le.Uint64(data[40+16*i:]))
		if c <= 0 || u <= 0 || u > size || u > totalU-int64(len(raw)) || c > int64(len(data)-off) {
			return nil, fmt.Errorf("%w: bad chunk %d sizes (compressed %d, raw %d)", ErrInvalid, i, c, u)
		}
		chunk, err := inflate(data[off:off+int(c)], u)
		if err != nil {
			return nil, fmt.Errorf("%w: chunk %d: %w", ErrInvalid, i, err)
		}
		raw = append(raw, chunk...)
		off += int(c)
		sumC += c
	}
	if off != len(data) {
		return nil, fmt.Errorf("%w: %d bytes after the last chunk", ErrInvalid, len(data)-off)
	}
	if sumC != totalC || int64(len(raw)) != totalU {
		return nil, fmt.Errorf("%w: sizes do not add up (compressed %d/%d, raw %d/%d)", ErrInvalid, sumC, totalC, len(raw), totalU)
	}
	return raw, nil
}

func inflate(src []byte, size int64) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	out, err := io.ReadAll(io.LimitReader(zr, size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(out)) != size {
		return nil, fmt.Errorf("inflated to %d bytes, header says %d", len(out), size)
	}
	return out, nil
}

// Encode writes p as a complete profile file.
func Encode(p *Profile) ([]byte, error) {
	w := &writer{}
	w.profile(p)
	raw := w.b.Bytes()
	if len(raw) > math.MaxUint32 {
		return nil, fmt.Errorf("profile payload of %d bytes is too large", len(raw))
	}
	binary.LittleEndian.PutUint32(raw, uint32(len(raw)-4))

	var chunks [][]byte
	for start := 0; start < len(raw); start += chunkSize {
		end := min(start+chunkSize, len(raw))
		var z bytes.Buffer
		zw := zlib.NewWriter(&z)
		if _, err := zw.Write(raw[start:end]); err != nil {
			return nil, err
		}
		if err := zw.Close(); err != nil {
			return nil, err
		}
		chunks = append(chunks, z.Bytes())
	}
	var total int
	for _, c := range chunks {
		total += len(c)
	}
	var out bytes.Buffer
	put := func(v int) {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], uint64(v))
		out.Write(b[:])
	}
	put(packageTag)
	put(chunkSize)
	put(total)
	put(len(raw))
	for i, c := range chunks {
		put(len(c))
		put(min(chunkSize, len(raw)-i*chunkSize))
	}
	for _, c := range chunks {
		out.Write(c)
	}
	return out.Bytes(), nil
}

// reader consumes the raw payload; the first error sticks and later reads
// return zero values.
type reader struct {
	b   []byte
	off int
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || len(r.b)-r.off < n {
		r.err = fmt.Errorf("truncated at offset %d reading %d bytes", r.off, n)
		return nil
	}
	s := r.b[r.off : r.off+n]
	r.off += n
	return s
}

func (r *reader) u8() uint8 {
	if s := r.take(1); s != nil {
		return s[0]
	}
	return 0
}

func (r *reader) u32() uint32 {
	if s := r.take(4); s != nil {
		return binary.LittleEndian.Uint32(s)
	}
	return 0
}

func (r *reader) i64() int64 {
	if s := r.take(8); s != nil {
		return int64(binary.LittleEndian.Uint64(s))
	}
	return 0
}

func (r *reader) f32() float32 { return math.Float32frombits(r.u32()) }
func (r *reader) bool32() bool { return r.u32() != 0 }
func (r *reader) vec2() Vec2   { return Vec2{r.f32(), r.f32()} }
func (r *reader) vec3() Vec3   { return Vec3{r.f32(), r.f32(), r.f32()} }
func (r *reader) vec4() Vec4   { return Vec4{r.f32(), r.f32(), r.f32(), r.f32()} }
func (r *reader) rot() Rotator { return Rotator{r.f32(), r.f32(), r.f32()} }
func (r *reader) color() Color { return Color{r.f32(), r.f32(), r.f32(), r.f32()} }

// fstring reads an Unreal FString: an int32 length including the NUL,
// negative for UTF-16LE code units, positive for single-byte (Latin-1)
// characters, 0 for empty.
func (r *reader) fstring() string {
	n := int32(r.u32())
	switch {
	case r.err != nil || n == 0:
		return ""
	case n > 0:
		s := r.take(int(n))
		if s == nil {
			return ""
		}
		if s[n-1] != 0 {
			r.err = fmt.Errorf("string at offset %d is not NUL-terminated", r.off-int(n))
			return ""
		}
		runes := make([]rune, n-1)
		for i, c := range s[:n-1] {
			runes[i] = rune(c)
		}
		return string(runes)
	default:
		if n == math.MinInt32 {
			r.err = fmt.Errorf("bad string length at offset %d", r.off-4)
			return ""
		}
		units := int(-n)
		s := r.take(units * 2)
		if s == nil {
			return ""
		}
		u := make([]uint16, units)
		for i := range u {
			u[i] = binary.LittleEndian.Uint16(s[2*i:])
		}
		if u[units-1] != 0 {
			r.err = fmt.Errorf("string at offset %d is not NUL-terminated", r.off-units*2)
			return ""
		}
		return string(utf16.Decode(u[:units-1]))
	}
}

// count reads an array length and checks that at least minItem bytes per
// item remain, so a corrupt count cannot force a huge allocation.
func (r *reader) count(minItem int) int {
	n := r.u32()
	if r.err != nil {
		return 0
	}
	if uint64(n)*uint64(minItem) > uint64(len(r.b)-r.off) {
		r.err = fmt.Errorf("array of %d items at offset %d exceeds the payload", n, r.off-4)
		return 0
	}
	return int(n)
}

func readArray[T any](r *reader, minItem int, item func() T) []T {
	n := r.count(minItem)
	out := make([]T, 0, n)
	for range n {
		if r.err != nil {
			return nil
		}
		out = append(out, item())
	}
	return out
}

func (r *reader) profile() *Profile {
	p := &Profile{}
	r.u32() // content length, implied by the chunk sizes
	p.Version = r.u32()
	if r.err == nil && p.Version != Version {
		r.err = fmt.Errorf("version %d is not supported, only %d", p.Version, Version)
		return nil
	}
	p.ID, p.Title, p.Description = r.fstring(), r.fstring(), r.fstring()
	p.DateAdded, p.DateReleased, p.DateLastPlayed, p.DateEdited = r.i64(), r.i64(), r.i64(), r.i64()
	p.Duration, p.Resume, p.ABStart, p.ABEnd = r.i64(), r.i64(), r.i64(), r.i64()
	p.PlayCount, p.Comments, p.Favorites = r.u32(), r.u32(), r.u32()
	p.IsFavorite = r.bool32()
	p.AverageRating = r.f32()
	p.AudioTrack = r.u32()
	p.Tags = readArray(r, 28, func() Tag {
		return Tag{Name: r.fstring(), Rating: r.f32(), Start: r.i64(), End: r.i64(), Track: r.u32()}
	})
	p.Format = readArray(r, 47, func() FormatKey {
		return FormatKey{Time: r.i64(), Projection: r.u8(), Stereo: r.u8(), EyeSwap: r.bool32(), ForceMono: r.bool32(),
			FlipFB: r.bool32(), FlipLR: r.bool32(), FlipUD: r.bool32(), AspectType: r.u8(), Aspect: r.f32(),
			Zoom: r.vec2(), Pan: r.vec2(), Orientation: r.u8()}
	})
	p.Lens = readArray(r, 56, func() LensKey {
		return LensKey{Time: r.i64(), TrueLens: r.fstring(), ExportLens: r.fstring(), TrueCal: r.vec4(), ExportCal: r.vec4(),
			TrueFOV: r.f32(), ExportFOV: r.f32()}
	})
	p.Stitch = readArray(r, 88, func() StitchKey {
		return StitchKey{Time: r.i64(), Shift: r.vec4(), Scale: r.vec4(), Shear: r.vec4(), Flare: r.vec4(), Slant: r.vec4()}
	})
	p.Alignment = readArray(r, 32, func() AlignmentKey {
		return AlignmentKey{Time: r.i64(), Position: r.vec3(), Rotation: r.rot()}
	})
	p.Orientation = readArray(r, 20, func() OrientationKey {
		return OrientationKey{Time: r.i64(), Rotation: r.rot()}
	})
	p.Origin = readArray(r, 20, func() OriginKey {
		return OriginKey{Time: r.i64(), Origin: r.vec3()}
	})
	p.Motion = readArray(r, 12, func() MotionKey {
		return MotionKey{Time: r.i64(), Distance: r.f32()}
	})
	p.AutoFocus = readArray(r, 36, func() AutoFocusKey {
		return AutoFocusKey{Time: r.i64(), Rotation: r.rot(), Focal: r.f32(), Min: r.f32(), Max: r.f32(), Speed: r.f32()}
	})
	p.Sync = readArray(r, 24, func() SyncKey {
		return SyncKey{Time: r.i64(), StartingOffset: r.i64(), TicksPerHourAdj: r.i64()}
	})
	p.Transition = readArray(r, 17, func() TransitionKey {
		return TransitionKey{Time: r.i64(), Type: r.u8(), Duration: r.i64()}
	})
	p.Image = readArray(r, 108, func() ImageKey {
		return ImageKey{Time: r.i64(), Sharpness: r.f32(), Exposure: r.f32(), Temperature: r.f32(), Tint: r.f32(),
			Saturation: r.vec4(), Contrast: r.vec4(), Gamma: r.vec4(), Gain: r.vec4(), Offset: r.vec4(),
			Shadows: r.f32(), Midtones: r.f32(), Highlights: r.f32()}
	})
	p.Audio = readArray(r, 16, func() AudioKey {
		return AudioKey{Time: r.i64(), Offset: r.f32(), Volume: r.f32()}
	})
	p.Environment = readArray(r, 194, func() EnvironmentKey {
		k := EnvironmentKey{Time: r.i64(), Background: r.u8(), BackgroundColor: r.color(), BackgroundName: r.fstring(),
			Mask: r.u8(), Opacity: r.f32()}
		for i := range k.ChromaKeys {
			k.ChromaKeys[i] = r.color()
		}
		for i := range k.Despill {
			k.Despill[i] = r.color()
		}
		k.Light, k.AlphaCoords = r.color(), r.color()
		return k
	})
	return p
}

type writer struct{ b bytes.Buffer }

func (w *writer) u8(v uint8) { w.b.WriteByte(v) }

func (w *writer) u32(v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	w.b.Write(b[:])
}

func (w *writer) i64(v int64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(v))
	w.b.Write(b[:])
}

func (w *writer) f32(v float32) { w.u32(math.Float32bits(v)) }

func (w *writer) bool32(v bool) {
	if v {
		w.u32(1)
		return
	}
	w.u32(0)
}

func (w *writer) vec2(v Vec2)   { w.f32(v.X); w.f32(v.Y) }
func (w *writer) vec3(v Vec3)   { w.f32(v.X); w.f32(v.Y); w.f32(v.Z) }
func (w *writer) vec4(v Vec4)   { w.f32(v.X); w.f32(v.Y); w.f32(v.Z); w.f32(v.W) }
func (w *writer) rot(v Rotator) { w.f32(v.Pitch); w.f32(v.Yaw); w.f32(v.Roll) }
func (w *writer) color(v Color) { w.f32(v.R); w.f32(v.G); w.f32(v.B); w.f32(v.A) }
func (w *writer) count(n int)   { w.u32(uint32(n)) }
func (w *writer) i32(v int32)   { w.u32(uint32(v)) }
func (w *writer) raw(b []byte)  { w.b.Write(b) }
func (w *writer) nul(units int) { w.raw(make([]byte, units)) }
func (w *writer) latin1(s string) {
	for _, c := range s {
		w.u8(uint8(c))
	}
}

// fstring writes s the way Unreal does: single bytes when every character
// is ASCII, else UTF-16LE with a negative length.
func (w *writer) fstring(s string) {
	if s == "" {
		w.i32(0)
		return
	}
	ascii := true
	for _, c := range s {
		if c >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		w.i32(int32(len(s) + 1))
		w.latin1(s)
		w.nul(1)
		return
	}
	u := utf16.Encode([]rune(s))
	w.i32(-int32(len(u) + 1))
	for _, c := range u {
		w.raw([]byte{byte(c), byte(c >> 8)})
	}
	w.nul(2)
}

func (w *writer) profile(p *Profile) {
	w.u32(0) // content length, filled in by Encode
	w.u32(p.Version)
	w.fstring(p.ID)
	w.fstring(p.Title)
	w.fstring(p.Description)
	for _, v := range []int64{p.DateAdded, p.DateReleased, p.DateLastPlayed, p.DateEdited, p.Duration, p.Resume, p.ABStart, p.ABEnd} {
		w.i64(v)
	}
	w.u32(p.PlayCount)
	w.u32(p.Comments)
	w.u32(p.Favorites)
	w.bool32(p.IsFavorite)
	w.f32(p.AverageRating)
	w.u32(p.AudioTrack)
	w.count(len(p.Tags))
	for _, t := range p.Tags {
		w.fstring(t.Name)
		w.f32(t.Rating)
		w.i64(t.Start)
		w.i64(t.End)
		w.u32(t.Track)
	}
	w.count(len(p.Format))
	for i := range p.Format {
		k := &p.Format[i]
		w.i64(k.Time)
		w.u8(k.Projection)
		w.u8(k.Stereo)
		w.bool32(k.EyeSwap)
		w.bool32(k.ForceMono)
		w.bool32(k.FlipFB)
		w.bool32(k.FlipLR)
		w.bool32(k.FlipUD)
		w.u8(k.AspectType)
		w.f32(k.Aspect)
		w.vec2(k.Zoom)
		w.vec2(k.Pan)
		w.u8(k.Orientation)
	}
	w.count(len(p.Lens))
	for i := range p.Lens {
		k := &p.Lens[i]
		w.i64(k.Time)
		w.fstring(k.TrueLens)
		w.fstring(k.ExportLens)
		w.vec4(k.TrueCal)
		w.vec4(k.ExportCal)
		w.f32(k.TrueFOV)
		w.f32(k.ExportFOV)
	}
	w.count(len(p.Stitch))
	for i := range p.Stitch {
		k := &p.Stitch[i]
		w.i64(k.Time)
		w.vec4(k.Shift)
		w.vec4(k.Scale)
		w.vec4(k.Shear)
		w.vec4(k.Flare)
		w.vec4(k.Slant)
	}
	w.count(len(p.Alignment))
	for _, k := range p.Alignment {
		w.i64(k.Time)
		w.vec3(k.Position)
		w.rot(k.Rotation)
	}
	w.count(len(p.Orientation))
	for _, k := range p.Orientation {
		w.i64(k.Time)
		w.rot(k.Rotation)
	}
	w.count(len(p.Origin))
	for _, k := range p.Origin {
		w.i64(k.Time)
		w.vec3(k.Origin)
	}
	w.count(len(p.Motion))
	for _, k := range p.Motion {
		w.i64(k.Time)
		w.f32(k.Distance)
	}
	w.count(len(p.AutoFocus))
	for _, k := range p.AutoFocus {
		w.i64(k.Time)
		w.rot(k.Rotation)
		w.f32(k.Focal)
		w.f32(k.Min)
		w.f32(k.Max)
		w.f32(k.Speed)
	}
	w.count(len(p.Sync))
	for _, k := range p.Sync {
		w.i64(k.Time)
		w.i64(k.StartingOffset)
		w.i64(k.TicksPerHourAdj)
	}
	w.count(len(p.Transition))
	for _, k := range p.Transition {
		w.i64(k.Time)
		w.u8(k.Type)
		w.i64(k.Duration)
	}
	w.count(len(p.Image))
	for i := range p.Image {
		k := &p.Image[i]
		w.i64(k.Time)
		w.f32(k.Sharpness)
		w.f32(k.Exposure)
		w.f32(k.Temperature)
		w.f32(k.Tint)
		w.vec4(k.Saturation)
		w.vec4(k.Contrast)
		w.vec4(k.Gamma)
		w.vec4(k.Gain)
		w.vec4(k.Offset)
		w.f32(k.Shadows)
		w.f32(k.Midtones)
		w.f32(k.Highlights)
	}
	w.count(len(p.Audio))
	for _, k := range p.Audio {
		w.i64(k.Time)
		w.f32(k.Offset)
		w.f32(k.Volume)
	}
	w.count(len(p.Environment))
	for i := range p.Environment {
		k := &p.Environment[i]
		w.i64(k.Time)
		w.u8(k.Background)
		w.color(k.BackgroundColor)
		w.fstring(k.BackgroundName)
		w.u8(k.Mask)
		w.f32(k.Opacity)
		for _, c := range k.ChromaKeys {
			w.color(c)
		}
		for _, c := range k.Despill {
			w.color(c)
		}
		w.color(k.Light)
		w.color(k.AlphaCoords)
	}
}
