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
