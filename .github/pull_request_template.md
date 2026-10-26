**What and why**

**Checklist**
- [ ] `gofmt -l .` is empty, `go vet ./...` and `go test -race ./...` pass
- [ ] New agent/owner: attribution test added, official removal documented (`agents.Owner.Removal`/`Short`), README table updated
- [ ] New artifact: rule + marker in `scan.ArtifactRules`, test, README row saying what regenerates it
- [ ] Removal paths changed: a test in `internal/clean` proves the refusal and the rescue
- [ ] No real paths, usernames or project names in code, tests or docs
