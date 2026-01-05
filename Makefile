generate:
	go generate -tags tools ./...

go-lint: generate
ifeq (, $(shell which golangci-lint))
	$(error golangci-lint binary is not found in path; install it from https://golangci-lint.run/usage/install/)
endif
	golangci-lint run --timeout=5m

lint: go-lint

test: generate
	mkdir -p bin
	go test -parallel 10 -coverprofile=bin/coverage.out -covermode count $$(go list ./pkg/business/... | grep -v fakeprovider)
	go run github.com/boumenot/gocover-cobertura -by-files -ignore-files '^.+_mock\.go$$' < bin/coverage.out > bin/coverage.xml

run: generate
	go run main.go

infra-start:
	docker compose -f ../infra/local/docker-compose.yaml up -d
	sleep 20 # find a better way to wait for the containers to be ready

infra-stop:
	docker compose -f ../infra/local/docker-compose.yaml down

e2e-tests:
	go test -timeout 30m -parallel 10 -v ./e2e/...

verify: lint test

assemble-e2e: generate
	go test -o bin/e2e-tests -c ./e2e/...

assemble: generate
	go build -o bin/app main.go

assemble-multiplatform: generate
assemble-multiplatform:
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-w -s -buildid=" -trimpath -o bin/app-linux-arm64 main.go
	env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-w -s -buildid=" -trimpath -o bin/app-linux-amd64 main.go

build: assemble verify

mod:
	go mod tidy
	go mod verify

mod-upgrade:
	go get -u

clean-generated:
	rm -rf api/*/api.gen.go
	rm -rf bin/
	rm -rf mockgen/externalmocks
	find pkg/business -name '*_mock.go' -exec rm {} +
