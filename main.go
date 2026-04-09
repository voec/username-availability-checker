package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	availableUsernames []string
	lock               sync.Mutex
)

func generate4l() string {
	letters := "abcdefghijklmnopqrstuvwxyz"
	sb := strings.Builder{}
	for i := 0; i < 4; i++ {
		sb.WriteByte(letters[rand.Intn(len(letters))])
	}
	return sb.String()
}

func generate4c() string {
	chars := "abcdefghijklmnopqrstuvwxyz0123456789"
	sb := strings.Builder{}
	for i := 0; i < 4; i++ {
		sb.WriteByte(chars[rand.Intn(len(chars))])
	}
	return sb.String()
}

func checkYoutube(username string) {
	url := fmt.Sprintf("https://www.github.com/%s", username) // change the site to be whatever u want
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("[wrong] %s\n", username)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		result := fmt.Sprintf("[available] %s", username)
		fmt.Println(result)
		lock.Lock()
		availableUsernames = append(availableUsernames, username)
		lock.Unlock()
	} else {
		fmt.Printf("[taken] %s\n", username)
	}
}

func worker(mode string, count int, wg *sync.WaitGroup) {
	defer wg.Done()
	for i := 0; i < count; i++ {
		var username string
		if mode == "4l" {
			username = generate4l()
		} else {
			username = generate4c()
		}
		checkYoutube(username)
	}
}

func main() {
	rand.Seed(time.Now().UnixNano())

	mode := "4l"
	totalChecks := 100
	threadCount := 50
	checksPerThread := totalChecks / threadCount

	var wg sync.WaitGroup
	for i := 0; i < threadCount; i++ {
		wg.Add(1)
		go worker(mode, checksPerThread, &wg)
	}
	wg.Wait()

	fmt.Println("Available usernames")
	for _, name := range availableUsernames {
		fmt.Println(name)
	}
}
