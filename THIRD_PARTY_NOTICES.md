# Third-party notices

Hody's own code uses only the Go standard library plus `k8s.io/client-go`
(with `k8s.io/api`, `k8s.io/apimachinery` and `sigs.k8s.io/yaml`, which
client-go already depends on). The modules below are compiled into the
`hody` binary (from `go list -deps ./cmd/hody`). Licenses were identified
from the header of each module's LICENSE file in the module cache during
the build; please confirm against the upstream repositories before
redistributing. Some modules carry more than one license text (for example
`go.yaml.in/yaml/v3` and `sigs.k8s.io/yaml` include MIT and Apache-2.0 or
BSD portions); the table lists the first one detected.

The Go standard library is BSD-3-Clause (Copyright The Go Authors).

| Module | Version | License |
|---|---|---|
| github.com/davecgh/go-spew | v1.1.2-0.20180830191138-d8f796af33cc | ISC |
| github.com/emicklei/go-restful/v3 | v3.13.0 | MIT |
| github.com/fxamacker/cbor/v2 | v2.9.1 | MIT |
| github.com/go-logr/logr | v1.4.3 | Apache-2.0 |
| github.com/go-openapi/jsonpointer | v1.0.0 | Apache-2.0 |
| github.com/go-openapi/jsonreference | v1.0.0 | Apache-2.0 |
| github.com/go-openapi/swag/cmdutils | v0.27.1 | Apache-2.0 |
| github.com/go-openapi/swag/conv | v0.27.1 | Apache-2.0 |
| github.com/go-openapi/swag/fileutils | v0.27.1 | Apache-2.0 |
| github.com/go-openapi/swag/jsonutils | v0.27.1 | Apache-2.0 |
| github.com/go-openapi/swag/loading | v0.27.1 | Apache-2.0 |
| github.com/go-openapi/swag/mangling | v0.27.1 | Apache-2.0 |
| github.com/go-openapi/swag/netutils | v0.27.1 | Apache-2.0 |
| github.com/go-openapi/swag/pools | v0.27.1 | Apache-2.0 |
| github.com/go-openapi/swag/stringutils | v0.27.1 | Apache-2.0 |
| github.com/go-openapi/swag/typeutils | v0.27.1 | Apache-2.0 |
| github.com/go-openapi/swag/yamlutils | v0.27.1 | Apache-2.0 |
| github.com/go-openapi/swag | v0.27.1 | Apache-2.0 |
| github.com/google/gnostic-models | v0.7.0 | Apache-2.0 |
| github.com/google/uuid | v1.6.0 | BSD-3-Clause |
| github.com/json-iterator/go | v1.1.12 | MIT |
| github.com/modern-go/concurrent | v0.0.0-20180306012644-bacd9c7ef1dd | Apache-2.0 |
| github.com/modern-go/reflect2 | v1.0.3-0.20250322232337-35a7c28c31ee | Apache-2.0 |
| github.com/munnerz/goautoneg | v0.0.0-20191010083416-a7dc8b61c822 | BSD-3-Clause |
| github.com/spf13/pflag | v1.0.10 | BSD-3-Clause |
| github.com/x448/float16 | v0.8.4 | MIT |
| go.yaml.in/yaml/v2 | v2.4.4 | Apache-2.0 |
| go.yaml.in/yaml/v3 | v3.0.4 | Apache-2.0 |
| golang.org/x/net | v0.57.0 | BSD-3-Clause |
| golang.org/x/oauth2 | v0.36.0 | BSD-3-Clause |
| golang.org/x/sys | v0.47.0 | BSD-3-Clause |
| golang.org/x/term | v0.45.0 | BSD-3-Clause |
| golang.org/x/text | v0.40.0 | BSD-3-Clause |
| golang.org/x/time | v0.15.0 | BSD-3-Clause |
| google.golang.org/protobuf | v1.36.12-0.20260120151049-f2248ac996af | BSD-3-Clause |
| gopkg.in/evanphx/json-patch.v4 | v4.13.0 | BSD-3-Clause |
| gopkg.in/inf.v0 | v0.9.1 | BSD-3-Clause |
| k8s.io/api | v0.37.2 | Apache-2.0 |
| k8s.io/apimachinery | v0.37.2 | Apache-2.0 |
| k8s.io/client-go | v0.37.2 | Apache-2.0 |
| k8s.io/klog/v2 | v2.140.0 | Apache-2.0 |
| k8s.io/kube-openapi | v0.0.0-20260721132016-d427ff9ee9ad | Apache-2.0 |
| k8s.io/utils | v0.0.0-20260626114624-be93311217bd | Apache-2.0 |
| sigs.k8s.io/json | v0.0.0-20250730193827-2d320260d730 | Apache-2.0 |
| sigs.k8s.io/randfill | v1.0.0 | Apache-2.0 |
| sigs.k8s.io/structured-merge-diff/v6 | v6.4.2 | Apache-2.0 |
| sigs.k8s.io/yaml | v1.6.0 | BSD-3-Clause |
