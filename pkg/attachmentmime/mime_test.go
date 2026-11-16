package attachmentmime

import "testing"

func TestContainerFormatsKeepTheirFileType(t *testing.T) {
	for _, tc := range []struct{ name, detected, want string }{
		{"ReignHub.APK", "application/zip", "application/vnd.android.package-archive"},
		{"ReignHub.apk", "application/octet-stream", "application/vnd.android.package-archive"},
		{"report.docx", "application/zip", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"archive.zip", "application/zip", "application/zip"},
		{"photo.apk", "image/png", "image/png"},
		{"photo.png", "application/zip", "application/zip"},
	} {
		if got := ForFile(tc.name, tc.detected); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
