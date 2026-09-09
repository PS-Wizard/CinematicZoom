PORT ?= 7788

.PHONY: run build test down

run:
	go run ./cmd/cinematic

build:
	go build -o cinematic ./cmd/cinematic

test:
	go test ./...

down:
	@pids=$$(ss -H -lptn "sport = :$(PORT)" | grep -oE 'pid=[0-9]+' | cut -d= -f2 | sort -u); \
	if [ -n "$$pids" ]; then \
		echo "killing $$pids on :$(PORT)"; \
		kill $$pids; \
	else \
		echo "nothing on :$(PORT)"; \
	fi
	@pkill -x cinematic 2>/dev/null || true
	@pkill -f 'go run ./cmd/cinematic' 2>/dev/null || true
