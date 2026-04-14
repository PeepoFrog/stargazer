package outputlayout

import (
	"path/filepath"
	"strings"
)

type Layout struct {
	TelescopeDir string
	ObjectDir    string
	BaseName     string
	FitsDir      string
	RenderDir    string
	ImagePath    string
	MetadataPath string
}

func TelescopeOutputDirName(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "jwst":
		return "JAMES_WEBB_SPACE_TELESCOPE"
	case "hst":
		return "HUBBLE_SPACE_TELESCOPE"
	default:
		return strings.ToUpper(strings.TrimSpace(source))
	}
}

func ObjectDirName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(s)) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "UNKNOWN_OBJECT"
	}
	return b.String()
}

func SafeFileName(s string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		" ", "_",
	)
	return replacer.Replace(strings.TrimSpace(s))
}

func Build(outDir, sourceName, targetName, presetName string) Layout {
	telescopeDir := TelescopeOutputDirName(sourceName)
	objectDir := ObjectDirName(targetName)
	baseName := objectDir
	renderDir := filepath.Join(outDir, telescopeDir, objectDir)

	return Layout{
		TelescopeDir: telescopeDir,
		ObjectDir:    objectDir,
		BaseName:     baseName,
		FitsDir:      filepath.Join(renderDir, "fits"),
		RenderDir:    renderDir,
		ImagePath:    filepath.Join(renderDir, baseName+"_"+SafeFileName(presetName)+".png"),
		MetadataPath: filepath.Join(renderDir, baseName+"_"+SafeFileName(presetName)+"_metadata.json"),
	}
}
