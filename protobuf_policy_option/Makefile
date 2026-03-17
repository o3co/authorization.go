# --- variables (defaults can be overridden by environment) ---
# project working directory (defaults to current directory)
WORKDIR ?= $(CURDIR)
# directory for generated artifacts (same as proto source)
TARGET_DIR ?= $(CURDIR)/schema
# Go-specific generated output directory
GO_TARGET_DIR ?= $(TARGET_DIR)/
# proto source directory
SCHEMA_DIR ?= $(CURDIR)/schema
# proto files to generate from
SCHEMA_RELS ?= $(wildcard $(SCHEMA_DIR)/*.proto)

.PHONY: generate
generate:
	@test -n "$(SCHEMA_RELS)" || (echo "No service protos found under schema/*.proto"; exit 1)
	@mkdir -p $(SCHEMA_DIR)
	@protoc -I $(SCHEMA_DIR) --go_out=paths=source_relative:$(SCHEMA_DIR) --go-grpc_out=paths=source_relative:$(SCHEMA_DIR) $(SCHEMA_RELS)