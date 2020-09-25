#!/usr/bin/env bash
set -o nounset
set -o errexit
set -o pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
TOOLS_DIR=${REPO_ROOT}/hack/tools
TOOLS_BIN=${TOOLS_DIR}/bin

# build tools
cd "${TOOLS_DIR}"
go build -o "bin/conversion-gen" k8s.io/code-generator/cmd/conversion-gen
cd "${REPO_ROOT}"

# run generators
"${TOOLS_BIN}/conversion-gen" -i ./apis/bootstrap/v1alpha1 -o . -O zz_generated.conversion --go-header-file hack/boilerplate.go.txt
"${TOOLS_BIN}/conversion-gen" -i ./apis/controlplane/v1alpha1 -o . -O zz_generated.conversion --go-header-file hack/boilerplate.go.txt

# gofmt the tree
find . -name "*.go" -type f -print0 | xargs -0 gofmt -s -w
