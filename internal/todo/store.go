package todo

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
)

// Store serializes all mutations and file access. Callbacks run after the
// store lock is released, so a UI can safely request another snapshot.
type Store struct {
	mu       sync.RWMutex
	path     string
	data     Data
	onChange func()
	now      func() time.Time
}

func OpenStore(path string) (*Store, error) {
	return openStore(path, time.Now)
}

func openStore(path string, now func() time.Time) (*Store, error) {
	data, err := LoadWithRollover(path, now())
	if err != nil {
		return nil, err
	}
	return &Store{path: path, data: data, now: now}, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) SetOnChange(fn func()) {
	s.mu.Lock()
	s.onChange = fn
	s.mu.Unlock()
}

func (s *Store) Snapshot() Data {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Clone()
}

func (s *Store) changed() {
	s.mu.RLock()
	fn := s.onChange
	s.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

func (s *Store) mutate(fn func(*Data)) error {
	s.mu.Lock()
	before := s.data.Clone()
	fn(&s.data)
	err := Save(s.data, s.path)
	if err != nil {
		s.data = before
	}
	s.mu.Unlock()
	if err == nil {
		s.changed()
	}
	return err
}

// Reload ignores watcher echoes that contain no actual content change.
func (s *Store) Reload() error {
	fresh, err := LoadWithRollover(s.path, s.now())
	if err != nil {
		return err
	}
	s.mu.Lock()
	changed := !fresh.SameContent(s.data)
	if changed {
		s.data = fresh
	}
	s.mu.Unlock()
	if changed {
		s.changed()
	}
	return nil
}

// ErrEmptyText is the error of a task without text.
var ErrEmptyText = errors.New("todo: empty task text")

func (s *Store) Add(raw string, tier Tier) error {
	item, ok := ParseInput(raw)
	if !ok {
		return ErrEmptyText
	}
	return s.mutate(func(data *Data) {
		items := data.Items(tier)
		*items = append(*items, item)
	})
}

func (s *Store) Toggle(id uint64, tier Tier) error {
	return s.mutate(func(data *Data) {
		items := data.Items(tier)
		for i := range *items {
			if (*items)[i].ID == id {
				(*items)[i].Done = !(*items)[i].Done
				return
			}
		}
	})
}

func (s *Store) Delete(id uint64, tier Tier) error {
	return s.mutate(func(data *Data) {
		items := data.Items(tier)
		*items = slices.DeleteFunc(*items, func(item Item) bool { return item.ID == id })
	})
}

func (s *Store) SetQuadrant(id uint64, quadrant Quadrant, tier Tier) error {
	return s.mutate(func(data *Data) {
		items := data.Items(tier)
		for i := range *items {
			if (*items)[i].ID == id {
				(*items)[i].Quadrant = quadrant
				return
			}
		}
	})
}

func (s *Store) Move(id uint64, from, to Tier) error {
	if from == to {
		return nil
	}
	return s.mutate(func(data *Data) {
		source := data.Items(from)
		for i, item := range *source {
			if item.ID == id {
				*source = slices.Delete(*source, i, i+1)
				target := data.Items(to)
				*target = append(*target, item)
				return
			}
		}
	})
}

// UpdateText preserves completion and existing focus minutes. An explicit
// duration marker, including ⏱0m, overrides the accumulated value.
func (s *Store) UpdateText(id uint64, raw string, tier Tier) (bool, error) {
	item, ok := ParseInput(raw)
	if !ok {
		return false, nil
	}
	_, suffixes := StripSuffixMarkers(strings.TrimSpace(raw))
	changed := false
	err := s.mutate(func(data *Data) {
		items := data.Items(tier)
		for i := range *items {
			if (*items)[i].ID != id {
				continue
			}
			(*items)[i].Text = item.Text
			(*items)[i].Tags = item.Tags
			(*items)[i].Quadrant = item.Quadrant
			if suffixes.HasFocusMinutes {
				(*items)[i].FocusMinutes = suffixes.FocusMinutes
			}
			changed = true
			return
		}
	})
	return changed, err
}

func (s *Store) CreditFocusMinutes(text string, tier Tier, minutes int) error {
	if minutes <= 0 {
		return nil
	}
	return s.mutate(func(data *Data) {
		items := data.Items(tier)
		*items = ApplyCredit(*items, text, minutes)
	})
}

// FilteredItems returns a display-ready copy, sorted by completion and
// priority. An empty tag disables filtering.
func FilteredItems(data Data, tier Tier, tag string) []Item {
	items := *data.Items(tier)
	filtered := make([]Item, 0, len(items))
	for _, item := range items {
		if tag == "" || slices.Contains(item.Tags, tag) {
			filtered = append(filtered, item)
		}
	}
	return Sorted(filtered)
}

func TodayCounts(data Data, tag string) (remaining, completed, total int) {
	for _, item := range data.TodayItems {
		if tag != "" && !slices.Contains(item.Tags, tag) {
			continue
		}
		total++
		if item.Done {
			completed++
		} else {
			remaining++
		}
	}
	return
}
