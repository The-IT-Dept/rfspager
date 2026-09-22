# Dependency patches

## `decode-uri-component@0.2.2`

`expo-router@57.0.22` requires CommonJS `query-string@7.1.3`, which in turn
requires `decode-uri-component@0.2.2`. Registry advisory `1147955` identifies
that version as vulnerable to exponential decoding of malformed percent-encoded
input. The latest compatible Expo SDK 57 release still carries that dependency.

`decode-uri-component@0.2.2.patch` backports the upstream v0.5.0 linear decoder
from tag `v0.5.0` (`d467efa37af15710dfc8af061f026a38dc44f67e`) while retaining
the CommonJS export and 0.2.x `+`-to-space behavior required by Query String 7.
The patch is applied by `pnpm.patchedDependencies` and is verified by a forced
frozen install plus a Query String 7 malformed-input child-process regression.

The package registry continues to report the version string `0.2.2`, so `pnpm
audit` continues to report this one moderate finding. Do not suppress the audit
or treat other findings as patched. Remove this patch when Expo Router upgrades
to a release that no longer depends on the affected CommonJS decoder.

The upstream MIT notice is preserved in
`decode-uri-component@0.2.2.MIT.txt` and the dependency's original license file
remains in installed package contents.
