.PHONY: debug release run run-release serve serve-release server server-release test test-debug dist dist-check acceptance mobile checksums pgo

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
# hand — `make dist VERSION=0.3.1` — because a tag is a decision, not a build
# artefact.
VERSION ?= 0.4.0
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

# Profile-guided optimisation. `cmd/*/default.pgo` is picked up by `go build`
# without a flag, so every way this repository is built — the targets here,
# `go run ./cmd/server`, `npm run serve:release`, a bare `go build` — gets it.
# That is why the profile is a committed file rather than a flag: a flag would
# be a second place the build could be wrong, and the paths above do not read
# this Makefile at all.
#
# Measured on one machine (Ryzen 5 3600, linux/amd64), same route and save,
# frames byte-identical: an LGT Clet's in-game busy time fell 14.5% and its
# tick p90 15.0%; a KTF title fell 7.0%. See docs/armcore.md.
#
# `make pgo` regenerates it. **It needs local archives and routes**, which are
# under var/ and are not in the repository, so it runs only on a machine that
# has them — and it is told which ones on the command line rather than here,
# because the files are one person's copies and their names are the games'.
# The three runs are not arbitrary: they are the three paths through the same
# engine — a Clet is 99.8% Thumb, an AOT-compiled LGT Java title is 78.9% ARM,
# and KTF is a third mix — and a profile of any one of them alone leaves the
# other two at the compiler's guess. All five have to be given:
#
#   make pgo PGO_LGT_CLET=var/games/lgt/<clet>.zip \
#            PGO_LGT_CLET_ROUTE=var/routes/<clet-in-game>.route \
#            PGO_LGT_CLET_SAVE=var/savedata/release/lgt/<PID> \
#            PGO_LGT_JAVA=var/games/lgt/<aot-title>.zip \
#            PGO_LGT_JAVA_ROUTE=var/routes/<aot-title-in-game>.route \
#            PGO_LGT_JAVA_SAVE=var/savedata/release/lgt/<PID> \
#            PGO_KTF=var/games/ktf/<title>.zip PGO_KTF_TICKS=2000
#
# See docs/armcore.md for what each run is for and what the committed profile
# was taken from.
PGO_LGT_CLET       ?=
PGO_LGT_CLET_ROUTE ?=
PGO_LGT_JAVA       ?=
PGO_LGT_JAVA_ROUTE ?=
PGO_KTF            ?=
# The two save trees are optional and are the difference between profiling a
# game and profiling a title screen. The scene worth compiling for is inside a
# save — a field, a battle — and the same route replayed from a fresh boot
# stops at the title screen having profiled nothing. Each is the directory the
# title's saves live in (`var/savedata/<profile>/lgt/<PID>`); it is copied into
# the work tree first, because the run plays and a profile taken against the
# save it is changing is a different profile every time.
PGO_LGT_CLET_SAVE  ?=
PGO_LGT_JAVA_SAVE  ?=
# How long each run is profiled for. The defaults suit a title that spends most
# of a run idle; a heavy one reaches the same amount of profile in far fewer
# ticks, and profiling it for 40,000 would take an hour.
PGO_LGT_TICKS      ?= 20000
PGO_KTF_TICKS      ?= 40000
PGO_WORK := build/pgo

pgo:
	@for pair in "PGO_LGT_CLET=$(PGO_LGT_CLET)" "PGO_LGT_CLET_ROUTE=$(PGO_LGT_CLET_ROUTE)" \
		"PGO_LGT_JAVA=$(PGO_LGT_JAVA)" "PGO_LGT_JAVA_ROUTE=$(PGO_LGT_JAVA_ROUTE)" \
		"PGO_KTF=$(PGO_KTF)"; do \
		name=$${pair%%=*}; file=$${pair#*=}; \
		[ -n "$$file" ] || { echo "make pgo: $$name is not set; see docs/armcore.md" >&2; exit 1; }; \
		[ -f "$$file" ] || { echo "make pgo: $$file ($$name) is missing; see docs/armcore.md" >&2; exit 1; }; \
	done
	rm -rf $(PGO_WORK)
	mkdir -p $(PGO_WORK)/save1 $(PGO_WORK)/save2
	@if [ -n "$(PGO_LGT_CLET_SAVE)" ]; then cp -R "$(PGO_LGT_CLET_SAVE)/." $(PGO_WORK)/save1/; fi
	@if [ -n "$(PGO_LGT_JAVA_SAVE)" ]; then cp -R "$(PGO_LGT_JAVA_SAVE)/." $(PGO_WORK)/save2/; fi
	WFEATURE_PERF_ARCHIVE="$(CURDIR)/$(PGO_LGT_CLET)" \
	WFEATURE_PERF_ROUTE="$(CURDIR)/$(PGO_LGT_CLET_ROUTE)" \
	WFEATURE_SAVE_ROOT="$(CURDIR)/$(PGO_WORK)/save1" WFEATURE_LOAD_TICKS=$(PGO_LGT_TICKS) \
		go test ./internal/platform/lgt -run LGTLoadCost \
			-cpuprofile $(PGO_WORK)/lgt-clet.prof -o $(PGO_WORK)/lgt.test -timeout 30m
	WFEATURE_PERF_ARCHIVE="$(CURDIR)/$(PGO_LGT_JAVA)" \
	WFEATURE_PERF_ROUTE="$(CURDIR)/$(PGO_LGT_JAVA_ROUTE)" \
	WFEATURE_SAVE_ROOT="$(CURDIR)/$(PGO_WORK)/save2" WFEATURE_LOAD_TICKS=$(PGO_LGT_TICKS) \
		go test ./internal/platform/lgt -run LGTLoadCost \
			-cpuprofile $(PGO_WORK)/lgt-java.prof -timeout 30m
	WFEATURE_PERF_ARCHIVE="$(CURDIR)/$(PGO_KTF)" WFEATURE_LOAD_TICKS=$(PGO_KTF_TICKS) \
		go test ./internal/platform/ktf -run TestLoadCostProbe \
			-cpuprofile $(PGO_WORK)/ktf.prof -o $(PGO_WORK)/ktf.test -timeout 30m
	@# Each profile is symbolised against the binary that produced it before the
	@# three are merged, because a merge cannot symbolise two different binaries.
	go tool pprof -proto $(PGO_WORK)/lgt.test $(PGO_WORK)/lgt-clet.prof > $(PGO_WORK)/clet.pb.gz
	go tool pprof -proto $(PGO_WORK)/lgt.test $(PGO_WORK)/lgt-java.prof > $(PGO_WORK)/java.pb.gz
	go tool pprof -proto $(PGO_WORK)/ktf.test $(PGO_WORK)/ktf.prof      > $(PGO_WORK)/ktf.pb.gz
	go tool pprof -proto $(PGO_WORK)/clet.pb.gz $(PGO_WORK)/java.pb.gz $(PGO_WORK)/ktf.pb.gz \
		> $(PGO_WORK)/default.pgo
	cp $(PGO_WORK)/default.pgo cmd/server/default.pgo
	cp $(PGO_WORK)/default.pgo cmd/cli/default.pgo
	@echo
	@echo "wrote cmd/server/default.pgo and cmd/cli/default.pgo"
	@echo "the two are one file in two places; rebuild and re-measure before committing them."

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
	@$(MAKE) --no-print-directory checksums
	@ls -l $(DIST)

# dist-check reads the archives back. `make dist` says the files were written;
# this says what is in them — that every entry extracts inside its own folder,
# that the launchers kept their executable bit, that the Windows text kept the
# line endings and byte order mark it is converted for, that the licence and
# the notices are the ones in this repository, and that the server built for
# this machine answers with the version that was stamped into it. All of that
# was a manual check before a tag; the release workflow runs this command now.
dist-check:
	go run ./internal/tools/distcheck -dir $(DIST)

# The phone builds. They are not part of `dist` and not in the release archive:
# each needs a toolchain the desktop build does not (an Android SDK, and Xcode
# for the iOS one), and a machine that has neither should still be able to cut
# a release. Run this where the toolchains are, and the artifacts land beside
# the desktop archives to be published with them.
mobile:
	@mkdir -p $(DIST)
	VERSION=$(VERSION) mobile/android/build.sh
	cp mobile/android/build/wfeature.apk $(DIST)/wfeature-$(VERSION)-android-arm64.apk
	VERSION=$(VERSION) mobile/ios/build.sh
	cp mobile/ios/build/wfeature.ipa $(DIST)/wfeature-$(VERSION)-ios-arm64.ipa
	@$(MAKE) --no-print-directory checksums
	@ls -l $(DIST)

# Every archive in the release directory, whichever command wrote it. The list
# is gathered first because most releases have no phone builds beside the
# desktop ones, and a pattern that matches nothing is an error rather than an
# empty list.
checksums:
	@cd $(DIST) && files=`ls *.tar.gz *.zip *.apk *.ipa 2>/dev/null`; \
	if command -v sha256sum >/dev/null 2>&1; then \
		sha256sum $$files > SHA256SUMS; \
	else \
		shasum -a 256 $$files > SHA256SUMS; \
	fi

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

# acceptance runs every local archive probe — six for KTF, one for LGT, two for
# SKT — and writes what they answered to var/acceptance/<date>.md. It needs the
# ignored local corpus under var/games and runs nowhere else, which is why it
# is not part of `make test`: no archive in this repository is a real game.
#
# The report is where a count belongs. A number typed into a document is a
# sentence about one afternoon, and the corpus changes underneath it; the file
# carries its date and the archive that produced every row. It stays under
# var/ because those rows are the games' names.
acceptance:
	go run ./internal/tools/acceptance $(ARGS)

test-debug:
	go test -tags debug ./...
