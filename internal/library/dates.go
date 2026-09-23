package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"stash-vr/internal/config"
	"stash-vr/internal/stash/gql"
)

// dateSweepInterval is the pause between two stash-box lookups. A variable
// so tests can shrink it.
var dateSweepInterval = 3 * time.Second

const (
	// dateMissExpiry is how long a scene no stash-box knew is left alone
	// before it is asked again.
	dateMissExpiry = 30 * 24 * time.Hour
	// dateFlushDebounce bounds how often dates.json is rewritten while the
	// sweeper is busy.
	dateFlushDebounce = 5 * time.Second
	// dateWarnEvery rate-limits the warning logged for a failing stash-box.
	dateWarnEvery = time.Hour
	datesFileName = "dates.json"
)

// dateEntry is what dates.json holds per scene id. A miss has an empty Date.
type dateEntry struct {
	Date    string    `json:"date"`
	Source  string    `json:"source,omitempty"`
	Checked time.Time `json:"checked"`
}

// settled reports whether the scene needs no lookup: it has a date, or it
// was a miss within dateMissExpiry.
func (e dateEntry) settled(now time.Time) bool {
	return e.Date != "" || now.Sub(e.Checked) < dateMissExpiry
}

// dateStore is the persisted set of looked-up release dates.
type dateStore struct {
	mu        sync.Mutex
	path      string // empty keeps the store in memory only
	entries   map[string]dateEntry
	dirty     bool
	lastFlush time.Time
}

// loadDateStore reads path. A missing or unreadable file gives an empty
// store; the next flush replaces a broken file.
func loadDateStore(path string) *dateStore {
	d := &dateStore{path: path, entries: map[string]dateEntry{}}
	if path == "" {
		return d
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Warn().Err(err).Str("path", path).Msg("Release dates: store not readable, starting empty")
		}
		return d
	}
	if err := json.Unmarshal(data, &d.entries); err != nil {
		log.Warn().Err(err).Str("path", path).Msg("Release dates: store not parsable, starting empty")
		d.entries = map[string]dateEntry{}
	}
	return d
}

func (d *dateStore) get(id string) (dateEntry, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.entries[id]
	return e, ok
}

func (d *dateStore) put(id string, e dateEntry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries[id] = e
	d.dirty = true
}

// flush writes the store atomically when it has unsaved changes.
func (d *dateStore) flush() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.dirty || d.path == "" {
		d.dirty = false
		return nil
	}
	data, err := json.MarshalIndent(d.entries, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(d.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, datesFileName+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), d.path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	d.dirty = false
	d.lastFlush = time.Now()
	return nil
}

// flushDue flushes when the last write is at least dateFlushDebounce old.
func (d *dateStore) flushDue() error {
	d.mu.Lock()
	due := d.dirty && time.Since(d.lastFlush) >= dateFlushDebounce
	d.mu.Unlock()
	if !due {
		return nil
	}
	return d.flush()
}

// dates returns the store, loading it from the config directory on first use.
func (libraryService *Service) dates() *dateStore {
	libraryService.datesOnce.Do(func() {
		path := ""
		if dir := config.Application().ConfigPath; dir != "" {
			path = filepath.Join(dir, datesFileName)
		}
		libraryService.dateStore = loadDateStore(path)
	})
	return libraryService.dateStore
}

func (libraryService *Service) flushDates() {
	if err := libraryService.dates().flush(); err != nil {
		log.Warn().Err(err).Msg("Release dates: store not written")
	}
}

// LookedUpDate returns the date a stash-box gave for scene id, or "".
func (libraryService *Service) LookedUpDate(id string) string {
	e, _ := libraryService.dates().get(id)
	return e.Date
}

// DateStats counts the undated scenes of the index by lookup state.
type DateStats struct{ Found, Missing, Unchecked int }

// DateStats covers the cached scenes whose Stash date is empty; scenes not
// fetched yet are left out.
func (libraryService *Service) DateStats() DateStats {
	now := time.Now()
	store := libraryService.dates()
	var stats DateStats
	for id, vd := range libraryService.snapshot() {
		if vd == nil || vd.stashDate() != "" {
			continue
		}
		e, ok := store.get(id)
		switch {
		case ok && e.Date != "":
			stats.Found++
		case ok && e.settled(now):
			stats.Missing++
		default:
			stats.Unchecked++
		}
	}
	return stats
}

// scraped is one scrapeSingleScene result.
type scraped struct{ Title, Date string }

// normalizeTitle keeps the lower-cased letters and digits of s.
func normalizeTitle(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// validReleaseDate accepts full YYYY-MM-DD dates other than the Unix epoch,
// which some stash-box entries carry as a placeholder.
func validReleaseDate(date string) bool {
	if date == "1970-01-01" {
		return false
	}
	_, err := time.Parse("2006-01-02", date)
	return err == nil
}

// acceptScrapedDate picks the first result that is either the only result or
// whose normalised title equals the scene's normalised title or file stem,
// provided its date is a full date.
func acceptScrapedDate(sceneTitle, fileStem string, results []scraped) (date string, ok bool) {
	wanted := map[string]struct{}{}
	for _, s := range []string{sceneTitle, fileStem} {
		if n := normalizeTitle(s); n != "" {
			wanted[n] = struct{}{}
		}
	}
	for _, r := range results {
		_, match := wanted[normalizeTitle(r.Title)]
		if (len(results) == 1 || match) && validReleaseDate(r.Date) {
			return r.Date, true
		}
	}
	return "", false
}

// stashBox is one entry of Stash's stash-box list; its position in the list
// is the stash_box_index scrapes use.
type stashBox struct{ name, endpoint string }

// label names the box in logs and dates.json.
func (b stashBox) label() string {
	if b.name != "" {
		return b.name
	}
	if u, err := url.Parse(b.endpoint); err == nil && u.Host != "" {
		return u.Host
	}
	return b.endpoint
}

// dateSweeper is the sweeper's shared state: requested scenes, the rescan
// flag set by index builds, the stash-box list of the current run and the
// warning rate limit.
type dateSweeper struct {
	mu       sync.Mutex
	wake     chan struct{}
	priority []string
	queued   map[string]struct{}
	rescan   bool
	boxes    []stashBox
	boxesOK  bool
	warned   map[string]time.Time
	started  bool
}

func newDateSweeper() *dateSweeper {
	return &dateSweeper{
		wake:   make(chan struct{}, 1),
		queued: map[string]struct{}{},
		warned: map[string]time.Time{},
		rescan: true,
	}
}

func (sw *dateSweeper) notify() {
	select {
	case sw.wake <- struct{}{}:
	default:
	}
}

// kickDateSweeper asks the sweeper to walk the index again; it never blocks.
func (libraryService *Service) kickDateSweeper() {
	sw := libraryService.sweeper
	sw.mu.Lock()
	sw.rescan = true
	sw.mu.Unlock()
	sw.notify()
}

// RequestDate queues scene id ahead of the index walk when its release date
// is unknown and it has not been checked recently. It never blocks.
func (libraryService *Service) RequestDate(id string) {
	if !config.Application().DateLookup {
		return
	}
	if e, ok := libraryService.dates().get(id); ok && e.settled(time.Now()) {
		return
	}
	sw := libraryService.sweeper
	sw.mu.Lock()
	if _, dup := sw.queued[id]; !dup {
		sw.queued[id] = struct{}{}
		sw.priority = append(sw.priority, id)
	}
	sw.mu.Unlock()
	sw.notify()
}

func (sw *dateSweeper) popPriority() (string, bool) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if len(sw.priority) == 0 {
		return "", false
	}
	id := sw.priority[0]
	sw.priority = sw.priority[1:]
	delete(sw.queued, id)
	return id, true
}

// takeRescan reports and clears the rescan flag, dropping the cached box
// list so each run starts from Stash's current configuration.
func (sw *dateSweeper) takeRescan() bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if !sw.rescan {
		return false
	}
	sw.rescan = false
	sw.boxes, sw.boxesOK = nil, false
	return true
}

func (sw *dateSweeper) dropPriority() {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.priority = nil
	clear(sw.queued)
}

// warn logs err for key at most once per dateWarnEvery.
func (sw *dateSweeper) warn(ctx context.Context, key string, err error) {
	sw.mu.Lock()
	last, seen := sw.warned[key]
	now := time.Now()
	if seen && now.Sub(last) < dateWarnEvery {
		sw.mu.Unlock()
		log.Ctx(ctx).Debug().Err(err).Str("source", key).Msg("Release dates: lookup failed")
		return
	}
	sw.warned[key] = now
	sw.mu.Unlock()
	log.Ctx(ctx).Warn().Err(err).Str("source", key).Msg("Release dates: lookup failed")
}

// stashBoxes returns the stash-boxes configured in Stash, cached for the
// current sweeper run.
func (libraryService *Service) stashBoxes(ctx context.Context) ([]stashBox, error) {
	sw := libraryService.sweeper
	sw.mu.Lock()
	if sw.boxesOK {
		boxes := sw.boxes
		sw.mu.Unlock()
		return boxes, nil
	}
	sw.mu.Unlock()

	resp, err := gql.StashBoxes(ctx, libraryService.Client())
	if err != nil {
		return nil, fmt.Errorf("StashBoxes: %w", err)
	}
	var boxes []stashBox
	if resp.Configuration != nil && resp.Configuration.General != nil {
		for _, b := range resp.Configuration.General.StashBoxes {
			if b != nil {
				boxes = append(boxes, stashBox{name: b.Name, endpoint: b.Endpoint})
			}
		}
	}
	sw.mu.Lock()
	sw.boxes, sw.boxesOK = boxes, true
	sw.mu.Unlock()
	return boxes, nil
}

// setReleaseDate gives the cached scene id its looked-up date. The cached
// VideoData is replaced, not modified, because readers share the pointer.
func (libraryService *Service) setReleaseDate(id, date string) {
	libraryService.muVdCache.Lock()
	defer libraryService.muVdCache.Unlock()
	if vd := libraryService.vdCache[id]; vd != nil {
		next := *vd
		next.releaseDate = date
		libraryService.vdCache[id] = &next
	}
}

// lookupDate asks each stash-box in order for scene id's release date and
// stores the first accepted date, or a miss when every box answered without
// one. A box error leaves the scene unchecked. It reports false when the run
// should stop because no stash-box can be asked.
func (libraryService *Service) lookupDate(ctx context.Context, id string) bool {
	vd, err := libraryService.GetScene(ctx, id, false)
	if err != nil {
		log.Ctx(ctx).Debug().Err(err).Str("scene", id).Msg("Release dates: scene skipped")
		return true
	}
	if vd.stashDate() != "" {
		return true
	}
	boxes, err := libraryService.stashBoxes(ctx)
	if err != nil {
		libraryService.sweeper.warn(ctx, "stash-box list", err)
		return false
	}
	if len(boxes) == 0 {
		return false
	}

	title := ""
	if vd.SceneParts.Title != nil {
		title = *vd.SceneParts.Title
	}
	stem := ""
	if len(vd.SceneParts.Files) > 0 && vd.SceneParts.Files[0] != nil {
		base := vd.SceneParts.Files[0].Basename
		stem = strings.TrimSuffix(base, filepath.Ext(base))
	}

	failed := false
	for i, box := range boxes {
		resp, err := gql.ScrapeSceneDate(ctx, libraryService.Client(), i, id)
		if err != nil {
			if ctx.Err() != nil {
				return false
			}
			libraryService.sweeper.warn(ctx, box.label(), err)
			failed = true
			continue
		}
		results := make([]scraped, 0, len(resp.ScrapeSingleScene))
		for _, r := range resp.ScrapeSingleScene {
			if r == nil {
				continue
			}
			var s scraped
			if r.Title != nil {
				s.Title = *r.Title
			}
			if r.Date != nil {
				s.Date = *r.Date
			}
			results = append(results, s)
		}
		date, ok := acceptScrapedDate(title, stem, results)
		if !ok {
			continue
		}
		libraryService.dates().put(id, dateEntry{Date: date, Source: box.label(), Checked: time.Now().UTC()})
		libraryService.setReleaseDate(id, date)
		log.Ctx(ctx).Info().Str("scene", id).Str("date", date).Str("source", box.label()).Msg("Release date found")
		if config.Application().DateWriteback {
			libraryService.writeBackDate(ctx, id, date)
		}
		return true
	}
	if failed {
		return true
	}
	libraryService.dates().put(id, dateEntry{Checked: time.Now().UTC()})
	log.Ctx(ctx).Debug().Str("scene", id).Msg("Release date not found on any stash-box")
	return true
}

// writeBackDate stores date on the scene in Stash and refreshes the cache.
func (libraryService *Service) writeBackDate(ctx context.Context, id, date string) {
	if _, err := gql.SceneUpdateDate(ctx, libraryService.Client(), id, &date); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("scene", id).Msg("Release dates: write-back failed")
		return
	}
	if _, err := libraryService.GetScene(ctx, id, true); err != nil {
		log.Ctx(ctx).Debug().Err(err).Str("scene", id).Msg("Release dates: scene not refreshed after write-back")
	}
}

// dateCandidates returns the index's undated scenes that need a lookup, in
// numeric id order. It fetches the scenes the cache does not hold yet.
func (libraryService *Service) dateCandidates(ctx context.Context) []string {
	libraryService.muVdCache.RLock()
	empty := len(libraryService.vdCache) == 0
	libraryService.muVdCache.RUnlock()
	if empty {
		return nil
	}
	vds, err := libraryService.GetScenes(ctx)
	if err != nil {
		log.Ctx(ctx).Debug().Err(err).Msg("Release dates: index not readable")
		return nil
	}
	now := time.Now()
	store := libraryService.dates()
	var ids []string
	for id, vd := range vds {
		if vd == nil || vd.stashDate() != "" {
			continue
		}
		if e, ok := store.get(id); ok && e.settled(now) {
			continue
		}
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int {
		x, _ := strconv.Atoi(a)
		y, _ := strconv.Atoi(b)
		return x - y
	})
	return ids
}

// StartDateSweeper runs the release-date sweeper until ctx ends. Calls after
// the first are ignored.
func (libraryService *Service) StartDateSweeper(ctx context.Context) {
	sw := libraryService.sweeper
	sw.mu.Lock()
	started := sw.started
	sw.started = true
	sw.mu.Unlock()
	if started {
		return
	}
	go libraryService.runDateSweeper(ctx)
}

// runDateSweeper looks up one scene per dateSweepInterval: requested scenes
// first, then the undated scenes of the index. It walks the index again after
// every index build and idles when there is nothing to do.
func (libraryService *Service) runDateSweeper(ctx context.Context) {
	sw := libraryService.sweeper
	defer libraryService.flushDates()
	var queue []string
	for {
		if ctx.Err() != nil {
			return
		}
		enabled := config.Application().DateLookup
		if sw.takeRescan() {
			queue = nil
			if enabled {
				queue = libraryService.dateCandidates(ctx)
			}
		}
		if !enabled {
			queue = nil
			sw.dropPriority()
		}

		id, ok := sw.popPriority()
		if !ok && len(queue) > 0 {
			id, queue, ok = queue[0], queue[1:], true
		}
		if !ok {
			libraryService.flushDates()
			select {
			case <-ctx.Done():
				return
			case <-sw.wake:
			}
			continue
		}
		if e, found := libraryService.dates().get(id); found && e.settled(time.Now()) {
			continue
		}

		if !libraryService.lookupDate(ctx, id) {
			queue = nil
			sw.dropPriority()
		}
		if err := libraryService.dates().flushDue(); err != nil {
			log.Ctx(ctx).Warn().Err(err).Msg("Release dates: store not written")
		}

		timer := time.NewTimer(dateSweepInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
