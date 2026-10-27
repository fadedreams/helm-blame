# helm-blame

[![test](https://github.com/fadedreams/helm-blame/actions/workflows/test.yml/badge.svg)](https://github.com/fadedreams/helm-blame/actions/workflows/test.yml)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

`git blame`, but for Helm values. See when each value of a release changed, in which revision, and what it changed from and to.

```console
$ helm blame demo replicaCount
replicaCount
  REV  UPDATED              STATUS      CHART          CHANGE
  1    2026-10-27 06:00:23  superseded  nginx-25.2.13  1  (first revision seen)
  2    2026-10-27 06:00:28  superseded  nginx-25.2.13  1 → 3
  3    2026-10-27 06:00:34  deployed    nginx-25.2.13  3 → 5
```

## Why

`helm history` tells you that a release was upgraded, and `helm get values --revision N` shows the values of one revision. Neither answers "when did `image.tag` change, and who set it to this?" `helm blame` compares the values of every revision with the one before it and shows the history per key.

## Requirements

- Helm 3 or 4
- Linux or macOS (on Windows, use WSL)
- Access to the cluster that holds the release, the same as for `helm history`

## Install

```bash
helm plugin install https://github.com/fadedreams/helm-blame --verify=false
```

The install hook downloads a prebuilt binary for your OS and architecture (linux/darwin, amd64/arm64) from the GitHub Releases page. It needs `curl` or `wget`.

Update with:

```bash
helm plugin update blame
```

Uninstall with:

```bash
helm plugin uninstall blame
```

## Usage

```
helm blame RELEASE [KEY] [flags]
```

`KEY` is a value key such as `replicaCount` or `image.tag`. A prefix such as `image` shows everything under it (`image.repository`, `image.tag`, ...) but does not match `imagePullPolicy`. Leave `KEY` out to list every key that ever changed.

### Examples

```bash
# History of one key
helm blame myapp replicaCount

# Everything under a prefix
helm blame myapp image

# Every key that changed at least once
helm blame myapp

# Also list keys that never changed
helm blame myapp --include-unchanged

# A release in another namespace
helm blame myapp image.tag -n production

# Machine-readable output
helm blame myapp replicaCount -o json

# Include chart default values, not only the ones you supplied
helm blame myapp resources --computed
```

A key name that contains a literal dot, such as an annotation, is written with a backslash: `annotations.example\.com/team`.

### Flags

| Flag | Description |
| --- | --- |
| `-n`, `--namespace` | Namespace of the release |
| `--kube-context` | Kubeconfig context to use |
| `--kubeconfig` | Path to the kubeconfig file |
| `--computed` | Include chart default values (like `helm get values --all`) |
| `--include-unchanged` | With no `KEY`, also list keys that never changed |
| `--show-secrets` | Do not mask values of keys that look like secrets |
| `-o`, `--output` | Output format: `table` (default) or `json` |
| `--version` | Print the version and exit |

### Change kinds

| Kind | Meaning |
| --- | --- |
| `initial` | The key as it was in the first revision |
| `added` | The key did not exist in the previous revision |
| `changed` | The value differs from the previous revision |
| `removed` | The key existed in the previous revision and is gone |

A key set to `null` is different from a key that is not set, and `1`, `1.0` and `true` count as different values.

### Secrets

Values of keys whose name contains `password`, `passwd`, `secret`, `token`, `api key`, `private key` or `credential` are shown as `<redacted>`. Use `--show-secrets` to see them. The masking is based on key names only, so check the output before pasting it anywhere public.

### JSON output

```bash
helm blame myapp replicaCount -o json
```

```json
[
  {
    "key": "replicaCount",
    "events": [
      {
        "revision": 1,
        "updated": "2026-10-27T06:00:23.794626208Z",
        "status": "superseded",
        "chart": "nginx-25.2.13",
        "kind": "initial",
        "old": null,
        "new": 1
      }
    ]
  }
]
```

## How it works

The plugin runs `helm history -o json` and then `helm get values --revision N -o json` for each revision. It flattens the nested values into `a.b.c` keys and compares each revision with the one before it. It does not read Kubernetes Secrets directly and has no dependencies beyond the Go standard library.

Note that Helm only keeps a limited number of revisions (10 by default, see `--history-max`), so older changes may no longer be available.

## Development

```bash
git clone https://github.com/fadedreams/helm-blame
cd helm-blame

gofmt -l .
go vet ./...
go test ./...

go build -o bin/helm-blame .
HELM_BLAME_SKIP_DOWNLOAD=1 helm plugin install .
```

Setting `HELM_BLAME_SKIP_DOWNLOAD=1` makes the install hook skip the binary download, so the plugin uses the `bin/helm-blame` you just built.

### Releasing

The version in `plugin.yaml` must match the git tag, because the install script builds its download URL from it (`version: "0.1.0"` goes with the tag `v0.1.0`). Pushing a `v*` tag starts the release workflow, which builds the binaries with GoReleaser. Do not re-tag a published version; publish a new one instead.

## Contributing

Issues and pull requests are welcome. Please run `gofmt`, `go vet` and `go test ./...` before opening a pull request.

## License

Copyright 2026 fadedreams

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this project except in compliance with the License. You may obtain a copy of the License at

<http://www.apache.org/licenses/LICENSE-2.0>

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the [LICENSE](LICENSE) file for the specific language governing permissions and limitations under the License.
