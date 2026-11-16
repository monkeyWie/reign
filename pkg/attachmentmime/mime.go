// Package attachmentmime preserves the format of files whose containers share
// a magic signature, such as APKs and Office documents stored as ZIP archives.
package attachmentmime

import (
	"mime"
	"path"
	"strings"
)

func ForFile(name, detected string) string {
	kind, _, _ := mime.ParseMediaType(detected)
	if kind != "" && kind != "application/zip" && kind != "application/x-zip-compressed" && kind != "application/octet-stream" {
		return detected
	}
	ext := strings.ToLower(path.Ext(name))
	if ext == ".apk" {
		return "application/vnd.android.package-archive"
	}
	switch ext {
	case ".docx", ".xlsx", ".pptx", ".odt", ".ods", ".odp", ".epub", ".jar", ".aab":
		if known := mime.TypeByExtension(ext); known != "" {
			return known
		}
	}
	if detected == "" {
		if known := mime.TypeByExtension(ext); known != "" {
			return known
		}
		return "application/octet-stream"
	}
	return detected
}
