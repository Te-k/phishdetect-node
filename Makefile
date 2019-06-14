BUILD_FOLDER  = $(shell pwd)/build

FLAGS_LINUX   = GOOS=linux GOARCH=amd64 CGO_ENABLED=0
FLAGS_DARWIN  = GOOS=darwin GOARCH=amd64 CGO_ENABLED=0
FLAGS_WINDOWS = GOOS=windows GOARCH=386 CC=i686-w64-mingw32-gcc CGO_ENABLED=1

lint:
	@echo "[lint] Running linter on codebase"
	@golint ./...

deps:
	@echo "[deps] Downloading modules..."
	go mod download
	go get -u github.com/gobuffalo/packr/...

	#@echo "[deps] Need to fix an issue with Docker client vendoring..."
	#rm -rf $(GOPATH)/src/github.com/docker/docker/vendor/github.com/docker/go-connections

	@echo "[deps] Done!"

linux:
	@mkdir -p $(BUILD_FOLDER)/linux

	@echo "[builder] Building Linux Web executable"
	$(FLAGS_LINUX) packr build --ldflags '-s -w -extldflags "-static"' -o $(BUILD_FOLDER)/linux/phishdetect-node

	@echo "[builder] Done!"

darwin:
	@mkdir -p $(BUILD_FOLDER)/darwin

	@echo "[builder] Building Darwin Web executable"
	$(FLAGS_DARWIN) packr build --ldflags '-s -w -extldflags "-static"' -o $(BUILD_FOLDER)/darwin/phishdetect-node

	@echo "[builder] Done!"

windows:
	@mkdir -p $(BUILD_FOLDER)/windows

	@echo "[builder] Building Windows Web executable"
	$(FLAGS_WINDOWS) packr build --ldflags '-s -w -extldflags "-static"' -o $(BUILD_FOLDER)/windows/phishdetect-node.exe

	@echo "[builder] Done!"

clean:
	rm -rf $(BUILD_FOLDER)
