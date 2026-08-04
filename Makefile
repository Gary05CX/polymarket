.PHONY: build run test tidy clean

build:
	go build -o bot ./cmd/bot

run: build
	./bot

test:
	go test ./...

tidy:
	go mod tidy

clean:
	rm -f bot bot.exe
	rm -rf data/*.duckdb data/*.wal
