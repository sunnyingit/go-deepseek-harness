# Electron client (native/). From this directory or via the root Makefile:
#   make mac          macOS .dmg / .zip  (bundles Python + Node + web)
#   make win          Windows .zip
#   make OS=mac|win   same, via OS=

OS ?=
NPM ?= npm
BUILDER = npx electron-builder
PREPARE = ./scripts/prepare-runtime.sh
# 1 = delete .tar.gz/.zip after extract. Default keeps downloads for reuse.
PRUNE_ARCHIVES ?= 0

DARWIN_ARCH := $(shell uname -m)
ifeq ($(DARWIN_ARCH),arm64)
MAC_TARGET := darwin-arm64
else
MAC_TARGET := darwin-x64
endif

.PHONY: help deps start mac win runtime-mac runtime-win clean

help:
	@echo "Build the PABuddy Electron client"
	@echo ""
	@echo "  make mac           macOS .zip with bundled Python/Node/web -> dist/"
	@echo "  make win           Windows .zip with win-x64 runtimes           -> dist/"
	@echo "  make OS=mac|win    same as the named target"
	@echo "  make start         unpackaged; uses ./runtime if present else go run"
	@echo "  make runtime-mac   download/build native/runtime for this Mac"
	@echo "  make runtime-win   download/build native/runtime for Windows"
	@echo "  make clean         remove dist/, runtime binaries, node_modules/"
	@echo "  PRUNE_ARCHIVES=1   optional: delete downloaded .tar.gz/.zip after extract"

deps:
	$(NPM) install

runtime-mac:
	PRUNE_ARCHIVES=$(PRUNE_ARCHIVES) $(PREPARE) $(MAC_TARGET)
	test -x runtime/bin/pabuddy-web
	test -x runtime/python/bin/python3
	test -x runtime/node/bin/node
	test -d runtime/node-packages/node_modules/pptxgenjs
	test -d runtime/node-packages/node_modules/react-icons
	test -d runtime/node-packages/node_modules/react-dom
	test -d runtime/node-packages/node_modules/sharp

runtime-win:
	PRUNE_ARCHIVES=$(PRUNE_ARCHIVES) $(PREPARE) win-x64
	test -f runtime/bin/pabuddy-web.exe
	test -f runtime/python/python.exe
	test -f runtime/node/node.exe
	test -d runtime/node-packages/node_modules/pptxgenjs
	test -d runtime/node-packages/node_modules/sharp

start: deps
	@if [ ! -x runtime/bin/pabuddy-web ]; then $(MAKE) runtime-mac; fi
	$(NPM) start

mac: deps runtime-mac
	$(BUILDER) --mac

win: deps runtime-win
	$(BUILDER) --win

clean:
	rm -rf dist node_modules
	rm -rf runtime/bin runtime/python runtime/node runtime/git runtime/wheels runtime/node-packages runtime/skills runtime/config.yaml
	rm -f runtime/*.tar.gz runtime/*.zip runtime/cpython-*

ifneq ($(OS),)
ifeq ($(OS),mac)
.DEFAULT_GOAL := mac
else ifneq ($(filter win windows,$(OS)),)
.DEFAULT_GOAL := win
else
$(error OS must be mac or win, got '$(OS)')
endif
else
.DEFAULT_GOAL := help
endif
