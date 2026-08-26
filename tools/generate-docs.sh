#!/bin/sh
# Generate provider documentation with tfplugindocs, driven by OpenTofu.
#
# tfplugindocs shells out to a `terraform` binary by default, and will download one if
# it cannot find it. Its --providers-schema flag takes a pre-generated schema JSON and
# skips the CLI entirely, which is what lets OpenTofu stand in.
#
# The throwaway config registers the provider as registry.terraform.io/hashicorp/slack
# rather than its real address. `tofu providers schema -json` keys its output by source
# address, and that is the key tfplugindocs looks up for `-provider-name slack`; using
# the real address would mean rewriting the JSON afterwards.
#
# Run via `go generate -tags tools ./tools` from the repository root.

set -eu

command -v tofu >/dev/null 2>&1 || {
	echo "generate-docs: tofu not found on PATH (https://opentofu.org/docs/intro/install/)" >&2
	exit 1
}

repo_root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

mkdir -p "$work/bin"
go build -o "$work/bin/terraform-provider-slack" "$repo_root"

cat >"$work/tofurc" <<EOF
provider_installation {
  dev_overrides {
    "registry.terraform.io/hashicorp/slack" = "$work/bin"
  }
  direct {}
}
EOF

cat >"$work/main.tf" <<'EOF'
terraform {
  required_providers {
    slack = {
      source = "registry.terraform.io/hashicorp/slack"
    }
  }
}
EOF

# dev_overrides bypasses the registry, so no `tofu init` is needed here.
(cd "$work" && TF_CLI_CONFIG_FILE="$work/tofurc" tofu providers schema -json) >"$work/schema.json"

cd "$repo_root/tools"
go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate \
	--provider-dir .. -provider-name slack --providers-schema "$work/schema.json"
