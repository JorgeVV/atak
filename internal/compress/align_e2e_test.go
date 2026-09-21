package compress

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/noisethanks/atak/internal/scan"
)

// synthUncompressedDDS builds a legacy-header 32-bit BGRA DDS at an arbitrary
// size. It matches the shape of the GAMMA UI sources this feature exists for:
// uncompressed art authored at whatever size the artist wanted, with no block
// constraint of its own, which only becomes a problem once it is compressed.
// Both backends read this variant, so a test can tell routing from capability.
func synthUncompressedDDS(width, height int) []byte {
	const (
		ddsdCaps        = 0x1
		ddsdHeight      = 0x2
		ddsdWidth       = 0x4
		ddsdPitch       = 0x8
		ddsdPixelFormat = 0x1000

		ddpfAlphaPixels = 0x1
		ddpfRGB         = 0x40

		ddscapsTexture = 0x1000
	)

	var b bytes.Buffer
	b.Write([]byte("DDS "))
	w := func(v uint32) { _ = binary.Write(&b, binary.LittleEndian, v) }

	w(124) // dwSize
	w(ddsdCaps | ddsdHeight | ddsdWidth | ddsdPitch | ddsdPixelFormat)
	w(uint32(height))
	w(uint32(width))
	w(uint32(width * 4)) // dwPitchOrLinearSize
	w(0)                 // dwDepth
	w(1)                 // dwMipMapCount
	for i := 0; i < 11; i++ {
		w(0) // dwReserved1
	}
	// DDS_PIXELFORMAT (32 bytes)
	w(32) // dwSize
	w(ddpfAlphaPixels | ddpfRGB)
	w(0)  // dwFourCC
	w(32) // dwRGBBitCount
	w(0x00ff0000)
	w(0x0000ff00)
	w(0x000000ff)
	w(0xff000000)
	w(ddscapsTexture)
	w(0) // dwCaps2
	w(0) // dwCaps3
	w(0) // dwCaps4
	w(0) // dwReserved2

	// A flat mid-gray field. The pixels never need to be interesting; the test
	// asserts on the output header, not on image quality.
	px := make([]byte, width*height*4)
	for i := range px {
		px[i] = 0x80
	}
	b.Write(px)
	return b.Bytes()
}

// ddsPayloadLen returns the byte count after the DDS header, picking the header
// size from the file's own FourCC so it works for both legacy and DX10 output.
func ddsPayloadLen(t *testing.T, path string) int64 {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 128 {
		t.Fatalf("%s: %d bytes, too short for a DDS header", path, len(data))
	}
	header := int64(128)
	if bytes.Equal(data[84:88], []byte("DX10")) {
		header = 148
	}
	return int64(len(data)) - header
}

// TestAlignEndToEndMisalignedSource is the regression test for the GAMMA freeze,
// run against the real binaries. A 269x271 uncompressed source is exactly the
// case that cost 10.8 s per load once it became BC7. compressonator-bc7e cannot
// resize, so texconv has to be involved, and the output must come back at
// 272x272 with a payload of whole 4x4 blocks. The encoding still belongs to
// compressonator-bc7e: dispatch splits the job rather than handing the whole
// thing to texconv, which is 300x slower at BC7.
func TestAlignEndToEndMisalignedSource(t *testing.T) {
	compressBin := repoToolPath(t, "compressonator-bc7e")
	texconvBin := repoToolPath(t, "texconv")

	const srcW, srcH = 269, 271
	const wantW, wantH = 272, 272

	src := writeFixture(t, "ui_hud_hit_mark.dds", synthUncompressedDDS(srcW, srcH))
	outDir := t.TempDir()
	job := Job{
		Asset:        scan.Asset{Path: src, Width: srcW, Height: srcH, SourceMipCount: 1},
		Format:       "BC7_UNORM",
		GenerateMips: false,
		OutputDir:    outDir,
	}

	r := dispatch(context.Background(), NewCompressonatorBackend(compressBin), NewTexconvBackend(texconvBin), job)
	if !r.Success {
		t.Fatalf("compression failed: err=%v stderr=%s", r.Err, r.Stderr)
	}
	if r.Backend != "compressonator-bc7e" {
		t.Errorf("Backend: got %q, want %q (texconv resizes, the configured encoder still encodes)",
			r.Backend, "compressonator-bc7e")
	}
	if r.FallbackReason != FallbackBlockAlign {
		t.Errorf("FallbackReason: got %q, want %q", r.FallbackReason, FallbackBlockAlign)
	}

	outPath := filepath.Join(outDir, "ui_hud_hit_mark.dds")
	info, err := scan.ParseDDS(outPath)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if info.Width != wantW || info.Height != wantH {
		t.Errorf("output dimensions: got %dx%d, want %dx%d", info.Width, info.Height, wantW, wantH)
	}
	if info.Width%blockDim != 0 || info.Height%blockDim != 0 {
		t.Errorf("output %dx%d is still not a multiple of %d, so the loader would re-encode it",
			info.Width, info.Height, blockDim)
	}
	// BC7 is 16 bytes per 4x4 block. An exact match proves the encoder wrote the
	// aligned size rather than padding a misaligned one.
	wantPayload := int64((wantW / blockDim) * (wantH / blockDim) * 16)
	if got := ddsPayloadLen(t, outPath); got != wantPayload {
		t.Errorf("payload: got %d bytes, want %d (%d blocks x 16)", got, wantPayload, wantPayload/16)
	}
}

// TestAlignEndToEndAlignedSourceStaysOnPrimary is the guard against the opposite
// regression. A source that is already a multiple of 4 must keep the configured
// backend and gain no fallback label, or every file in a run would be rerouted to
// the slower path for nothing.
func TestAlignEndToEndAlignedSourceStaysOnPrimary(t *testing.T) {
	compressBin := repoToolPath(t, "compressonator-bc7e")
	texconvBin := repoToolPath(t, "texconv")

	const size = 64
	src := writeFixture(t, "ui_aligned.dds", synthUncompressedDDS(size, size))
	outDir := t.TempDir()
	job := Job{
		Asset:        scan.Asset{Path: src, Width: size, Height: size, SourceMipCount: 1},
		Format:       "BC7_UNORM",
		GenerateMips: false,
		OutputDir:    outDir,
	}

	r := dispatch(context.Background(), NewCompressonatorBackend(compressBin), NewTexconvBackend(texconvBin), job)
	if !r.Success {
		t.Fatalf("compression failed: err=%v stderr=%s", r.Err, r.Stderr)
	}
	if r.Backend != "compressonator-bc7e" {
		t.Errorf("Backend: got %q, want %q (an aligned source must not be rerouted)", r.Backend, "compressonator-bc7e")
	}
	if r.FallbackReason != "" {
		t.Errorf("FallbackReason: got %q, want empty", r.FallbackReason)
	}

	info, err := scan.ParseDDS(filepath.Join(outDir, "ui_aligned.dds"))
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if info.Width != size || info.Height != size {
		t.Errorf("output dimensions: got %dx%d, want %dx%d unchanged", info.Width, info.Height, size, size)
	}
}

// TestAlignEndToEndTexconvPrimary covers the other configuration. With texconv as
// the primary backend there is no fallback and dispatch never routes anything, so
// the alignment has to come from Run itself and still be reported.
func TestAlignEndToEndTexconvPrimary(t *testing.T) {
	texconvBin := repoToolPath(t, "texconv")

	const srcW, srcH = 43, 43
	src := writeFixture(t, "bg1.dds", synthUncompressedDDS(srcW, srcH))
	outDir := t.TempDir()
	job := Job{
		Asset:        scan.Asset{Path: src, Width: srcW, Height: srcH, SourceMipCount: 1},
		Format:       "BC7_UNORM",
		GenerateMips: false,
		OutputDir:    outDir,
	}

	r := dispatch(context.Background(), NewTexconvBackend(texconvBin), nil, job)
	if !r.Success {
		t.Fatalf("compression failed: err=%v stderr=%s", r.Err, r.Stderr)
	}
	if r.FallbackReason != FallbackBlockAlign {
		t.Errorf("FallbackReason: got %q, want %q", r.FallbackReason, FallbackBlockAlign)
	}

	info, err := scan.ParseDDS(filepath.Join(outDir, "bg1.dds"))
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if info.Width != 44 || info.Height != 44 {
		t.Errorf("output dimensions: got %dx%d, want 44x44", info.Width, info.Height)
	}
}
