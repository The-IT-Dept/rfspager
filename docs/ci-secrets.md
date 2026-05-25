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

---

## Secret inventory at a glance

| Workflow                  | Secret name                            | What it is                                                |
| ------------------------- | -------------------------------------- | --------------------------------------------------------- |
| **mobile-release** (iOS)  | `EXPO_TOKEN`                           | Expo personal access token                                |
|                           | `IOS_GOOGLE_SERVICE_INFO_PLIST`        | base64 of `GoogleService-Info.plist`                      |
|                           | `APPLE_ASC_KEY_P8`                     | base64 of the App Store Connect API `.p8` key             |
|                           | `APPLE_ASC_KEY_ID`                     | 10-char key ID (e.g. `ABCDE12345`)                        |
|                           | `APPLE_ASC_ISSUER_ID`                  | UUID of your ASC issuer                                   |
| **mobile-release** (Android) | `ANDROID_GOOGLE_SERVICES_JSON`      | base64 of `google-services.json`                          |
|                           | `ANDROID_UPLOAD_KEYSTORE`              | base64 of the upload keystore (`.jks` or `.keystore`)     |
|                           | `ANDROID_KEYSTORE_PASSWORD`            | keystore password                                         |
|                           | `ANDROID_KEY_ALIAS`                    | alias inside the keystore                                 |
|                           | `ANDROID_KEY_PASSWORD`                 | password for that alias                                   |
|                           | `ANDROID_PLAY_SERVICE_ACCOUNT_JSON`    | base64 of Google Play service-account JSON                |
| **server-release**, **web-release** | *(none — uses `GITHUB_TOKEN`)* | GHCR push uses the built-in token                  |
| **release-please**        | *(none — uses `GITHUB_TOKEN`)*         | Default permissions are enough                            |

---

## 1. Expo

### `EXPO_TOKEN`

Used by `eas-cli` in CI to authenticate against your Expo account / EAS
project.

1. Sign in at <https://expo.dev/>.
2. **Account settings → Access tokens → Create token.** Name it
   `github-actions-rfspager`, copy the value (you only see it once).
3. Add as the GitHub secret `EXPO_TOKEN`.

The Expo project needs to exist and be linked from `apps/mobile/app.json`
(its `extra.eas.projectId`). If it doesn't yet, run:

```bash
cd apps/mobile
eas init           # creates the project, updates app.json
```

…locally, commit the change, then push.

---

## 2. iOS — App Store Connect API key

We use ASC API keys (not the legacy Apple ID + app-specific password)
because they're machine-friendly and don't need 2FA.

1. Go to
   <https://appstoreconnect.apple.com/access/integrations/api> → **Keys**
   tab → **Generate API Key**.
2. **Access:** "App Manager" is sufficient for `eas submit`.
3. Download the `.p8` (you only get it once). Note the **Key ID** and
   **Issuer ID** shown on the same page.
4. Encode and store:

   ```bash
   base64 -i AuthKey_ABCDE12345.p8 | pbcopy
   ```

5. Add three GitHub secrets:
   - `APPLE_ASC_KEY_P8`        — paste the base64 string
   - `APPLE_ASC_KEY_ID`        — `ABCDE12345`
   - `APPLE_ASC_ISSUER_ID`     — the UUID shown above the keys table

> The workflow materialises the `.p8` at `apps/mobile/.secrets/asc.p8` and
> sets `ASC_KEY_PATH`, `ASC_KEY_ID`, `ASC_ISSUER_ID` env vars — these are
> the names `eas submit` reads.

### iOS signing — local builds, no Apple in the loop

Because we use `eas build --local`, EAS handles credentials by **fetching
the existing signing identity from EAS' credential store** the first time
it runs, then caching it on the runner for the build. So you must seed it
once interactively from your dev machine:

```bash
cd apps/mobile
eas credentials                  # interactive
#   → iOS → release profile → Set up Distribution Certificate
#   → iOS → release profile → Set up Provisioning Profile
```

After that, CI runs `eas build --local --non-interactive` and EAS uses
`EXPO_TOKEN` to pull the credentials it already has on file. You don't
need a separate `.p12` in GitHub.

If you'd rather avoid the EAS credential store and provide cert + profile
yourself, set `credentialsSource: "local"` in `apps/mobile/credentials.json`
and add two more secrets (`IOS_DIST_CERT_P12`, `IOS_PROVISIONING_PROFILE`);
the workflow has comments showing where they'd plug in.

### `IOS_GOOGLE_SERVICE_INFO_PLIST`

Firebase iOS config. Download from the Firebase console:

1. <https://console.firebase.google.com/> → project → ⚙ → Project settings
   → **Your apps** → iOS app `io.devlan.rfspager` → **GoogleService-Info.plist**.
2. `base64 -i GoogleService-Info.plist | pbcopy`
3. Store as `IOS_GOOGLE_SERVICE_INFO_PLIST`.

---

## 3. Android — upload keystore + Play Console

### `ANDROID_UPLOAD_KEYSTORE` (+ passwords)

You only generate this **once** for the app's lifetime. Lose it and you
can never push another update to Google Play without going through their
key-reset process.

```bash
keytool -genkey -v \
  -keystore upload.keystore \
  -alias upload \
  -keyalg RSA -keysize 2048 -validity 10000 \
  -storepass <STRONG_PASS> -keypass <STRONG_PASS> \
  -dname "CN=RFSPager, OU=The IT Dept, O=The IT Dept, L=Sydney, ST=NSW, C=AU"
```

Then:

```bash
base64 -i upload.keystore | pbcopy
```

Add four GitHub secrets:

- `ANDROID_UPLOAD_KEYSTORE`     — base64
- `ANDROID_KEYSTORE_PASSWORD`   — `-storepass` value
- `ANDROID_KEY_ALIAS`           — `upload`
- `ANDROID_KEY_PASSWORD`        — `-keypass` value

**Back up `upload.keystore` somewhere durable (1Password vault).**

### `ANDROID_PLAY_SERVICE_ACCOUNT_JSON`

Used by `eas submit` to upload to Play Console.

1. Google Cloud Console → IAM → **Service Accounts** → Create service account.
   Name it `rfspager-play-publish`.
2. Skip role grants here.
3. Service account → **Keys** → Add key → JSON → download.
4. Play Console → **Users and permissions** → Invite → paste the service
   account email → grant **Admin (all permissions)** scoped to the RFSPager
   app (Release manager is the minimum needed for upload + release; Admin
   is the easiest).
5. Wait ~5 minutes for permissions to propagate.
6. Encode and store:

   ```bash
   base64 -i rfspager-play-publish.json | pbcopy
   ```

   …as `ANDROID_PLAY_SERVICE_ACCOUNT_JSON`.

### `ANDROID_GOOGLE_SERVICES_JSON`

Firebase Android config:

1. Firebase console → project → ⚙ → Project settings → **Your apps** →
   Android app `io.devlan.rfspager` → **google-services.json**.
2. `base64 -i google-services.json | pbcopy`
3. Store as `ANDROID_GOOGLE_SERVICES_JSON`.

---

## 4. Server + web Docker images

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

## 5. Cutting a release

```bash
# 1. Make sure release-please has merged its version-bump PR for the
#    component you want to ship.

# 2. Push the matching tag. release-please usually creates the tag for
#    you on merge of the bump PR; if not, do it manually:
git tag server-v0.2.0  && git push origin server-v0.2.0
git tag web-v0.2.0     && git push origin web-v0.2.0
git tag mobile-v4.2.0  && git push origin mobile-v4.2.0
```

Watch the run under the **Actions** tab. The mobile job takes ~30–45 min
because the IPA/AAB are produced on the runner (no EAS cloud).

### Dry-run mobile build without submitting

```text
Actions → mobile-release → Run workflow
  platform = ios        (or android, or all)
  submit   = false
```

This produces a build artifact you can download and side-load, without
touching the App Store / Play Store.

---

## 6. Local secrets layout

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

## Troubleshooting

- **`eas build` exits with "credentials not set up"** — run `eas
  credentials` from your dev box once to seed the EAS credential store
  (see iOS section above). The runner needs nothing else as long as it
  has `EXPO_TOKEN`.
- **`eas submit` fails on iOS with "authentication failed"** — check that
  the ASC API key still has "App Manager" access and hasn't been revoked.
  Keys expire after 1 year by default.
- **Play submit fails with "the caller does not have permission"** — the
  service-account invitation in Play Console hasn't been accepted, or it
  was added at the *organization* level instead of being explicitly
  granted access to the RFSPager app. Re-invite scoped to the app.
- **Docker push fails with 403** — repo → Settings → Actions → General →
  "Workflow permissions" must be **Read and write**. Default for new
  repos is read-only.
