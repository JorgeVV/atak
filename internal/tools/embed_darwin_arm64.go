//go:build darwin && arm64

package tools

import _ "embed"

// Each macOS tool is embedded as a thin ARM64 Mach-O, split from the universal
// build with `lipo -thin arm64`. Embedding per GOARCH keeps the dead slice out
// of the release binary; the release already ships one archive per architecture.

//go:embed bin/texconv-macos-arm64
var texconvBin []byte

//go:embed bin/7zz-macos-arm64
var sevenZipBin []byte

//go:embed bin/compressonator-bc7e-macos-arm64
var compressonatorBin []byte

const texconvName = "texconv"
const sevenZipName = "7zz"
const compressonatorName = "compressonatorcli"
