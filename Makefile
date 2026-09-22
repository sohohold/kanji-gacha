.PHONY: web run test build dict
web:
	cd web && pnpm install --frozen-lockfile && pnpm run build
run:
	go run ./cmd/api
build: web
	CGO_ENABLED=0 go build -trimpath -o bin/kanji-gacha ./cmd/api
test:
	go test -race ./...
	cd web && pnpm run test -- src
# Required: KANJIVG=/path/to/kanjivg.zip KANJIDIC=/path/to/kanjidic2.xml.gz
dict:
	go run ./cmd/builddict -kanjivg "$(KANJIVG)" -kanjidic "$(KANJIDIC)"
