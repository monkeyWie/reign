package codex

// The live sequence owns the order of shared items. Retain history-only items
// before their next shared anchor, including history preceding a partial tail.
func mergeIPCItems(history, live []any) []any {
	shared := map[string]bool{}
	for _, item := range live {
		if id := stringValue(object(item)["id"]); id != "" {
			shared[id] = true
		}
	}
	before := map[string][]any{}
	missing := []any{}
	for _, item := range history {
		id := stringValue(object(item)["id"])
		if shared[id] {
			before[id] = missing
			missing = nil
		} else {
			missing = append(missing, item)
		}
	}
	out, pending := []any{}, []any{}
	for _, item := range live {
		id := stringValue(object(item)["id"])
		if prior, exists := before[id]; exists {
			out = append(out, prior...)
			out = append(out, pending...)
			pending = nil
			out = append(out, item)
		} else {
			pending = append(pending, item)
		}
	}
	out = append(out, missing...)
	return append(out, pending...)
}

// App keeps a local steering bubble after acknowledgement and represents the
// matching server item as "steered". Canonical history can still contain the
// original userMessage. Match explicit server/client IDs, never message text:
// repeated instructions are distinct messages. Preserve the local bubble's ID
// and position so acknowledgement does not remount it or its attachments.
func ipcMessageItems(items []any) []any {
	servers, clients := map[string]bool{}, map[string]bool{}
	for _, value := range items {
		i := object(value)
		if i["type"] != "steeringUserMessage" {
			continue
		}
		if id := stringValue(i["serverUserMessageId"]); id != "" {
			servers[id] = true
		}
		for _, field := range []string{"clientUserMessageId", "serverClientUserMessageId"} {
			if id := stringValue(i[field]); id != "" {
				clients[id] = true
			}
		}
	}
	out := make([]any, 0, len(items))
	for _, value := range items {
		i := object(value)
		if i["type"] == "userMessage" && (servers[stringValue(i["id"])] || clients[stringValue(i["clientId"])]) {
			continue
		}
		out = append(out, value)
	}
	return out
}
