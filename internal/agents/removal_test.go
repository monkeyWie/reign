package agents

import (
	"context"
	"testing"

	"github.com/monkeyWie/reignhub/internal/provider"
)

func TestRemoveSessionPersistsWithoutTouchingOtherConversations(t *testing.T) {
	for _, protocol := range []string{"acp", "pi"} {
		t.Run(protocol, func(t *testing.T) {
			dir := t.TempDir()
			p := fixtureProvider(t, protocol, dir, true)
			ctx := context.Background()
			removed, err := p.Create(ctx, provider.CreateOptions{CWD: dir})
			if err != nil {
				t.Fatal(err)
			}
			kept, err := p.Create(ctx, provider.CreateOptions{CWD: dir})
			if err != nil {
				t.Fatal(err)
			}
			p.mu.Lock()
			p.sessions[removed.ID].sending = true
			p.mu.Unlock()
			if err = p.RemoveSession(ctx, removed.ID); err == nil {
				t.Fatal("removed running session")
			}
			p.mu.Lock()
			p.sessions[removed.ID].sending = false
			p.mu.Unlock()
			if err = p.RemoveSession(ctx, removed.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = p.Transcript(ctx, removed.ID, ""); err == nil {
				t.Fatal("removed session still addressable")
			}
			if err = p.Send(ctx, removed.ID, "must not execute"); err == nil {
				t.Fatal("removed session accepted execution")
			}
			if err = p.Close(); err != nil {
				t.Fatal(err)
			}
			next := fixtureProvider(t, protocol, dir, true)
			page, err := next.Sessions(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, ss := range page.Sessions {
				if ss.ID == removed.ID {
					t.Fatal("restart resurrected removed session")
				}
				found = found || ss.ID == kept.ID
			}
			if !found {
				t.Fatal("removal affected another session")
			}
			if protocol == "acp" {
				for _, ss := range page.Sessions {
					if ss.ID == kept.ID {
						continue
					}
					if err = next.RemoveSession(ctx, ss.ID); err != nil {
						t.Fatal(err)
					}
					if err = next.discover(ctx); err != nil {
						t.Fatal(err)
					}
					listed, err := next.Sessions(ctx, "")
					if err != nil {
						t.Fatal(err)
					}
					for _, item := range listed.Sessions {
						if item.ID == ss.ID {
							t.Fatal("native discovery reimported removed conversation")
						}
					}
				}
			}
		})
	}
}
