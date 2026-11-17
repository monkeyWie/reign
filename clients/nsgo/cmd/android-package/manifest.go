package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/cmd/nsgo/apk"
)

// The SDK understands service types and manifest attributes beyond the small
// standalone NSGo packager. Preserve those attributes and use aapt2 to encode
// them instead of changing the framework or dropping Android's declarations.
func prepareSDKManifest(template []byte, cfg apk.Manifest) ([]byte, error) {
	if !regexp.MustCompile(`^[a-zA-Z][\w]*(?:\.[a-zA-Z][\w]*)+$`).MatchString(cfg.Package) {
		return nil, fmt.Errorf("invalid Android application ID")
	}
	text := string(template)
	set := func(tag, name, value string) {
		var escaped bytes.Buffer
		_ = xml.EscapeText(&escaped, []byte(value))
		key := name + `="` + escaped.String() + `"`
		start := regexp.MustCompile(`<` + tag + `\b[^>]*>`)
		attr := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `="[^"]*"`)
		text = start.ReplaceAllStringFunc(text, func(node string) string {
			if attr.MatchString(node) {
				return attr.ReplaceAllStringFunc(node, func(string) string { return key })
			}
			return strings.TrimSuffix(node, ">") + " " + key + ">"
		})
	}
	set("manifest", "package", cfg.Package)
	set("manifest", "android:versionName", cfg.VersionName)
	set("manifest", "android:versionCode", strconv.Itoa(cfg.VersionCode))
	set("application", "android:label", cfg.Label)
	if cfg.TargetSDK == 0 {
		cfg.TargetSDK = 34
	}
	set("uses-sdk", "android:targetSdkVersion", strconv.Itoa(cfg.TargetSDK))
	set("provider", "android:authorities", cfg.Package+".nsgo.files")
	for _, permission := range cfg.Permissions {
		if !regexp.MustCompile(`^[a-zA-Z][\w.]+$`).MatchString(permission) {
			return nil, fmt.Errorf("invalid permission %q", permission)
		}
		if !strings.Contains(text, `android:name="`+permission+`"`) {
			text = strings.Replace(text, "<application", `<uses-permission android:name="`+permission+`" /><application`, 1)
		}
	}
	// Parsing catches malformed templates before invoking an external compiler.
	decoder := xml.NewDecoder(strings.NewReader(text))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return []byte(text), nil
}
