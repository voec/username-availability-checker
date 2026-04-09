> # fast and concurrent username availability checker written in Go.  
this tool generates random usernames and checks whether they are available on a specified website (e.g. GitHub, soundcloud, etc.).

---

## features

- high-performance concurrent checking using goroutines  
- supports multiple generation modes:
  - `4l` → 4-letter usernames (a–z)  
  - `4c` → 4-character usernames (a–z, 0–9)  
- easily configurable target website  
- multi-threaded for efficient large-scale checks  
- real-time detection of available usernames  
- automatically saves available usernames to a `.txt` file  
- thread-safe file writing (safe under concurrency)  

---

## how it works

1. generates random usernames based on selected mode  
2. sends HTTP requests to the target website  
3. interprets responses:
   - `404` → ✅ Available  
   - Other status → ❌ Taken  
4. immediately writes available usernames to `available.txt`  
5. continues processing concurrently for maximum performance  

---

## output

- available usernames are:
  - printed in the console  
  - saved in `available.txt` (one per line)  

### example

- [available] abcd
- [available] x9k2
- [taken] test

---

## installation

make sure u have Go installed (1.18+ recommended).

```bash
git clone https://github.com/voec/username-availability-checker
cd username-availability-checker
go run main.go
```

---

## configuration

you can edit the variables inside `main.go` to customize how the checker behaves:

```go
const baseURL = "https://github.com/%s" 

func main() {
	mode := "4l"
	totalChecks := 100
	threadCount := 10
}
```
---

| Variable      | Description                                                                                       | Example Values                                                          |
| ------------- | ------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `baseURL`     | website URL format used to check usernames. `%s` is replaced with the generated username.         | `"https://github.com/%s"`<br>`"https://soundcloud.com/%s"`              |
| `mode`        | username generation mode.                                                                         | `"4l"` = 4 lowercase letters<br>`"4c"` = 4 lowercase letters or numbers |
| `totalChecks` | total number of usernames to generate and check.                                                  | `100`, `1000`, `50000`                                                  |
| `threadCount` | number of concurrent worker goroutines. higher values increase speed and may trigger rate limits. | `10`, `50`, `100`                                                       |

---

## example

```
const baseURL = "https://soundcloud.com/%s"
func main() {
	mode := "4c"
	totalChecks := 5000
	threadCount := 50
}
```

this will:

- generate random 4-character usernames
- check 5,000 usernames
- use 50 concurrent workers
- test availability on SoundCloud
