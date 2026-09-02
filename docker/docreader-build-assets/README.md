# DocReader build assets

Optional architecture-specific binaries can be placed here to support offline
or restricted-network image builds. The Dockerfile falls back to downloading
the release artifact when a matching local file is not present.

Supported filename pattern:

- `grpc_health_probe-linux-<arch>` (v0.4.24)

The amd64 v0.4.24 release used for production was verified as:

```text
sha256:7e564681110ee4563637457b91e42f62f96b79618a835bb05ae2305acdcc3db0
```
