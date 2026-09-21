package compress

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noisethanks/atak/internal/scan"
)

// TestPlanResizeAgainstCorpus runs the planner over a real texture tree. It is
// skipped unless ATAK_CORPUS names a directory, because the useful corpora are
// multi-gigabyte mod installs that cannot live in the repo.
//
//	ATAK_CORPUS=/path/to/mods go test ./internal/compress/ -run Corpus -v
//
// The assertion is the one the feature has to hold on real data: for every DDS
// the scanner can parse, the planned output dimensions are a multiple of 4, so
// the engine's loader never has to round and re-encode.
func TestPlanResizeAgainstCorpus(t *testing.T) {
	root := os.Getenv("ATAK_CORPUS")
	if root == "" {
		t.Skip("set ATAK_CORPUS to a directory of DDS files to run this")
	}

	var scanned, misaligned, unparsed int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".dds") {
			return nil
		}
		info, perr := scan.ParseDDS(path)
		if perr != nil || info.Width <= 0 || info.Height <= 0 {
			unparsed++
			return nil
		}
		scanned++
		if info.Width%blockDim != 0 || info.Height%blockDim != 0 {
			misaligned++
		}

		asset := scan.Asset{Path: path, Width: info.Width, Height: info.Height}
		p := planResize(asset, "BC7_UNORM", 0)
		outW, outH := info.Width, info.Height
		if p.needed() {
			outW, outH = p.Width, p.Height
		}
		if outW%blockDim != 0 || outH%blockDim != 0 {
			t.Errorf("%s: %dx%d planned as %dx%d, still not a multiple of %d",
				path, info.Width, info.Height, outW, outH, blockDim)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d DDS parsed, %d misaligned and now planned to an aligned size, %d unreadable",
		root, scanned, misaligned, unparsed)
}
