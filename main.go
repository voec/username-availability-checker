package main

import (
	"bufio"
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	letters = "abcdefghijklmnopqrstuvwxyz"
	chars   = "abcdefghijklmnopqrstuvwxyz0123456789"
	baseURL = "https://soundcloud.com/%s" // change this
)

type Config struct {
	Mode        string
	TotalChecks int
	ThreadCount int
	OutputFile  string
	Timeout     time.Duration
}

type Result struct {
	Username  string
	Available bool
	Error     error
}

func generate(r *rand.Rand, mode string) string {
	pool := letters
	if mode != "4l" {
		pool = chars
	}

	sb := strings.Builder{}
	sb.Grow(4)

	for i := 0; i < 4; i++ {
		sb.WriteByte(pool[r.Intn(len(pool))])
	}
	return sb.String()
}

func checkUsername(ctx context.Context, username string, client *http.Client) (bool, error) {
	url := fmt.Sprintf(baseURL, username)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("failed creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return true, nil
	case http.StatusOK:
		return false, nil
	default:
		return false, fmt.Errorf("idk: %d", resp.StatusCode)
	}
}

func worker(
	ctx context.Context,
	mode string,
	jobs <-chan int,
	results chan<- Result,
	client *http.Client,
	seedChan <-chan int64,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	seed := <-seedChan
	source := rand.NewSource(seed)
	r := rand.New(source)

	for range jobs {
		username := generate(r, mode)

		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		available, err := checkUsername(checkCtx, username, client)
		cancel()

		results <- Result{
			Username:  username,
			Available: available,
			Error:     err,
		}
	}
}

func saveResults(results <-chan Result, outputFile string) error {
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed creating output file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	stats := struct {
		available int
		taken     int
		errors    int
	}{}

	for result := range results {
		if result.Error != nil {
			fmt.Printf("[error] %s: %v\n", result.Username, result.Error)
			stats.errors++
		} else if result.Available {
			fmt.Printf("[available] %s\n", result.Username)
			if _, err := writer.WriteString(result.Username + "\n"); err != nil {
				fmt.Printf("[idk] %s: %v\n", result.Username, err)
				stats.errors++
				continue
			}
			stats.available++
		} else {
			fmt.Printf("[taken] %s\n", result.Username)
			stats.taken++
		}
	}

	fmt.Printf("\nSummary\n")
	fmt.Printf("Available: %d\nTaken: %d\nErrors: %d\n", stats.available, stats.taken, stats.errors)
	fmt.Printf("Saved available usernames to %s\n", outputFile)

	return nil
}

func main() {
	config := Config{
		Mode:        "4l",
		TotalChecks: 100,
		ThreadCount: 10,
		OutputFile:  "available.txt",
		Timeout:     30 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	jobs := make(chan int, config.ThreadCount)
	results := make(chan Result, config.ThreadCount)
	seedChan := make(chan int64, config.ThreadCount)

	go func() {
		for i := 0; i < config.ThreadCount; i++ {
			seedChan <- time.Now().UnixNano() + int64(i)*1000
		}
		close(seedChan)
	}()

	var wg sync.WaitGroup

	for i := 0; i < config.ThreadCount; i++ {
		wg.Add(1)
		go worker(ctx, config.Mode, jobs, results, client, seedChan, &wg)
	}

	go func() {
		for i := 0; i < config.TotalChecks; i++ {
			jobs <- i
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	if err := saveResults(results, config.OutputFile); err != nil {
		fmt.Printf("Error saving results: %v\n", err)
		os.Exit(1)
	}
}
