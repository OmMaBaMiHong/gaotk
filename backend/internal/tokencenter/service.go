package tokencenter

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed demo.json
var demoJSON []byte

type Config struct {
	SourceURL    string
	SourceToken  string
	SourceFormat string
	Interval     time.Duration
	Timeout      time.Duration
	SeedDemo     bool
}

func ConfigFromEnv() (Config, error) {
	c := Config{SourceURL: strings.TrimSpace(os.Getenv("TOKENCENTER_SOURCE_URL")), SourceToken: os.Getenv("TOKENCENTER_SOURCE_TOKEN"), SourceFormat: strings.TrimSpace(os.Getenv("TOKENCENTER_SOURCE_FORMAT")), Interval: 15 * time.Minute, Timeout: 20 * time.Second}
	if c.SourceFormat == "" {
		c.SourceFormat = "catalog"
	}
	for key, dest := range map[string]*time.Duration{"TOKENCENTER_SYNC_INTERVAL": &c.Interval, "TOKENCENTER_SYNC_TIMEOUT": &c.Timeout} {
		if value := os.Getenv(key); value != "" {
			parsed, err := time.ParseDuration(value)
			if err != nil || parsed <= 0 {
				return c, fmt.Errorf("%s must be a positive duration", key)
			}
			*dest = parsed
		}
	}
	if value := os.Getenv("TOKENCENTER_SEED_DEMO"); value != "" {
		var err error
		c.SeedDemo, err = strconv.ParseBool(value)
		if err != nil {
			return c, errors.New("TOKENCENTER_SEED_DEMO must be boolean")
		}
	}
	if err := c.validate(); err != nil {
		return c, err
	}
	return c, nil
}
func (c Config) validate() error {
	if c.SourceFormat != "" && c.SourceFormat != "catalog" && c.SourceFormat != "chaos" {
		return errors.New("TOKENCENTER_SOURCE_FORMAT must be catalog or chaos")
	}
	if c.SourceURL != "" && !webURL(c.SourceURL) {
		return errors.New("TOKENCENTER_SOURCE_URL must be an http(s) URL without credentials or fragment")
	}
	if c.SourceToken != "" && (!strings.HasPrefix(c.SourceURL, "https://") || strings.ContainsAny(c.SourceToken, "\r\n")) {
		return errors.New("TOKENCENTER_SOURCE_TOKEN requires HTTPS and cannot contain line breaks")
	}
	if c.Interval <= 0 || c.Timeout <= 0 {
		return errors.New("sync interval and timeout must be positive")
	}
	return nil
}

type Service struct {
	store       Store
	cfg         Config
	client      *http.Client
	mu          sync.Mutex
	lifecycleMu sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
}

func NewService(store Store, cfg Config) (*Service, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Service{store: store, cfg: cfg, client: &http.Client{
		Timeout: cfg.Timeout,
		// A trusted configured source cannot redirect the fetch/token to another host.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}
func DemoCatalog() (Catalog, error) { return Decode(bytes.NewReader(demoJSON)) }
func (s *Service) Seed(ctx context.Context) error {
	if !s.cfg.SeedDemo {
		return nil
	}
	c, err := DemoCatalog()
	if err != nil {
		return err
	}
	return s.store.Save(ctx, Snapshot{Catalog: c}, true)
}
func (s *Service) Catalog(ctx context.Context) (Catalog, error) {
	snapshot, err := s.store.Load(ctx)
	return snapshot.Catalog, err
}
func (s *Service) Import(ctx context.Context, catalog Catalog) (Catalog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := Normalize(catalog)
	if err != nil {
		return c, err
	}
	// A manual/full import invalidates prior source validators atomically.
	err = s.store.Save(ctx, Snapshot{Catalog: c}, false)
	return c, err
}
func (s *Service) Sync(ctx context.Context) (Catalog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.SourceURL == "" {
		return Catalog{}, ErrNoSource
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	previous, err := s.store.Load(ctx)
	if err != nil && !errors.Is(err, ErrEmpty) {
		return Catalog{}, err
	}
	if s.cfg.SourceFormat == "chaos" {
		return s.syncChaos(ctx, previous)
	}
	var validators *Snapshot
	if previous.SourceURL == s.cfg.SourceURL && previous.Catalog.RankingAuthority != "chaos" {
		validators = &previous
	}
	response, err := s.sourceRequest(ctx, s.cfg.SourceURL, validators)
	if err != nil {
		return Catalog{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		if validators == nil || (previous.ETag == "" && previous.LastModified == "") {
			return Catalog{}, errors.New("source returned 304 without a successful snapshot")
		}
		return previous.Catalog, nil
	}
	if response.StatusCode != http.StatusOK {
		return Catalog{}, fmt.Errorf("catalog source returned HTTP %d", response.StatusCode)
	}
	catalog, err := Decode(response.Body)
	if err != nil {
		return Catalog{}, err
	}
	snapshot := Snapshot{Catalog: catalog, SourceURL: s.cfg.SourceURL, ETag: response.Header.Get("ETag"), LastModified: response.Header.Get("Last-Modified")}
	if err = s.store.Save(ctx, snapshot, false); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func (s *Service) sourceRequest(ctx context.Context, target string, previous *Snapshot) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, errors.New("invalid configured catalog source")
	}
	req.Header.Set("Accept", "application/json")
	if s.cfg.SourceToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.SourceToken)
	}
	if previous != nil {
		if previous.ETag != "" {
			req.Header.Set("If-None-Match", previous.ETag)
		}
		if previous.LastModified != "" {
			req.Header.Set("If-Modified-Since", previous.LastModified)
		}
	}
	response, err := s.client.Do(req)
	if err != nil {
		return nil, errors.New("catalog source request failed (connection or timeout)")
	}
	return response, nil
}

// Start performs an immediate fetch, then periodic refreshes. No source means no worker.
func (s *Service) Start(parent context.Context) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.cancel != nil || s.cfg.SourceURL == "" {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel, s.done = cancel, make(chan struct{})
	go func() {
		defer close(s.done)
		run := func(interval time.Duration) time.Duration {
			catalog, err := s.Sync(ctx)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("[TokenCenter] sync failed; keeping last successful catalog: %v", err)
				}
				return interval
			}
			if s.cfg.SourceFormat == "chaos" {
				if published, ok := publishedPullInterval(catalog); ok {
					return published
				}
			}
			return interval
		}
		interval := run(s.cfg.Interval)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if next := run(interval); next != interval {
					interval = next
					ticker.Reset(interval)
				}
			}
		}
	}()
}

func publishedPullInterval(catalog Catalog) (time.Duration, bool) {
	if catalog.SourceSnapshot == nil {
		return 0, false
	}
	var config struct {
		Delivery struct {
			PullIntervalSeconds int64 `json:"pullIntervalSeconds"`
		} `json:"delivery"`
	}
	if json.Unmarshal(catalog.SourceSnapshot.Config, &config) != nil || config.Delivery.PullIntervalSeconds <= 0 {
		return 0, false
	}
	// Native delivery configuration is integer seconds. ParseDuration checks the
	// runtime representation without introducing a business min/max interval.
	interval, err := time.ParseDuration(strconv.FormatInt(config.Delivery.PullIntervalSeconds, 10) + "s")
	return interval, err == nil && interval > 0
}
func (s *Service) Stop() {
	s.lifecycleMu.Lock()
	cancel, done := s.cancel, s.done
	s.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}
