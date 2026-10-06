# CI / CD secrets & certificates

This is the operator's runbook for the RFSPager release pipeline. It lists
**every** secret each workflow needs, what it's for, and exactly how to mint
it. Once a secret is in GitHub Actions, the workflows in
`.github/workflows/` will pick it up — they're already wired.

> **Convention.** Anything that is a binary or multi-line file (e.g. a `.p8`,
> a `.keystore`, a `GoogleService-Info.plist`) is **stored base64-encoded** in
> the GitHub secret. The workflow does `base64 -d` to materialise the file at
> a known path inside the runner. To produce the value locally:
>
> ```bash
> base64 -i ./my-file.p8 | pbcopy      # macOS
> base64 -w 0 my-file.p8 | xclip       # linux
> ```

GitHub UI: **Repo → Settings → Secrets and variables → Actions → New
repository secret**.

> Looking for **runtime** secrets (the ones the server reads at boot,
> e.g. APNs, Firebase, IAP, Stripe)? Those live in `./.env` on the
> server host — see `.env.example` for the full list. CI doesn't need
> them. For wiring App Store + Play Store subscription **webhooks** to
> the server, see [`iap-webhooks.md`](./iap-webhooks.md).

---

## Secret inventory at a glance

The mobile apps are built on GitHub-hosted runners from the native projects
`expo prebuild` generates, signed with the keys below and uploaded with
fastlane (`apps/mobile/fastlane/Fastfile`). No Expo build or submit service
and no `EXPO_TOKEN` is involved.

| Kind | Name | What it is |
| ---- | ---- | ---------- |
| variable | `EXPO_PUBLIC_API_URL` | Backend origin baked into the app (`https://rfspager.app`) |
| variable | `APPLE_TEAM_ID` | Apple Developer team (`W62CSN3S5H`) |
| variable | `EXPO_PUBLIC_FACEBOOK_APP_ID`, `EXPO_PUBLIC_SOURCE_URL` | Client identifiers |
| secret | `EXPO_PUBLIC_GOOGLE_MAPS_API_KEY`, `EXPO_PUBLIC_FACEBOOK_CLIENT_TOKEN` | Client keys (bundled into the app; restrict them provider-side) |
| secret | `IOS_GOOGLE_SERVICE_INFO_PLIST` | base64 of the Firebase iOS config |
| secret | `ANDROID_GOOGLE_SERVICES_JSON` | base64 of the Firebase Android config |
| secret | `IOS_DIST_CERT_P12`, `IOS_DIST_CERT_PASSWORD` | base64 of the Apple Distribution certificate + its password |
| secret | `IOS_PROVISIONING_PROFILE` | base64 of the App Store provisioning profile for `io.devlan.rfspager` |
| secret | `APPLE_ASC_KEY_P8`, `APPLE_ASC_KEY_ID`, `APPLE_ASC_ISSUER_ID` | App Store Connect API key used to upload to TestFlight |
| secret | `ANDROID_UPLOAD_KEYSTORE`, `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD` | Play upload key |
| secret | `ANDROID_PLAY_SERVICE_ACCOUNT_JSON` | base64 of a service-account key with release rights in Play Console |

Server, web and receiver images need nothing beyond `GITHUB_TOKEN`.

Missing signing secrets do not fail the build: iOS then archives unsigned and
Android signs with a throwaway key, and nothing is uploaded. That is how the
pipeline is proven before the keys exist.

---

## 1. iOS

### App Store Connect API key (`APPLE_ASC_KEY_*`)

App Store Connect -> Users and Access -> Integrations -> App Store Connect API
-> Team Keys -> **+**. **Admin** access is needed to create the distribution
certificate and profile below; uploading alone needs App Manager. Download the
`.p8` (only once), note the Key ID; the Issuer ID is shown above the table.
This is a different key from the In-App Purchase key the server uses.

### Distribution certificate and profile (`IOS_DIST_CERT_*`, `IOS_PROVISIONING_PROFILE`)

Create once with the API key, then store in GitHub:

1. `openssl` an EC/RSA key and CSR, create an **Apple Distribution**
   certificate from it (Certificates API or developer.apple.com), and export
   key + certificate as a password-protected `.p12`.
2. Create an **App Store** provisioning profile for `io.devlan.rfspager` using
   that certificate. The App ID needs Push Notifications, Sign in with Apple
   and Associated Domains, which the app's entitlements use.
3. `base64 -i dist.p12 | gh secret set IOS_DIST_CERT_P12`, likewise
   `IOS_PROVISIONING_PROFILE`, and set `IOS_DIST_CERT_PASSWORD`.

Distribution certificates last a year: renew both before expiry. Apple allows
only a few per team, so revoke the old one once the new one is in.

### `IOS_GOOGLE_SERVICE_INFO_PLIST`

Firebase console -> project `rfspager-e1e40` -> iOS app `io.devlan.rfspager`
-> GoogleService-Info.plist, then `base64 -i GoogleService-Info.plist`.

---

## 2. Android

### Upload key (`ANDROID_UPLOAD_KEYSTORE` + passwords)

The app uses Play App Signing, so CI only holds the *upload* key. If the
existing one is lost: Play Console -> Test and release -> App integrity -> App
signing -> **Request upload key reset**, generate a new key with
`keytool -genkeypair -keystore upload.keystore -alias upload -keyalg RSA
-keysize 2048 -validity 10000`, upload its certificate
(`keytool -export -rfc -keystore upload.keystore -alias upload -file
upload_certificate.pem`) and wait for Google's approval. Back the keystore up
somewhere durable; losing it means another reset.

### `ANDROID_PLAY_SERVICE_ACCOUNT_JSON`

Google Cloud (project `rfspager-e1e40`, with the Google Play Android Developer
API enabled) -> IAM -> Service Accounts -> create `rfspager-play-publish`, add a
JSON key. Play Console -> Users and permissions -> invite its email with
release permissions for the RFSPager app. Keep it separate from the server's
`rfspager-go` account so CI never holds the key production uses.

### `ANDROID_GOOGLE_SERVICES_JSON`

Firebase console -> Android app `io.devlan.rfspager` -> google-services.json,
then `base64 -i google-services.json`.

---

## 3. Server, web and receiver images

These workflows push to **GitHub Container Registry** under the repo owner
namespace (`ghcr.io/the-it-dept/...`). They authenticate with the built-in
`GITHUB_TOKEN` — no extra secrets needed.

To pull the image elsewhere, the consumer needs read access on the GHCR
package (Settings → Packages → package → Manage access).

If you want to push to a different registry (Docker Hub, ECR, etc.), the
swap is straightforward:

```yaml
- uses: docker/login-action@v3
  with:
    registry: <host>
    username: ${{ secrets.REGISTRY_USERNAME }}
    password: ${{ secrets.REGISTRY_TOKEN }}
```

…and update the `images:` line in the metadata step.

---

---

## 4. Cutting a release

Merge the release-please PR. release-please tags `vX.Y.Z`, publishes the
GitHub release, and its `build-release` job starts `server-release`,
`web-release` and `receiver-release` for the tag, plus `mobile-release` when
anything under `apps/mobile` changed since the previous release.

The app's store version is `apps/mobile/package.json` (bumped by
release-please); the build number is derived from it in `app.config.js`
(5.3.1 -> 550301000), always above the old app's last build (530040100).

Mobile builds land in **TestFlight** and the **Play internal track**.
Promoting to the public stores is done by hand in App Store Connect and Play
Console.

### Manual runs

```text
Actions -> mobile-release -> Run workflow
  tag            = v5.3.1 (or a branch, e.g. main, for a test build)
  platform       = all | ios | android
  upload         = false for a build-only dry run
  play_track     = internal | alpha | beta
  build_revision = bump (1, 2, ...) to re-upload the same version
```

The image workflows take the same `tag` input.

---

## 5. Local secrets layout

For local dev the same materials live in:

```
./.env                  — Go server config (DATABASE_URL, JWT_SECRET, …)
./.secrets/             — APNs .p8, Firebase admin JSON, etc.
apps/mobile/.env        — EXPO_PUBLIC_* overrides
apps/mobile/GoogleService-Info.plist
apps/mobile/google-services.json
```

All of these are listed in the root `.gitignore`. **Never commit them.**

---

---

## Troubleshooting

- **TestFlight upload "authentication failed"**: the ASC key was revoked or
  lacks access; keys do not expire but can be revoked in App Store Connect.
- **"No profile matching" / code signing errors**: the profile does not
  match the certificate in `IOS_DIST_CERT_P12`, or the App ID lacks a
  capability the entitlements need. Regenerate the profile.
- **Play upload "the caller does not have permission"**: the service account
  was not granted access to the RFSPager app in Play Console, or the Play
  Developer API is not enabled in its Google Cloud project.
- **Play rejects the version code**: a build with that number exists; re-run
  with a higher `build_revision`.
- **Docker push fails with 403**: Settings -> Actions -> General ->
  Workflow permissions must be Read and write.
