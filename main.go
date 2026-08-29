package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36 Edg/119.0.0.0",
	"Mozilla/5.0 (Linux; Android 13; SM-G991B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
	"Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
}

type Site struct {
	Name         string
	URLTemplate  string
	CheckFunc    func(resp *http.Response, body string, finalURL string) bool
}

var defaultSites = map[string]Site{
	"soundcloud": {
		Name:        "soundcloud",
		URLTemplate: "https://soundcloud.com/%s",
		CheckFunc: func(resp *http.Response, body string, finalURL string) bool {
			if resp.StatusCode == http.StatusNotFound {
				return true
			}
			if resp.StatusCode == http.StatusOK && (strings.Contains(body, "sorry, we can't find that page") || strings.Contains(body, "user not found")) {
				return true
			}
			return false
		},
	},
	"twitter": {
		Name:        "twitter",
		URLTemplate: "https://twitter.com/%s",
		CheckFunc: func(resp *http.Response, body string, finalURL string) bool {
			if resp.StatusCode == http.StatusNotFound {
				return true
			}
			if resp.StatusCode == http.StatusOK && strings.Contains(body, "This account doesn’t exist") {
				return true
			}
			return false
		},
	},
	"instagram": {
		Name:        "instagram",
		URLTemplate: "https://www.instagram.com/%s/",
		CheckFunc: func(resp *http.Response, body string, finalURL string) bool {
			if resp.StatusCode == http.StatusNotFound {
				return true
			}
			if resp.StatusCode == http.StatusOK && (strings.Contains(body, "Sorry, this page isn't available.") || strings.Contains(body, "Page Not Found")) {
				return true
			}
			return false
		},
	},
	"github": {
		Name:        "github",
		URLTemplate: "https://github.com/%s",
		CheckFunc: func(resp *http.Response, body string, finalURL string) bool {
			return resp.StatusCode == http.StatusNotFound
		},
	},
	"reddit": {
		Name:        "reddit",
		URLTemplate: "https://www.reddit.com/user/%s/",
		CheckFunc: func(resp *http.Response, body string, finalURL string) bool {
			return resp.StatusCode == http.StatusNotFound
		},
	},
	"tiktok": {
		Name:        "tiktok",
		URLTemplate: "https://www.tiktok.com/@%s",
		CheckFunc: func(resp *http.Response, body string, finalURL string) bool {
			return resp.StatusCode == http.StatusNotFound
		},
	},
	"pinterest": {
		Name:        "pinterest",
		URLTemplate: "https://www.pinterest.com/%s/",
		CheckFunc: func(resp *http.Response, body string, finalURL string) bool {
			if resp.StatusCode == http.StatusNotFound {
				return true
			}
			if resp.StatusCode == http.StatusOK && strings.Contains(body, "Sorry, the page you were looking for doesn’t exist.") {
				return true
			}
			return false
		},
	},
	"youtube": {
		Name:        "youtube",
		URLTemplate: "https://www.youtube.com/@%s",
		CheckFunc: func(resp *http.Response, body string, finalURL string) bool {
			if resp.StatusCode == http.StatusNotFound {
				return true
			}
			if resp.StatusCode == http.StatusOK && strings.Contains(body, "This channel does not exist") {
				return true
			}
			return false
		},
	},
	"twitch": {
		Name:        "twitch",
		URLTemplate: "https://www.twitch.tv/%s",
		CheckFunc: func(resp *http.Response, body string, finalURL string) bool {
			return resp.StatusCode == http.StatusNotFound
		},
	},
}

type Config struct {
	Mode           string
	Length         int
	Total          int
	Threads        int
	Output         string
	Log            string
	Timeout        time.Duration
	ReqTimeout     time.Duration
	Rate           float64
	Retries        int
	Verbose        bool
	ProxyFile      string
	MinDelay       time.Duration
	MaxDelay       time.Duration
	OutputFormat   string
	StopAfterFound int
	Sites          map[string]Site
	SiteList       string
	CustomSites    string
}

type Result struct {
	Username  string `json:"username"`
	Site      string `json:"site"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

type ProxyPool struct {
	mu      sync.Mutex
	proxies []*url.URL
	index   int
}

func NewProxyPool(path string) (*ProxyPool, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var proxies []*url.URL
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "://") {
			line = "http://" + line
		}
		u, err := url.Parse(line)
		if err == nil {
			proxies = append(proxies, u)
		}
	}
	if len(proxies) == 0 {
		return nil, errors.New("no proxies loaded")
	}
	return &ProxyPool{proxies: proxies}, nil
}

func (p *ProxyPool) Next() *url.URL {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.proxies) == 0 {
		return nil
	}
	u := p.proxies[p.index%len(p.proxies)]
	p.index++
	return u
}

type RateLimiter struct {
	interval time.Duration
	tokens   chan struct{}
}

func NewRateLimiter(rps float64) *RateLimiter {
	if rps <= 0 {
		return nil
	}
	interval := time.Duration(float64(time.Second) / rps)
	rl := &RateLimiter{
		interval: interval,
		tokens:   make(chan struct{}, 1),
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for range t.C {
			select {
			case rl.tokens <- struct{}{}:
			default:
			}
		}
	}()
	return rl
}

func (rl *RateLimiter) Wait(ctx context.Context) error {
	if rl == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-rl.tokens:
		return nil
	}
}

func generateUsername(r *rand.Rand, cfg Config) string {
	var pool string
	switch cfg.Mode {
	case "letters":
		pool = "abcdefghijklmnopqrstuvwxyz"
	case "digits":
		pool = "0123456789"
	case "alphanumeric":
		pool = "abcdefghijklmnopqrstuvwxyz0123456789"
	default:
		pool = cfg.Mode
	}
	if pool == "" {
		pool = "abcdefghijklmnopqrstuvwxyz"
	}
	sb := strings.Builder{}
	sb.Grow(cfg.Length)
	for i := 0; i < cfg.Length; i++ {
		sb.WriteByte(pool[r.Intn(len(pool))])
	}
	return sb.String()
}

func isTransient(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr)
}

func checkSite(ctx context.Context, username string, site Site, client *http.Client, cfg Config, rl *RateLimiter, proxyPool *ProxyPool, ua string) (bool, error) {
	target := fmt.Sprintf(site.URLTemplate, username)
	for attempt := 0; attempt <= cfg.Retries; attempt++ {
		if err := rl.Wait(ctx); err != nil {
			return false, err
		}
		if cfg.MinDelay > 0 || cfg.MaxDelay > 0 {
			delay := cfg.MinDelay
			if cfg.MaxDelay > cfg.MinDelay {
				delay += time.Duration(rand.Int63n(int64(cfg.MaxDelay-cfg.MinDelay)))
			}
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(delay):
			}
		}
		reqCtx, cancel := context.WithTimeout(ctx, cfg.ReqTimeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
		if err != nil {
			cancel()
			return false, err
		}
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Connection", "keep-alive")
		if proxyPool != nil {
			proxyURL := proxyPool.Next()
			if proxyURL != nil {
				transport := &http.Transport{
					Proxy:           http.ProxyURL(proxyURL),
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				}
				client = &http.Client{
					Transport: transport,
					Timeout:   cfg.ReqTimeout,
					CheckRedirect: func(req *http.Request, via []*http.Request) error {
						if len(via) >= 5 {
							return errors.New("too many redirects")
						}
						return nil
					},
				}
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			if attempt < cfg.Retries && isTransient(err) {
				backoff := time.Duration(1<<uint(attempt))*time.Second + time.Duration(rand.Intn(500))*time.Millisecond
				select {
				case <-ctx.Done():
					return false, ctx.Err()
				case <-time.After(backoff):
				}
				continue
			}
			return false, err
		}
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		resp.Body.Close()
		cancel()
		body := strings.ToLower(string(bodyBytes))
		finalURL := resp.Request.URL.String()
		if resp.StatusCode == http.StatusTooManyRequests {
			if attempt < cfg.Retries {
				backoff := time.Duration(2<<uint(attempt))*time.Second + time.Duration(rand.Intn(1000))*time.Millisecond
				select {
				case <-ctx.Done():
					return false, ctx.Err()
				case <-time.After(backoff):
				}
				continue
			}
			return false, errors.New("rate limited")
		}
		if attempt < cfg.Retries && (resp.StatusCode >= 500 || resp.StatusCode == 408) {
			backoff := time.Duration(1<<uint(attempt))*time.Second + time.Duration(rand.Intn(500))*time.Millisecond
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(backoff):
			}
			continue
		}
		return site.CheckFunc(resp, body, finalURL), nil
	}
	return false, errors.New("max retries")
}

func worker(ctx context.Context, cfg Config, jobs <-chan int, results chan<- Result, client *http.Client, rl *RateLimiter, proxyPool *ProxyPool, seed int64, wg *sync.WaitGroup) {
	defer wg.Done()
	r := rand.New(rand.NewSource(seed))
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-jobs:
			if !ok {
				return
			}
			username := generateUsername(r, cfg)
			ua := userAgents[rand.Intn(len(userAgents))]
			for _, site := range cfg.Sites {
				avail, err := checkSite(ctx, username, site, client, cfg, rl, proxyPool, ua)
				res := Result{Username: username, Site: site.Name, Available: avail}
				if err != nil {
					res.Error = err.Error()
				}
				results <- res
			}
		}
	}
}

func progress(ctx context.Context, total int, processed *int64, stopChan chan struct{}, foundAvail *int64, stopAfter int) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			done := atomic.LoadInt64(processed)
			avail := atomic.LoadInt64(foundAvail)
			elapsed := time.Since(start)
			rate := float64(done) / elapsed.Seconds()
			fmt.Printf("\rProgress: %d/%d (%.1f%%) | Rate: %.1f/s | Found: %d | Elapsed: %s",
				done, total, float64(done)/float64(total)*100, rate, avail, elapsed.Round(time.Second))
			if stopAfter > 0 && avail >= int64(stopAfter) {
				close(stopChan)
				return
			}
		}
	}
}

func saveResults(results <-chan Result, cfg Config, processed *int64, foundAvail *int64) error {
	out, err := os.Create(cfg.Output)
	if err != nil {
		return err
	}
	defer out.Close()
	var logFile *os.File
	var logWriter *bufio.Writer
	if cfg.Log != "" {
		logFile, err = os.Create(cfg.Log)
		if err != nil {
			return err
		}
		defer logFile.Close()
		logWriter = bufio.NewWriter(logFile)
		defer logWriter.Flush()
	}
	var writer interface {
		Write(p []byte) (n int, err error)
		Flush() error
	}
	switch cfg.OutputFormat {
	case "csv":
		csvWriter := csv.NewWriter(out)
		csvWriter.Write([]string{"username", "site", "available", "error"})
		writer = csvWriter
	case "json":
		writer = out
	default:
		writer = bufio.NewWriter(out)
	}
	defer writer.Flush()
	var available, taken, errCount int
	for res := range results {
		atomic.AddInt64(processed, 1)
		if res.Error != "" {
			errCount++
			if cfg.Verbose {
				fmt.Printf("\n[error] %s on %s: %s\n", res.Username, res.Site, res.Error)
			}
			if logWriter != nil {
				logWriter.WriteString(fmt.Sprintf("ERROR\t%s\t%s\t%s\n", res.Username, res.Site, res.Error))
			}
			continue
		}
		if res.Available {
			atomic.AddInt64(foundAvail, 1)
			available++
			if cfg.Verbose {
				fmt.Printf("\n[available] %s on %s\n", res.Username, res.Site)
			}
			switch cfg.OutputFormat {
			case "csv":
				writer.(*csv.Writer).Write([]string{res.Username, res.Site, "true", ""})
			case "json":
				b, _ := json.Marshal(res)
				writer.Write(append(b, '\n'))
			default:
				fmt.Fprintf(writer, "%s:%s\n", res.Site, res.Username)
			}
		} else {
			taken++
			if cfg.Verbose {
				fmt.Printf("\n[taken] %s on %s\n", res.Username, res.Site)
			}
			if logWriter != nil {
				logWriter.WriteString(fmt.Sprintf("TAKEN\t%s\t%s\n", res.Username, res.Site))
			}
		}
	}
	fmt.Printf("\n\nSummary\nAvailable: %d\nTaken: %d\nErrors: %d\n", available, taken, errCount)
	fmt.Printf("Saved to %s\n", cfg.Output)
	return nil
}

func parseCustomSites(spec string, sites map[string]Site) error {
	if spec == "" {
		return nil
	}
	parts := strings.Split(spec, ";")
	for _, part := range parts {
		fields := strings.Split(part, ",")
		if len(fields) < 2 {
			return fmt.Errorf("invalid custom site spec: %s", part)
		}
		name := fields[0]
		tpl := fields[1]
		var marker string
		if len(fields) >= 3 {
			marker = fields[2]
		}
		sites[name] = Site{
			Name:        name,
			URLTemplate: tpl,
			CheckFunc: func(resp *http.Response, body string, finalURL string) bool {
				if resp.StatusCode == http.StatusNotFound {
					return true
				}
				if marker != "" && strings.Contains(body, strings.ToLower(marker)) {
					return true
				}
				return false
			},
		}
	}
	return nil
}

func main() {
	cfg := Config{}
	flag.StringVar(&cfg.Mode, "mode", "letters", "charset: letters, digits, alphanumeric, or custom string")
	flag.IntVar(&cfg.Length, "length", 4, "username length")
	flag.IntVar(&cfg.Total, "total", 100, "total usernames to generate")
	flag.IntVar(&cfg.Threads, "threads", 10, "concurrent workers")
	flag.StringVar(&cfg.Output, "output", "available.txt", "output file")
	flag.StringVar(&cfg.Log, "log", "", "optional log file")
	flag.DurationVar(&cfg.Timeout, "timeout", 60*time.Second, "overall timeout (0 unlimited)")
	flag.DurationVar(&cfg.ReqTimeout, "req-timeout", 10*time.Second, "request timeout")
	flag.Float64Var(&cfg.Rate, "rate", 5.0, "requests per second (0 unlimited)")
	flag.IntVar(&cfg.Retries, "retries", 3, "retry count")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "print every result")
	flag.StringVar(&cfg.ProxyFile, "proxy-file", "", "file with proxies, one per line")
	flag.DurationVar(&cfg.MinDelay, "min-delay", 0, "minimum delay before each request")
	flag.DurationVar(&cfg.MaxDelay, "max-delay", 0, "maximum delay before each request")
	flag.StringVar(&cfg.OutputFormat, "format", "text", "output format: text, csv, json")
	flag.IntVar(&cfg.StopAfterFound, "stop-after", 0, "stop after finding N available (0 no limit)")
	flag.StringVar(&cfg.SiteList, "sites", "", "comma-separated list of sites to check (empty = all default)")
	flag.StringVar(&cfg.CustomSites, "custom-sites", "", "custom sites spec: name:url_template:notfound_marker;name2:url2:marker2")
	flag.Parse()

	if cfg.Length <= 0 || cfg.Total <= 0 || cfg.Threads <= 0 {
		fmt.Println("invalid config")
		os.Exit(1)
	}

	sites := make(map[string]Site)
	for k, v := range defaultSites {
		sites[k] = v
	}
	if err := parseCustomSites(cfg.CustomSites, sites); err != nil {
		fmt.Printf("custom sites error: %v\n", err)
		os.Exit(1)
	}
	if cfg.SiteList != "" {
		selected := strings.Split(cfg.SiteList, ",")
		newSites := make(map[string]Site)
		for _, name := range selected {
			name = strings.TrimSpace(name)
			if site, ok := sites[name]; ok {
				newSites[name] = site
			} else {
				fmt.Printf("unknown site: %s\n", name)
				os.Exit(1)
			}
		}
		sites = newSites
	}
	cfg.Sites = sites
	if len(cfg.Sites) == 0 {
		fmt.Println("no sites selected")
		os.Exit(1)
	}

	var ctx context.Context
	var cancel context.CancelFunc
	if cfg.Timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), cfg.Timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nshutting down...")
		cancel()
	}()

	proxyPool, err := NewProxyPool(cfg.ProxyFile)
	if err != nil {
		fmt.Printf("proxy error: %v\n", err)
		os.Exit(1)
	}

	client := &http.Client{
		Timeout: cfg.ReqTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}

	rl := NewRateLimiter(cfg.Rate)

	totalChecks := cfg.Total * len(cfg.Sites)
	jobs := make(chan int, cfg.Threads*2)
	results := make(chan Result, cfg.Threads*2)
	var processed int64
	var foundAvail int64
	stopChan := make(chan struct{})
	go progress(ctx, totalChecks, &processed, stopChan, &foundAvail, cfg.StopAfterFound)

	var wg sync.WaitGroup
	for i := 0; i < cfg.Threads; i++ {
		wg.Add(1)
		seed := time.Now().UnixNano() + int64(i)*1000
		go worker(ctx, cfg, jobs, results, client, rl, proxyPool, seed, &wg)
	}

	go func() {
		defer close(jobs)
		for i := 0; i < cfg.Total; i++ {
			select {
			case <-ctx.Done():
				return
			case <-stopChan:
				return
			case jobs <- i:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	if err := saveResults(results, cfg, &processed, &foundAvail); err != nil {
		fmt.Printf("save error: %v\n", err)
		os.Exit(1)
	}
}
