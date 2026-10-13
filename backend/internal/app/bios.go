package app

import (
	"context"
	"crypto/md5"  //nolint:gosec // matches files against RomM's MD5, not for security
	"crypto/sha1" //nolint:gosec // see downloads.go
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// biosStaging is where BIOS files land in staging. Like a game's staging folder, whatever a stop
// leaves in it is cleaned up the next time the backend starts.
const biosStaging = "bios"

// FirmwareServer is the part of the RomM API BIOS files need.
type FirmwareServer interface {
	Firmware(ctx context.Context, platformID int) ([]domain.Firmware, error)
	DownloadFirmware(ctx context.Context, id int, name string, open func(start int64) (io.Writer, error)) error
}

// FirmwareDialer returns a FirmwareServer for server, authenticated with token.
type FirmwareDialer func(server domain.Server, token string) FirmwareServer

// BIOSStore keeps the records of Plugin-owned BIOS files.
type BIOSStore interface {
	SaveOwnedBIOS(ctx context.Context, b domain.OwnedBIOS) error
	OwnedBIOS(ctx context.Context, platformID int) ([]domain.OwnedBIOS, error)
	DeleteOwnedBIOS(ctx context.Context, firmwareID int) error
}

// BIOSPrefs keeps whether queueing a game also downloads its platform's missing BIOS files.
type BIOSPrefs interface {
	AutoBIOS() bool
	SetAutoBIOS(on bool) error
}

// BIOSFile is one of a platform's firmware files on the Server, and how it stands on this Deck.
type BIOSFile struct {
	domain.Firmware
	// Path is where the file goes; empty when the platform's firmware is InEmulator.
	Path   string
	Status domain.BIOSStatus
	// Owned is set when the file at Path is one the plugin placed and is unchanged since.
	Owned bool
}

// PlatformBIOS is the firmware files the Server has for a platform.
type PlatformBIOS struct {
	PlatformID   int
	PlatformName string
	// InEmulator is set when EmuDeck keeps the platform's firmware inside its emulator, where the
	// plugin doesn't place it. Its Files then have no Path or Status.
	InEmulator bool
	Files      []BIOSFile
	// Downloading is set while the plugin downloads the platform's missing files.
	Downloading bool
}

// Missing returns the files that aren't in the BIOS folder.
func (p PlatformBIOS) Missing() []BIOSFile {
	var out []BIOSFile
	for _, f := range p.Files {
		if f.Status == domain.BIOSMissing {
			out = append(out, f)
		}
	}
	return out
}

// BIOSUpdate is a platform's BIOS download starting, with Done unset, or ending.
type BIOSUpdate struct {
	PlatformID   int
	PlatformName string
	// Auto is set when queueing a game started the download.
	Auto bool
	Done bool
	// Files is how many files the download fetches, and Downloaded how many of them it placed.
	Files      int
	Downloaded int
	// Problem is why a file didn't download, set when Done.
	Problem error
}

// BIOSListener hears each BIOS download start and end.
type BIOSListener func(BIOSUpdate)

type BIOS struct {
	catalog CatalogStore
	install *Install
	conn    *Connection
	dial    FirmwareDialer
	// store is nil when state.db couldn't be opened, and problem says why.
	store    BIOSStore
	problem  error
	prefs    BIOSPrefs
	onChange BIOSListener
	log      *slog.Logger
	now      func() time.Time
	// base outlives single calls, like Downloads' does.
	base context.Context

	mu sync.Mutex
	// busy are the platforms whose BIOS files are downloading.
	busy map[int]bool
	// checked are the platforms whose BIOS files a queued game has looked for since the backend
	// started, so a run of downloads asks the Server once.
	checked map[int]bool
	// sums are the hashes of files in the BIOS folder, kept while their size and time are the same.
	sums map[string]cachedSums
}

type cachedSums struct {
	size int64
	mod  time.Time
	sums domain.FileSums
}

// NewBIOS returns the BIOS service. A nil store turns BIOS downloads off, reporting problem.
func NewBIOS(ctx context.Context, catalog CatalogStore, install *Install, conn *Connection, dial FirmwareDialer,
	store BIOSStore, problem error, prefs BIOSPrefs, listen BIOSListener, log *slog.Logger,
) *BIOS {
	return &BIOS{
		catalog: catalog, install: install, conn: conn, dial: dial, store: store, problem: problem, prefs: prefs,
		onChange: listen, log: log, now: time.Now, base: ctx,
		busy: map[int]bool{}, checked: map[int]bool{}, sums: map[string]cachedSums{},
	}
}

// AutoDownload says whether queueing a game also downloads its platform's missing BIOS files.
func (b *BIOS) AutoDownload() bool { return b.prefs.AutoBIOS() }

// SetAutoDownload turns downloading BIOS files with games on or off.
func (b *BIOS) SetAutoDownload(on bool) error { return b.prefs.SetAutoBIOS(on) }

// Status returns the firmware files the Server has for a platform, and how each stands in the BIOS
// folder. A record of a file the plugin placed that has since gone or changed is dropped: the file
// is no longer the plugin's.
func (b *BIOS) Status(ctx context.Context, platformID int) (PlatformBIOS, error) {
	platform, ok, err := b.catalog.Platform(ctx, platformID)
	if err != nil {
		return PlatformBIOS{}, err
	}
	if !ok {
		return PlatformBIOS{}, &domain.Error{Kind: domain.KindNotFound, Message: "That platform isn't in the library. Refresh the library."}
	}
	in, err := b.conn.activeSignIn()
	if err != nil {
		return PlatformBIOS{}, err
	}
	if in == nil {
		return PlatformBIOS{}, errBIOSNotSignedIn
	}
	firmware, err := b.dial(in.Server, in.Token).Firmware(ctx, platformID)
	if err != nil {
		return PlatformBIOS{}, b.conn.observe(*in, err)
	}

	out := PlatformBIOS{PlatformID: platformID, PlatformName: platform.Name, Downloading: b.isBusy(platformID)}
	folder, placed := domain.BIOSFolder(platform.Slug)
	if !placed {
		out.InEmulator = true
		for _, f := range firmware {
			out.Files = append(out.Files, BIOSFile{Firmware: f})
		}
		return out, nil
	}
	dir, err := b.dir(folder)
	if err != nil {
		return PlatformBIOS{}, err
	}
	owned, err := b.owned(ctx, platformID)
	if err != nil {
		return PlatformBIOS{}, err
	}
	for _, f := range firmware {
		name, ok := f.SafeName()
		if !ok {
			b.log.Warn("skipping firmware with an unsafe name", "firmware_id", f.ID, "name", f.FileName)
			continue
		}
		file := BIOSFile{Firmware: f, Path: filepath.Join(dir, name)}
		var info os.FileInfo
		file.Status, info, err = b.statusAt(f, file.Path)
		if err != nil {
			return PlatformBIOS{}, err
		}
		if rec, ok := owned[f.ID]; ok {
			file.Owned = info != nil && rec.File.Path == file.Path && rec.File.SizeBytes == info.Size() && rec.File.ModTime.Equal(info.ModTime().UTC())
			if !file.Owned {
				b.log.Info("BIOS file no longer the plugin's", "firmware_id", f.ID, "path", rec.File.Path)
				if err := b.store.DeleteOwnedBIOS(ctx, f.ID); err != nil {
					b.log.Warn("drop BIOS record", "firmware_id", f.ID, "err", err)
				}
			}
		}
		out.Files = append(out.Files, file)
	}
	return out, nil
}

// Download downloads a platform's missing BIOS files in the background and returns how many it
// fetches. Files already in the BIOS folder stay as they are, whatever they hold.
func (b *BIOS) Download(ctx context.Context, platformID int) (int, error) {
	if b.store == nil {
		return 0, b.problem
	}
	status, err := b.Status(ctx, platformID)
	if err != nil {
		return 0, err
	}
	if status.InEmulator {
		return 0, &domain.Error{Kind: domain.KindCantDownload, Message: "EmuDeck keeps " + status.PlatformName + " firmware inside its emulator, so the plugin doesn't place it. Install it from the emulator."}
	}
	missing := status.Missing()
	if len(missing) == 0 || !b.start(platformID) {
		return len(missing), nil
	}
	go b.fetchAll(status, missing, false)
	return len(missing), nil
}

// GameQueued downloads a platform's missing BIOS files when a game of it joins the download queue,
// unless that's turned off. Only the first game of each platform since the backend started looks.
func (b *BIOS) GameQueued(platformID int) {
	if b.store == nil || !b.prefs.AutoBIOS() {
		return
	}
	b.mu.Lock()
	if b.checked[platformID] {
		b.mu.Unlock()
		return
	}
	b.checked[platformID] = true
	b.mu.Unlock()

	go func() {
		in, err := b.conn.activeSignIn()
		if err != nil || in == nil || !biosAllowed(in.Scopes) {
			return
		}
		status, err := b.Status(b.base, platformID)
		if err != nil {
			// The next game queued looks again.
			b.mu.Lock()
			delete(b.checked, platformID)
			b.mu.Unlock()
			b.log.Warn("check BIOS for queued game", "platform_id", platformID, "err", err)
			return
		}
		missing := status.Missing()
		if status.InEmulator || len(missing) == 0 || !b.start(platformID) {
			return
		}
		b.fetchAll(status, missing, true)
	}()
}

// biosAllowed says whether a token with scopes may read firmware.
func biosAllowed(scopes []string) bool {
	for _, d := range domain.DisabledFeatures(scopes) {
		if d.Feature == domain.FeatureBIOS {
			return false
		}
	}
	return true
}

// fetchAll downloads files, a platform's missing BIOS files, reporting the start and the end.
func (b *BIOS) fetchAll(status PlatformBIOS, files []BIOSFile, auto bool) {
	defer b.finish(status.PlatformID)
	update := BIOSUpdate{PlatformID: status.PlatformID, PlatformName: status.PlatformName, Auto: auto, Files: len(files)}
	b.log.Info("BIOS download started", "platform_id", status.PlatformID, "files", len(files), "auto", auto)
	b.onChange(update)

	update.Done = true
	in, err := b.conn.activeSignIn()
	if err == nil && in == nil {
		err = errBIOSNotSignedIn
	}
	if err != nil {
		update.Problem = err
		b.onChange(update)
		return
	}
	server := b.dial(in.Server, in.Token)
	for _, f := range files {
		if err := b.fetch(server, status.PlatformID, f); err != nil {
			err = b.conn.observe(*in, err)
			b.log.Warn("BIOS download failed", "firmware_id", f.ID, "name", f.FileName, "err", err)
			if update.Problem == nil {
				update.Problem = err
			}
			if b.base.Err() != nil {
				return
			}
			continue
		}
		update.Downloaded++
	}
	b.log.Info("BIOS download ended", "platform_id", status.PlatformID, "downloaded", update.Downloaded, "of", len(files))
	b.onChange(update)
}

// fetch downloads one BIOS file into staging, checks it against the Server's hash, moves it into
// place and records it as Plugin-owned. Something already at its path stays, and fails it.
func (b *BIOS) fetch(server FirmwareServer, platformID int, f BIOSFile) error {
	staging := filepath.Join(b.install.State(false).EmuDeck.Root, stagingDir, biosStaging, strconv.Itoa(f.ID))
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return fmt.Errorf("create staging folder: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()
	tmp := filepath.Join(staging, filepath.Base(f.Path))

	sha, md, crc := sha1.New(), md5.New(), crc32.NewIEEE() //nolint:gosec // see imports
	var out *os.File
	var size int64
	open := func(int64) (io.Writer, error) {
		if out != nil {
			_ = out.Close()
		}
		file, err := os.Create(tmp)
		if err != nil {
			return nil, fmt.Errorf("open download: %w", err)
		}
		out, size = file, 0
		sha.Reset()
		md.Reset()
		crc.Reset()
		return io.MultiWriter(file, sha, md, crc, countWriter{&size}), nil
	}
	err := server.DownloadFirmware(b.base, f.ID, f.FileName, open)
	if out != nil {
		if err == nil {
			err = out.Sync()
		}
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		return err
	}
	if out == nil {
		return errors.New("the server sent nothing to write")
	}
	sums := sumsFrom(crc, md, sha)
	if f.StatusOf(size, sums) != domain.BIOSPresent {
		return &domain.Error{Kind: domain.KindUnreachable, Message: f.FileName + " from RomM doesn't match RomM's own hash of it."}
	}

	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return err
	}
	if err := checkFree(f.Path); err != nil {
		return err
	}
	if err := moveFile(tmp, f.Path); err != nil {
		return fmt.Errorf("move %s into place: %w", f.FileName, err)
	}
	info, err := os.Stat(f.Path)
	if err == nil {
		err = b.store.SaveOwnedBIOS(context.WithoutCancel(b.base), domain.OwnedBIOS{
			FirmwareID:  f.ID,
			PlatformID:  platformID,
			File:        domain.PlacedFile{Path: f.Path, SizeBytes: info.Size(), ModTime: info.ModTime().UTC(), SHA1: sums.SHA1},
			InstalledAt: b.now().UTC(),
		})
	}
	if err != nil {
		// Without a record the file would be User-managed. It is the plugin's own download, so it
		// goes.
		if rmErr := os.Remove(f.Path); rmErr != nil {
			b.log.Error("remove placed BIOS file", "path", f.Path, "err", rmErr)
		}
		return fmt.Errorf("record BIOS file: %w", err)
	}
	b.log.Info("BIOS file placed", "firmware_id", f.ID, "path", f.Path)
	return nil
}

// statusAt compares the file at path with f. info is nil when nothing is there.
func (b *BIOS) statusAt(f domain.Firmware, path string) (domain.BIOSStatus, os.FileInfo, error) {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return domain.BIOSMissing, nil, nil
	case err != nil:
		return "", nil, err
	case info.IsDir():
		return domain.BIOSDifferent, info, nil
	}
	sums, err := b.sumsOf(path, info)
	if err != nil {
		return "", nil, err
	}
	return f.StatusOf(info.Size(), sums), info, nil
}

// sumsOf hashes the file at path, or returns the hashes from the last time while it looks the same.
func (b *BIOS) sumsOf(path string, info os.FileInfo) (domain.FileSums, error) {
	b.mu.Lock()
	c, ok := b.sums[path]
	b.mu.Unlock()
	if ok && c.size == info.Size() && c.mod.Equal(info.ModTime()) {
		return c.sums, nil
	}
	in, err := os.Open(path)
	if err != nil {
		return domain.FileSums{}, err
	}
	defer func() { _ = in.Close() }()
	sha, md, crc := sha1.New(), md5.New(), crc32.NewIEEE() //nolint:gosec // see imports
	if _, err := io.Copy(io.MultiWriter(sha, md, crc), in); err != nil {
		return domain.FileSums{}, fmt.Errorf("hash %s: %w", path, err)
	}
	sums := sumsFrom(crc, md, sha)
	b.mu.Lock()
	b.sums[path] = cachedSums{size: info.Size(), mod: info.ModTime(), sums: sums}
	b.mu.Unlock()
	return sums, nil
}

func sumsFrom(crc hash.Hash32, md, sha hash.Hash) domain.FileSums {
	return domain.FileSums{
		CRC:  fmt.Sprintf("%08x", crc.Sum32()),
		MD5:  hex.EncodeToString(md.Sum(nil)),
		SHA1: hex.EncodeToString(sha.Sum(nil)),
	}
}

// dir is the folder under EmuDeck's BIOS folder that folder names.
func (b *BIOS) dir(folder string) (string, error) {
	state := b.install.State(false)
	if !state.EmuDeckFound() {
		return "", &domain.Error{Kind: domain.KindCantDownload, Message: "EmuDeck wasn't found, so there's nowhere to put BIOS files. See Setup status."}
	}
	base := state.EmuDeck.BIOSPath
	if base == "" {
		base = filepath.Join(state.EmuDeck.Root, "bios")
	}
	return filepath.Join(base, folder), nil
}

// owned returns the records of a platform's Plugin-owned BIOS files, by firmware id.
func (b *BIOS) owned(ctx context.Context, platformID int) (map[int]domain.OwnedBIOS, error) {
	out := map[int]domain.OwnedBIOS{}
	if b.store == nil {
		return out, nil
	}
	recs, err := b.store.OwnedBIOS(ctx, platformID)
	if err != nil {
		return nil, err
	}
	for _, r := range recs {
		out[r.FirmwareID] = r
	}
	return out, nil
}

func (b *BIOS) isBusy(platformID int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.busy[platformID]
}

// start marks a platform's BIOS download as running, and reports false when one already is.
func (b *BIOS) start(platformID int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.busy[platformID] {
		return false
	}
	b.busy[platformID] = true
	return true
}

func (b *BIOS) finish(platformID int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.busy, platformID)
}

// countWriter counts the bytes written through it into n.
type countWriter struct{ n *int64 }

func (c countWriter) Write(p []byte) (int, error) {
	*c.n += int64(len(p))
	return len(p), nil
}

var errBIOSNotSignedIn = &domain.Error{Kind: domain.KindSignedOut, Message: "Sign in to RomM to download BIOS files."}
