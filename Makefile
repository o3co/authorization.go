.PHONY: install
install: generate

# --- variables (defaults can be overridden by environment) ---
# project working directory (defaults to current directory)
WORKDIR ?= $(CURDIR)
# directory for generated artifacts
TARGET_DIR ?= generated
# Go-specific generated output directory
GO_TARGET_DIR ?= $(TARGET_DIR)/
# proto source directory
SCHEMA_DIR ?= $(WORKDIR)/schema
# proto files to generate from
SCHEMA_RELS ?= $(wildcard $(SCHEMA_DIR)/*.proto)

.PHONY: clean
clean:
	rm -rf $(TARGET_DIR)

.PHONY: generate
generate: clean generated/go

.PHONY: generated/go
generated/go:
	@test -n "$(SCHEMA_RELS)" || (echo "No service protos found under schema/*.proto"; exit 1)
	@mkdir -p $(GO_TARGET_DIR)
	@protoc \
		-I $(WORKDIR) \
	  --go_out=paths=source_relative:$(GO_TARGET_DIR) \
	  --go-grpc_out=paths=source_relative:$(GO_TARGET_DIR) \
	  $(SCHEMA_RELS)