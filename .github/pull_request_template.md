## What and why

<!-- What does this change, and what problem does it solve? -->

## Checklist

- [ ] Every fix has a test that fails without it
- [ ] `gofmt`, `go vet` and `go test -race ./...` pass (CI runs them too)
- [ ] `./hody eval eval/scenarios` still passes 12 of 12
- [ ] Hody stays read-only, and no evidence text reaches the model outside the gateway
- [ ] Docs updated (README, docs/, CHANGELOG.md) where behaviour changed
- [ ] No real secrets in code, tests or fixtures (scenarios use fake canaries)
