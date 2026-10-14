package rpc

import (
	"context"
	"encoding/base64"
	"net/http"
	"sync"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/app"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// screenshotFetches is how many state screenshots a list fetches from RomM at once.
const screenshotFetches = 4

func stateRoutes(s *Server, saves *app.Saves) {
	status := func(ctx context.Context, id int) (schema.StateStatus, error) {
		st, err := saves.StateStatus(ctx, id)
		if err != nil {
			return schema.StateStatus{}, wireError(err)
		}
		return schema.StateStatus{
			ID: st.ROMID, Syncs: st.Syncs, Why: st.Why, Emulator: st.Emulator, Dir: st.Dir,
			Settings: toWireStateSettings(st.Settings), Own: toWireStatePrefs(st.Own), Global: toWireStateSettings(saves.GlobalStateSettings()),
			Waiting: st.Waiting, Problem: st.Problem,
		}, nil
	}

	Register(s, "state_status", func(ctx context.Context, p schema.StateStatusParams) (schema.StateStatus, error) {
		return status(ctx, p.ID)
	})

	Register(s, "save_states", func(ctx context.Context, p schema.SaveStatesParams) (schema.SaveStates, error) {
		list, err := saves.States(ctx, p.ID)
		if err != nil {
			return schema.SaveStates{}, wireError(err)
		}
		out := schema.SaveStates{States: make([]schema.SaveStateEntry, len(list))}
		var wg sync.WaitGroup
		sem := make(chan struct{}, screenshotFetches)
		for i, e := range list {
			out.States[i] = schema.SaveStateEntry{
				ID: e.ID, At: rfc3339(e.At), FromDeck: e.FromDeck, Slot: e.Slot, Placed: e.Placed, SizeBytes: e.SizeBytes,
			}
			if e.Screenshot == "" {
				continue
			}
			wg.Go(func() {
				sem <- struct{}{}
				defer func() { <-sem }()
				// A screenshot that can't be fetched leaves the state without one.
				if data, err := saves.StateScreenshot(ctx, e.Screenshot); err == nil && len(data) > 0 {
					out.States[i].Screenshot = "data:" + http.DetectContentType(data) + ";base64," + base64.StdEncoding.EncodeToString(data)
				}
			})
		}
		wg.Wait()
		return out, nil
	})

	Register(s, "load_save_state", func(ctx context.Context, p schema.LoadSaveStateParams) (schema.StateStatus, error) {
		if err := saves.LoadState(ctx, p.ID, p.StateID); err != nil {
			return schema.StateStatus{}, wireError(err)
		}
		return status(ctx, p.ID)
	})

	Register(s, "set_state_prefs", func(ctx context.Context, p schema.SetStatePrefsParams) (schema.StateStatus, error) {
		if err := saves.SetStatePrefs(ctx, p.ID, fromWireStatePrefs(p.Own)); err != nil {
			return schema.StateStatus{}, wireError(err)
		}
		return status(ctx, p.ID)
	})
}

func toWireStateSettings(s domain.StateSettings) schema.StateSettings {
	return schema.StateSettings{Sync: s.Sync, Target: s.Target, Extra: s.Extra, Keep: s.Keep}
}

func toWireStatePrefs(p domain.StatePrefs) schema.StatePrefs {
	return schema.StatePrefs{Sync: p.Sync, Target: p.Target, Extra: p.Extra, Keep: p.Keep}
}

func fromWireStatePrefs(p schema.StatePrefs) domain.StatePrefs {
	return domain.StatePrefs{Sync: p.Sync, Target: p.Target, Extra: p.Extra, Keep: p.Keep}
}
