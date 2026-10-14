package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func TestStateRecords(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "state.db"))

	if err := s.PutStateFile(ctx, domain.StateFile{ROMID: 7, Path: "/s/G.state", Hash: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutStateFile(ctx, domain.StateFile{ROMID: 7, Path: "/s/G.state", Hash: "b"}); err != nil {
		t.Fatal(err)
	}
	if files, err := s.StateFiles(ctx, 7); err != nil || len(files) != 1 || files[0].Hash != "b" {
		t.Fatalf("files %+v, %v", files, err)
	}

	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	for i, id := range []int{30, 10, 20} {
		if err := s.AddUploadedState(ctx, domain.UploadedState{StateID: id, ROMID: 7, Path: "/s/G.state", At: at.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	ups, err := s.UploadedStates(ctx, 7)
	if err != nil || len(ups) != 3 || ups[0].StateID != 30 || ups[2].StateID != 20 || !ups[0].At.Equal(at) {
		t.Fatalf("uploads %+v, %v", ups, err)
	}
	if err := s.ForgetUploadedStates(ctx, []int{30}); err != nil {
		t.Fatal(err)
	}
	if ups, _ := s.UploadedStates(ctx, 7); len(ups) != 2 {
		t.Fatalf("after forgetting: %+v", ups)
	}

	if g, err := s.StateGame(ctx, 7); err != nil || g.Placed != 0 || g.ROMID != 7 {
		t.Fatalf("unseen game %+v, %v", g, err)
	}
	if err := s.PutStateGame(ctx, domain.StateGame{ROMID: 7, Placed: 84, Waiting: true, Problem: "p", CheckedAt: at}); err != nil {
		t.Fatal(err)
	}
	if g, err := s.StateGame(ctx, 7); err != nil || g.Placed != 84 || !g.Waiting || g.Problem != "p" || !g.CheckedAt.Equal(at) {
		t.Fatalf("game %+v, %v", g, err)
	}
	if games, err := s.StateGames(ctx); err != nil || len(games) != 1 {
		t.Fatalf("games %+v, %v", games, err)
	}
}

func TestStatePrefs(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "state.db"))
	if p, err := s.StatePrefs(ctx, 7); err != nil || !p.Empty() {
		t.Fatalf("unset: %+v, %v", p, err)
	}
	on, target, keep := false, "3", 0
	if err := s.SetStatePrefs(ctx, 7, domain.StatePrefs{Sync: &on, Target: &target, Keep: &keep}); err != nil {
		t.Fatal(err)
	}
	p, err := s.StatePrefs(ctx, 7)
	if err != nil || p.Sync == nil || *p.Sync || p.Target == nil || *p.Target != "3" || p.Extra != nil || p.Keep == nil || *p.Keep != 0 {
		t.Fatalf("set: %+v, %v", p, err)
	}
	if err := s.SetStatePrefs(ctx, 7, domain.StatePrefs{}); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.StatePrefs(ctx, 7); !p.Empty() {
		t.Fatalf("cleared: %+v", p)
	}
}

func TestForgetServerDropsStateRecords(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "state.db"))
	keep := 5
	_ = s.PutStateFile(ctx, domain.StateFile{ROMID: 1, Path: "p", Hash: "h"})
	_ = s.AddUploadedState(ctx, domain.UploadedState{StateID: 1, ROMID: 1, Path: "p"})
	_ = s.PutStateGame(ctx, domain.StateGame{ROMID: 1, Placed: 2})
	_ = s.SetStatePrefs(ctx, 1, domain.StatePrefs{Keep: &keep})
	if err := s.ForgetServer(ctx); err != nil {
		t.Fatal(err)
	}
	files, _ := s.StateFiles(ctx, 1)
	ups, _ := s.UploadedStates(ctx, 1)
	g, _ := s.StateGame(ctx, 1)
	p, _ := s.StatePrefs(ctx, 1)
	if len(files) != 0 || len(ups) != 0 || g.Placed != 0 || !p.Empty() {
		t.Fatalf("left: %v %v %+v %+v", files, ups, g, p)
	}
}
