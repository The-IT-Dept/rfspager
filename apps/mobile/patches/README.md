# Dependency patches

## `decode-uri-component@0.2.2`

`expo-router@57.0.22` requires CommonJS `query-string@7.1.3`, which in turn
requires `decode-uri-component@0.2.2`. Registry advisory `1147955` identifies
that version as vulnerable to exponential decoding of malformed percent-encoded
input. The latest compatible Expo SDK 57 release still carries that dependency.

`decode-uri-component@0.2.2.patch` backports the upstream linear decoder from
v0.5.0 (`d467efa37af15710dfc8af061f026a38dc44f67e`) plus the v0.5.1 single-pass
fix for GHSA-qr28-fpjm-wq7f (quadratic replacement of percent-encoded runs; the
UTF-16 BOM pairs and truncated `%C2` move into the linear decoder), while retaining
the CommonJS export and 0.2.x `+`-to-space behavior required by Query String 7.
The patch is applied by `pnpm.patchedDependencies` and is verified by a forced
frozen install plus a Query String 7 malformed-input child-process regression.

The package registry continues to report the version string `0.2.2`, so `pnpm
audit` reports the advisories this patch fixes. The original one is moderate
and stays visible; GHSA-qr28-fpjm-wq7f is high and is listed in
`pnpm.auditConfig.ignoreGhsas` only because the patch carries the upstream
0.5.1 fix (output matches upstream 0.5.1 on malformed input). Do not ignore any
other advisory on this package without the same evidence. Remove this patch when Expo Router upgrades
to a release that no longer depends on the affected CommonJS decoder.

The upstream MIT notice is preserved in
`decode-uri-component@0.2.2.MIT.txt` and the dependency's original license file
remains in installed package contents.
