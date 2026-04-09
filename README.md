> # a fast, concurrent username availability checker written in Go. this tool generates random usernames and checks whether they are available on a specified website (currently for GitHub).

---

## features

- concurrent checking using goroutines  
- supports:
- `4l` → 4-letter usernames (a–z)
- `4c` → 4-character usernames (a–z, 0–9)
- easily configurable to check any website  
- multi-threaded for high performance  
- collects and displays available usernames  

---

## how it works

1. generates random usernames  
2. sends HTTP requests to check if the username exists  
3. interprets responses:
   - `404` → ✅ Available  
   - Other → ❌ Taken  
4. stores available usernames in memory  
5. prints results at the end  

---

## installation

make sure u got Go installed (1.18+ recommended).

```bash
git clone https://github.com/voec/username-availability-checker
cd username-availability-checker
go run main.go
