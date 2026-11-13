//go:build js && wasm

package hubui

import (
	"errors"
	"github.com/monkeyWie/reign/clients/nsgo/browserclient"
	"net/url"
	"strings"
	"syscall/js"
)

func readDroppedFile(uri string) (string, []byte, error) {
	if !strings.HasPrefix(uri, "blob:") {
		return "", nil, errors.New("仅接受拖入的本地文件")
	}
	parts := strings.SplitN(uri, "#", 2)
	name := "附件"
	if len(parts) == 2 {
		name, _ = url.PathUnescape(parts[1])
	}
	defer js.Global().Get("URL").Call("revokeObjectURL", parts[0])
	resp, e := browserclient.Await(js.Global().Call("fetch", parts[0]))
	if e != nil {
		return "", nil, e
	}
	if !resp.Get("ok").Bool() {
		return "", nil, errors.New("无法读取拖入的文件")
	}
	blob, e := browserclient.Await(resp.Call("blob"))
	if e != nil {
		return "", nil, e
	}
	if blob.Get("size").Int() > 50<<20 {
		return "", nil, errors.New("单个文件不能超过 50 MB")
	}
	buf, e := browserclient.Await(blob.Call("arrayBuffer"))
	if e != nil {
		return "", nil, e
	}
	raw := js.Global().Get("Uint8Array").New(buf)
	b := make([]byte, raw.Get("byteLength").Int())
	js.CopyBytesToGo(b, raw)
	return name, b, nil
}
