package main

import (
	"os"
	"strings"
	"testing"

	"github.com/nsgo-dev/nsgo/cmd/nsgo/apk"
)

func TestSDKManifestRetainsServiceAndSigningApplicationIdentity(t *testing.T) {
	template, err := os.ReadFile("../../native/android/AndroidManifest.xml")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := prepareSDKManifest(template, apk.Manifest{Package: "dev.agenthub.nsgo", Label: "ReignHub & Agents", VersionName: "0.1.0-rc.21", VersionCode: 121, Permissions: []string{"android.permission.CAMERA", "android.permission.POST_NOTIFICATIONS"}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(manifest)
	for _, required := range []string{`package="dev.agenthub.nsgo"`, `android:versionCode="121"`, `android:foregroundServiceType="connectedDevice"`, `android:stopWithTask="true"`, `android:authorities="dev.agenthub.nsgo.nsgo.files"`, `android:label="ReignHub &amp; Agents"`} {
		if !strings.Contains(text, required) {
			t.Fatalf("missing SDK manifest field %s", required)
		}
	}
	if strings.Count(text, `android:name="android.permission.POST_NOTIFICATIONS"`) != 1 {
		t.Fatal("duplicated notification permission")
	}
}
