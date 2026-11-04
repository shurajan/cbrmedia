# Цели для всех Go-модулей сразу: make test, make check, make build …
MODULES := core recorder tbot api converter
TARGETS := build test lint vet vuln check tidy clean

.PHONY: $(TARGETS)
$(TARGETS):
	@set -e; for m in $(MODULES); do echo "== $$m: $@"; $(MAKE) -s -C $$m $@; done
