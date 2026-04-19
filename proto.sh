#!/bin/bash

set -eou pipefail

shopt -s globstar

find_service_proto_files() {
  # Only our first-party .proto files — the vendored googleapis +
  # protoc-gen-openapiv2 imports live under proto/ too but we don't want
  # to generate Go stubs for them.
  find "${PROTO_DIR}/playtesthub" -name "*.proto" -type f
}

PROTO_DIR="${1:-proto}"
OUT_DIR="${2:-pkg/pb}"
APIDOCS_DIR="${3:-gateway/apidocs}"

# Clean previously generated files.
rm -rf "${OUT_DIR:?}"/* && \
  mkdir -p "${OUT_DIR:?}"

# Clean previously generated swagger files.
rm -rf "${APIDOCS_DIR:?}"/* && \
  mkdir -p "${APIDOCS_DIR}"

# Step 1: Generate Go code for first-party proto files.
protoc \
  -I "${PROTO_DIR}" \
  --go_out="${OUT_DIR}" \
  --go_opt=paths=source_relative \
  --go-grpc_out="${OUT_DIR}" \
  --go-grpc_opt=paths=source_relative,require_unimplemented_servers=false \
  --grpc-gateway_out=logtostderr=true:"${OUT_DIR}" \
  --grpc-gateway_opt=paths=source_relative \
  $(find_service_proto_files)

# Step 2: Generate OpenAPI/Swagger for the service definition. Emit a
# single merged `api.swagger.json` at the top of APIDOCS_DIR so
# main.go's glob in serveSwaggerJSON finds exactly one file regardless
# of how many .proto files we add.
protoc \
  -I "${PROTO_DIR}" \
  --openapiv2_out "${APIDOCS_DIR}" \
  --openapiv2_opt=logtostderr=true,allow_merge=true,merge_file_name=api \
  "${PROTO_DIR}"/playtesthub/v1/playtesthub.proto
