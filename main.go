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

var (
	lock   sync.Mutex
	client = &http.Client{Timeout: 5 * time.Second}
	writer *bufio.Writer
	file   *os.File
)

const (
	letters = "abcdefghijklmnopqrstuvwxyz"
	chars   = "abcdefghijklmnopqrstuvwxyz0123456789"
	baseURL = "https://soundcloud.com/%s" // change this 
)

func generate(r *rand.Rand, mode string) string {
	var pool string
	if mode == "4l" {
		pool = letters
	} else {
		pool = chars
	}

	sb := strings.Builder{}
	sb.Grow(4)

	for i := 0; i < 4; i++ {
		sb.WriteByte(pool[r.Intn(len(pool))])
	}
	return sb.String()
}

func saveToFile(username string) {
	lock.Lock()
	defer lock.Unlock()

	writer.WriteString(username + "\n")
	writer.Flush() 
}

func checkUsername(ctx context.Context, username string) {
	url := fmt.Sprintf(baseURL, username)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Printf("[error] %s: %v\n", username, err)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[failed] %s\n", username)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		fmt.Printf("[available] %s\n", username)
		saveToFile(username)
	} else {
		fmt.Printf("[taken] %s\n", username)
	}
}

func worker(mode string, jobs <-chan int, wg *sync.WaitGroup) {
	defer wg.Done()

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	for range jobs {
		username := generate(r, mode)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		checkUsername(ctx, username)
		cancel()
	}
}

func main() {
	mode := "4l"
	totalChecks := 100
	threadCount := 10

	var err error
	file, err = os.Create("available.txt")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	writer = bufio.NewWriter(file)
	defer writer.Flush()

	jobs := make(chan int, totalChecks)
	var wg sync.WaitGroup

	for i := 0; i < threadCount; i++ {
		wg.Add(1)
		go worker(mode, jobs, &wg)
	}

	for i := 0; i < totalChecks; i++ {
		jobs <- i
	}
	close(jobs)

	wg.Wait()

	fmt.Println("\nSaved available usernames to available.txt")
}
