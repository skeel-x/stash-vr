package hsp

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func readSample(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/sample.hsp")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// rawPayload inflates the single chunk after the 48-byte header.
func rawPayload(t *testing.T, file []byte) []byte {
	t.Helper()
	zr, err := zlib.NewReader(bytes.NewReader(file[headerSize:]))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDecode_Sample(t *testing.T) {
	p, err := Decode(readSample(t))
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != 7 || p.ID != "https://vr.example/heresphere/1" || p.Title != "Sample" || p.Description != "" {
		t.Fatalf("header strings: %+v", p)
	}
	if len(p.Tags) != 2 || p.Tags[0].Name != "#:Tag" || p.Tags[1].Name != "@:Performer" {
		t.Fatalf("tags: %+v", p.Tags)
	}
	if len(p.Format) != 1 || p.Format[0].Projection != ProjectionFisheye || p.Format[0].Stereo != StereoSideBySide {
		t.Fatalf("format: %+v", p.Format)
	}
	if p.Format[0].Zoom != (Vec2{1, 1}) {
		t.Fatalf("zoom: %+v", p.Format[0].Zoom)
	}
	if len(p.Lens) != 1 || p.Lens[0].TrueLens != "Linear" || p.Lens[0].TrueFOV != 180 {
		t.Fatalf("lens: %+v", p.Lens)
	}
	if len(p.Alignment) != 1 || p.Alignment[0].Position.Y < 4.78 || p.Alignment[0].Position.Y > 4.79 {
		t.Fatalf("alignment: %+v", p.Alignment)
	}
	if len(p.Environment) != 1 || p.Environment[0].Background != BackgroundPassthrough || p.Environment[0].Mask != MaskAlphaPacked {
		t.Fatalf("environment: %+v", p.Environment)
	}
	if p.Duration != 2544598*10000 {
		t.Fatalf("duration ticks %d", p.Duration)
	}
	for name, n := range map[string]int{"stitch": len(p.Stitch), "orientation": len(p.Orientation), "origin": len(p.Origin),
		"motion": len(p.Motion), "autofocus": len(p.AutoFocus), "sync": len(p.Sync), "transition": len(p.Transition),
		"image": len(p.Image), "audio": len(p.Audio)} {
		if n != 1 {
			t.Errorf("%s: %d keyframes, want 1", name, n)
		}
	}
}

func TestEncode_RoundTripsRawPayload(t *testing.T) {
	file := readSample(t)
	p, err := Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	if want, got := rawPayload(t, file), rawPayload(t, out); !bytes.Equal(want, got) {
		t.Fatalf("raw payload differs: %d vs %d bytes", len(want), len(got))
	}
	again, err := Decode(out)
	if err != nil {
		t.Fatal(err)
	}
	if again.Title != p.Title || len(again.Tags) != len(p.Tags) {
		t.Fatalf("re-decoded %+v", again)
	}
}

func TestEncode_Header(t *testing.T) {
	out, err := Encode(Default())
	if err != nil {
		t.Fatal(err)
	}
	var h [6]int64
	if err := binary.Read(bytes.NewReader(out[:headerSize]), binary.LittleEndian, &h); err != nil {
		t.Fatal(err)
	}
	raw := rawPayload(t, out)
	compressed := int64(len(out) - headerSize)
	if h[0] != 0x9E2A83C1 || h[1] != 131072 || h[2] != compressed || h[3] != int64(len(raw)) || h[4] != compressed || h[5] != int64(len(raw)) {
		t.Fatalf("header %v, compressed %d raw %d", h, compressed, len(raw))
	}
	if n := binary.LittleEndian.Uint32(raw); int(n) != len(raw)-4 {
		t.Fatalf("content length %d, raw %d", n, len(raw))
	}
	if v := binary.LittleEndian.Uint32(raw[4:]); v != 7 {
		t.Fatalf("version %d", v)
	}
}

func TestDefault_HasEverySection(t *testing.T) {
	p := Default()
	counts := []int{len(p.Format), len(p.Lens), len(p.Stitch), len(p.Alignment), len(p.Orientation), len(p.Origin),
		len(p.Motion), len(p.AutoFocus), len(p.Sync), len(p.Transition), len(p.Image), len(p.Audio), len(p.Environment)}
	for i, n := range counts {
		if n != 1 {
			t.Errorf("section %d has %d keyframes", i, n)
		}
	}
	if p.Stitch[0].Scale != (Vec4{1, 1, 1, 1}) || p.Audio[0].Volume != 1 || p.Environment[0].Background != BackgroundGlobal {
		t.Fatalf("default values %+v", p)
	}
	// Default must not share slices between calls.
	p.Format[0].Zoom.X = 9
	if Default().Format[0].Zoom.X != 1 {
		t.Fatal("Default shares state between calls")
	}
}

func TestFString_UTF16(t *testing.T) {
	p := Default()
	p.Title = "Tänzerin ☂"
	p.Tags = []Tag{{Name: "#:Ünïcode", Rating: 1, End: 5, Track: 3}}
	out, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	raw := rawPayload(t, out)
	// id is empty (length 0), then the title follows at offset 12.
	if n := int32(binary.LittleEndian.Uint32(raw[12:])); n != -11 {
		t.Fatalf("title length %d, want -11 (10 UTF-16 units + NUL)", n)
	}
	got, err := Decode(out)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != p.Title || got.Tags[0] != p.Tags[0] {
		t.Fatalf("got %q %+v", got.Title, got.Tags)
	}
}

func TestFString_ASCIIIsSingleByte(t *testing.T) {
	p := Default()
	p.ID = "abc"
	out, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	raw := rawPayload(t, out)
	if n := int32(binary.LittleEndian.Uint32(raw[8:])); n != 4 || string(raw[12:16]) != "abc\x00" {
		t.Fatalf("id encoded as %d %q", n, raw[12:16])
	}
}

func TestDecode_Rejects(t *testing.T) {
	good := readSample(t)

	badTag := bytes.Clone(good)
	badTag[0] ^= 0xff

	p, err := Decode(good)
	if err != nil {
		t.Fatal(err)
	}
	p.Version = 6
	badVersion, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}

	badSize := bytes.Clone(good)
	binary.LittleEndian.PutUint64(badSize[24:], 99)

	cases := map[string][]byte{
		"empty":           nil,
		"short header":    good[:20],
		"bad tag":         badTag,
		"bad version":     badVersion,
		"truncated chunk": good[:len(good)-10],
		"raw size":        badSize,
		"trailing bytes":  append(bytes.Clone(good), 0),
		"truncated body":  truncatedPayload(t, good),
		"extra payload":   extraPayload(t, good),
	}
	for name, data := range cases {
		if _, err := Decode(data); err == nil {
			t.Errorf("%s: expected an error", name)
		} else if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: error %v does not wrap ErrInvalid", name, err)
		}
	}
}

// repack compresses raw into a single-chunk file with a matching header.
func repack(t *testing.T, raw []byte) []byte {
	t.Helper()
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	for _, v := range []int64{0x9E2A83C1, 131072, int64(z.Len()), int64(len(raw)), int64(z.Len()), int64(len(raw))} {
		_ = binary.Write(&out, binary.LittleEndian, v)
	}
	out.Write(z.Bytes())
	return out.Bytes()
}

// truncatedPayload cuts the last keyframe short while keeping the content
// length consistent, so only the structure reader can notice.
func truncatedPayload(t *testing.T, file []byte) []byte {
	raw := rawPayload(t, file)
	raw = raw[:len(raw)-8]
	binary.LittleEndian.PutUint32(raw, uint32(len(raw)-4))
	return repack(t, raw)
}

// extraPayload appends bytes the structure does not account for.
func extraPayload(t *testing.T, file []byte) []byte {
	raw := append(rawPayload(t, file), 1, 2, 3, 4)
	binary.LittleEndian.PutUint32(raw, uint32(len(raw)-4))
	return repack(t, raw)
}

func TestTicks(t *testing.T) {
	if TimespanTicks(1.5) != 15_000_000 {
		t.Fatalf("timespan %d", TimespanTicks(1.5))
	}
	// 1970-01-01 is 621355968000000000 ticks after 0001-01-01.
	if got := DateTicks(time.Unix(0, 0)); got != 621355968000000000 {
		t.Fatalf("date %d", got)
	}
	if DateTicks(time.Time{}) != 0 {
		t.Fatal("zero time must be 0 ticks")
	}
}

func TestEncode_LargeProfileUsesSeveralChunks(t *testing.T) {
	p := Default()
	for i := range 6000 {
		p.Tags = append(p.Tags, Tag{Name: "#:Tag number " + time.Duration(i).String(), Track: uint32(i % 7)})
	}
	out, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	raw := int64(le.Uint64(out[24:]))
	if raw <= chunkSize {
		t.Fatalf("raw size %d does not need chunks", raw)
	}
	if first := int64(le.Uint64(out[40:])); first != chunkSize {
		t.Fatalf("first chunk raw size %d", first)
	}
	got, err := Decode(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != len(p.Tags) || got.Tags[5999] != p.Tags[5999] {
		t.Fatalf("got %d tags", len(got.Tags))
	}
}
