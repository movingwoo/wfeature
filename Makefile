.PHONY: debug release run run-release serve serve-release server server-release test test-debug dist

# Each profile owns its own output directory so a debug and a release build can
# coexist. Rebuilding one never silently replaces the other, and which binary
# ran is visible from its path.
DEBUG_BIN := build/debug/wfeature
RELEASE_BIN := build/release/wfeature
DEBUG_SERVER := build/debug/wfeature-server
RELEASE_SERVER := build/release/wfeature-server

# A release is one archive per operating system: the release server binary, a
# launcher, and the empty games/ tree the binary looks for beside itself. The
# version is stamped into the binary and into the archive names, and is set by
# hand — `make dist VERSION=0.2.0` — because a tag is a decision, not a build
# artefact.
VERSION ?= 0.2.0
DIST := build/dist
DIST_PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64

debug:
	mkdir -p $(dir $(DEBUG_BIN))
	go build -tags debug -o $(DEBUG_BIN) ./cmd/cli

release:
	mkdir -p $(dir $(RELEASE_BIN))
	go build -trimpath -ldflags="-s -w" -o $(RELEASE_BIN) ./cmd/cli

# The server carries the client files inside it, so these two binaries are the
# whole deliverable: drop one next to a games/ directory and leave it running.
server:
	mkdir -p $(dir $(DEBUG_SERVER))
	go build -tags debug -o $(DEBUG_SERVER) ./cmd/server

server-release:
	mkdir -p $(dir $(RELEASE_SERVER))
	go build -trimpath -ldflags="-s -w" -o $(RELEASE_SERVER) ./cmd/server

# dist builds the downloadable release: one archive per platform holding the
# server, its launcher and the empty games/ tree.
#
# Windows gets a .zip because that is what Explorer opens without a tool, and
# its text is converted while it is staged: CRLF because cmd.exe parses .bat
# line by line and the READMEs open in whatever editor a first-time user has,
# plus a UTF-8 BOM on the Korean ones so an editor that would otherwise guess
# CP949 cannot render them as mojibake. The .bat stays ASCII and BOM-free —
# cmd.exe would echo a BOM. The sources under packaging/ stay LF.
#
# COPYFILE_DISABLE keeps macOS's bsdtar from writing ._ AppleDouble companions
# into an archive that Linux users would then have to look at.
dist:
	rm -rf $(DIST)
	@for platform in $(DIST_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		name=wfeature-$(VERSION)-$$os-$$arch; \
		stage=$(DIST)/$$name; \
		binary=wfeature-server; source=packaging/linux; \
		scripts="start.sh stop.sh status.sh"; \
		case $$os in \
			windows) binary=wfeature-server.exe; source=packaging/windows; \
				scripts="start.bat stop.bat status.bat";; \
			darwin) source=packaging/macos; \
				scripts="start.command stop.command status.command";; \
		esac; \
		echo "  $$name"; \
		mkdir -p $$stage/games/ktf $$stage/games/lgt $$stage/games/skt; \
		GOOS=$$os GOARCH=$$arch go build -trimpath \
			-ldflags="-s -w -X main.version=$(VERSION)" \
			-o $$stage/$$binary ./cmd/server || exit 1; \
		cp LICENSE $$stage/LICENSE.txt; \
		cp internal/licenses/THIRD-PARTY-NOTICES.md $$stage/; \
		if [ $$os = windows ]; then \
			for script in $$scripts; do \
				sed -e 's/$$/\r/' $$source/$$script > $$stage/$$script; \
			done; \
			printf '\357\273\277' > $$stage/README.txt; \
			sed -e 's/$$/\r/' $$source/README.txt >> $$stage/README.txt; \
			printf '\357\273\277' > $$stage/games/README.txt; \
			sed -e 's/$$/\r/' packaging/games/README.txt >> $$stage/games/README.txt; \
			(cd $(DIST) && zip -qr $$name.zip $$name) || exit 1; \
		else \
			for script in $$scripts; do \
				cp $$source/$$script $$stage/; \
				chmod +x $$stage/$$script; \
			done; \
			cp $$source/README.txt $$stage/; \
			cp packaging/games/README.txt $$stage/games/; \
			COPYFILE_DISABLE=1 tar -czf $(DIST)/$$name.tar.gz -C $(DIST) $$name || exit 1; \
		fi; \
		rm -rf $$stage; \
	done
	@cd $(DIST) && if command -v sha256sum >/dev/null 2>&1; then \
		sha256sum *.tar.gz *.zip > SHA256SUMS; \
	else \
		shasum -a 256 *.tar.gz *.zip > SHA256SUMS; \
	fi
	@ls -l $(DIST)

# run/run-release forward their arguments: `make run ARGS="runktf game.zip -play"`.
run: debug
	./$(DEBUG_BIN) $(ARGS)

run-release: release
	./$(RELEASE_BIN) $(ARGS)

# serve starts the local server, which carries the client files and runs the
# emulator. The server is built per profile like every other binary here, so
# which profile it serves is the binary that is running rather than a flag.
# The same two commands run by hand on any OS; see docs/running.md.
serve:
	go run -tags debug ./cmd/server

serve-release:
	go run ./cmd/server

test:
	go test ./...
	node --test web/*.test.mjs

test-debug:
	go test -tags debug ./...
