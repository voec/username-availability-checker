package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	letters       = "abcdefghijklmnopqrstuvwxyz"
	chars         = "abcdefghijklmnopqrstuvwxyz0123456789"
	baseURL       = "https://github.com/%s" // change this
	defaultUAFile = ""
)

var defaultUserAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0",
}

type Config struct {
	Mode        string
	TotalChecks int
	ThreadCount int
	OutputFile  string
	Timeout     time.Duration
	UAFile      string
	Delay       time.Duration
	Retries     int
	Proxy       string
	Quiet       bool
}

type Result struct {
	Username  string
	Available bool
	Error     error
}

type Stats struct {
	Available int64
	Taken     int64
	Errors    int64
	Checked   int64
	Start     time.Time
}

func (s *Stats) AddAvailable() { atomic.AddInt64(&s.Available, 1) }
func (s *Stats) AddTaken()     { atomic.AddInt64(&s.Taken, 1) }
func (s *Stats) AddError()     { atomic.AddInt64(&s.Errors, 1) }
func (s *Stats) AddChecked()   { atomic.AddInt64(&s.Checked, 1) }

func generate(r *rand.Rand, mode string) string {
	pool := letters
	if mode == "alnum" {
		pool = chars
	}
	sb := strings.Builder{}
	sb.Grow(4)
	for i := 0; i < 4; i++ {
		sb.WriteByte(pool[r.Intn(len(pool))])
	}
	return sb.String()
}

func uniqueUsername(r *rand.Rand, mode string, seen *sync.Map) string {
	for {
		name := generate(r, mode)
		if _, loaded := seen.LoadOrStore(name, struct{}{}); !loaded {
			return name
		}
	}
}

func loadUserAgents(filename string) ([]string, error) {
	if filename == "" {
		return defaultUserAgents, nil
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var agents []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			agents = append(agents, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		return nil, fmt.Errorf("no user agents found in file %s", filename)
	}
	return agents, nil
}

func newHTTPClient(timeout time.Duration, proxyURL string) (*http.Client, error) {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %w", err)
		}
		transport.Proxy = http.ProxyURL(u)
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}, nil
}

func checkUsername(ctx context.Context, username string, client *http.Client) (bool, error) {
	url := fmt.Sprintf(baseURL, username)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("creating request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound:
		return true, nil
	case http.StatusOK:
		return false, nil
	case http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable:
		return false, fmt.Errorf("temporary status %d", resp.StatusCode)
	default:
		return false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
}

type workerInput struct {
	ctx        context.Context
	mode       string
	jobs       <-chan int
	results    chan<- Result
	client     *http.Client
	userAgents []string
	rateLimit  <-chan time.Time
	delay      time.Duration
	retries    int
	seen       *sync.Map
	wg         *sync.WaitGroup
}

func worker(in workerInput) {
	defer in.wg.Done()
}

func workerWithSeed(
	ctx context.Context,
	mode string,
	jobs <-chan int,
	results chan<- Result,
	client *http.Client,
	userAgents []string,
	rateLimit <-chan time.Time,
	delay time.Duration,
	retries int,
	seen *sync.Map,
	seed int64,
	wg *sync.WaitGroup,
) {
	defer wg.Done()
	source := rand.NewSource(seed)
	r := rand.New(source)
	uaLen := len(userAgents)
	uaIndex := 0
	for range jobs {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if rateLimit != nil {
			select {
			case <-rateLimit:
			case <-ctx.Done():
				return
			}
		}
		username := uniqueUsername(r, mode, seen)
		ua := userAgents[uaIndex%uaLen]
		uaIndex++
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var available bool
		var err error
		for attempt := 0; attempt <= retries; attempt++ {
			if attempt > 0 {
				backoff := time.Duration(1<<uint(attempt-1)) * 100 * time.Millisecond
				jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
				select {
				case <-time.After(backoff + jitter):
				case <-ctx.Done():
					return
				}
			}
			req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, fmt.Sprintf(baseURL, username), nil)
			if err != nil {
				err = fmt.Errorf("creating request: %w", err)
				break
			}
			req.Header.Set("User-Agent", ua)
			resp, err := client.Do(req)
			if err != nil {
				if attempt < retries && ctx.Err() == nil {
					continue
				}
				err = fmt.Errorf("request failed: %w", err)
				break
			}
			defer resp.Body.Close()
			switch resp.StatusCode {
			case http.StatusNotFound:
				available = true
				err = nil
			case http.StatusOK:
				available = false
				err = nil
			case http.StatusTooManyRequests, http.StatusInternalServerError,
				http.StatusBadGateway, http.StatusServiceUnavailable:
				if attempt < retries && ctx.Err() == nil {
					continue
				}
				err = fmt.Errorf("temporary status %d", resp.StatusCode)
			default:
				err = fmt.Errorf("unexpected status %d", resp.StatusCode)
			}
			break
		}
		results <- Result{
			Username:  username,
			Available: available,
			Error:     err,
		}
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			}
		}
	}
}

func saveResults(
	results <-chan Result,
	outputFile string,
	quiet bool,
	stats *Stats,
	start time.Time,
) error {
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed creating output file: %w", err)
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	defer writer.Flush()
	for result := range results {
		stats.AddChecked()

		if result.Error != nil {
			stats.AddError()
			if !quiet {
				fmt.Printf("[error] %s: %v\n", result.Username, result.Error)
			}
		} else if result.Available {
			stats.AddAvailable()
			if !quiet {
				fmt.Printf("[available] %s\n", result.Username)
			}
			if _, err := writer.WriteString(result.Username + "\n"); err != nil {
				fmt.Printf("[write error] %s: %v\n", result.Username, err)
				stats.AddError()
				continue
			}
		} else {
			stats.AddTaken()
			if !quiet {
				fmt.Printf("[taken] %s\n", result.Username)
			}
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("\n====== Summary ======\n")
	fmt.Printf("Total checked : %d\n", stats.Checked)
	fmt.Printf("Available     : %d\n", stats.Available)
	fmt.Printf("Taken         : %d\n", stats.Taken)
	fmt.Printf("Errors        : %d\n", stats.Errors)
	fmt.Printf("Elapsed time  : %s\n", elapsed.Round(time.Millisecond))
	if stats.Checked > 0 {
		fmt.Printf("Rate          : %.2f checks/sec\n", float64(stats.Checked)/elapsed.Seconds())
	}
	fmt.Printf("Available saved to %s\n", outputFile)
	return nil
}

func main() {
	mode := flag.String("mode", "letters", "username character set: 'letters' (a-z) or 'alnum' (a-z0-9)")
	total := flag.Int("total", 100, "total number of usernames to check")
	threads := flag.Int("threads", 10, "number of concurrent workers")
	output := flag.String("output", "available.txt", "file to save available usernames")
	timeout := flag.Duration("timeout", 30*time.Second, "overall timeout for the entire operation")
	uaFile := flag.String("ua-file", "", "file with user agents (one per line); if empty, built-in list used")
	delay := flag.Duration("delay", 0, "minimum delay between requests per worker (e.g., 100ms)")
	retries := flag.Int("retries", 2, "number of retries on temporary errors (429, 5xx)")
	proxy := flag.String("proxy", "", "proxy URL (e.g., http://user:pass@host:port)")
	quiet := flag.Bool("quiet", false, "suppress per‑username output, only print summary")
	flag.Parse()

	if *mode != "letters" && *mode != "alnum" {
		fmt.Fprintf(os.Stderr, "Invalid mode '%s'. Must be 'letters' or 'alnum'.\n", *mode)
		os.Exit(1)
	}

	userAgents, err := loadUserAgents(*uaFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading user agents: %v\n", err)
		os.Exit(1)
	}

	client, err := newHTTPClient(5*time.Second, *proxy)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating HTTP client: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	jobs := make(chan int, *threads)
	results := make(chan Result, *threads)
	seen := &sync.Map{}
	stats := &Stats{Start: time.Now()}
	var wg sync.WaitGroup
	for i := 0; i < *threads; i++ {
		wg.Add(1)
		seed := time.Now().UnixNano() + int64(i)*1000 + rand.Int63n(1000)
		go workerWithSeed(
			ctx,
			*mode,
			jobs,
			results,
			client,
			userAgents,
			nil,
			*delay,
			*retries,
			seen,
			seed,
			&wg,
		)
	}
	go func() {
		defer close(jobs)
		for i := 0; i < *total; i++ {
			select {
			case jobs <- i:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()
	if err := saveResults(results, *output, *quiet, stats, stats.Start); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving results: %v\n", err)
		os.Exit(1)
	}
}
