# Electron client in native/
#   make mac
#   make win
#   make OS=mac|win

.PHONY: help mac win start clean

help:
	@$(MAKE) -C native help

mac win start clean:
	@$(MAKE) -C native $@

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
