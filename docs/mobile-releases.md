# Releasing RFSPager

How a change gets from `main` to the App Store, Google Play and the production
server. Everything below has been run for real (5.3.x, 5.4.0). If you hit
something new, add it to [Troubleshooting](#troubleshooting).

```
commit to main ─► release-please PR ─► merge ─► tag vX.Y.Z
                                                  ├─► server / web / receiver images (CI)
                                                  └─► mobile-release.yml (if apps/mobile changed)
                                                        ├─► iOS  → TestFlight
                                                        └─► Android → Play internal track
scripts/submit-to-stores.sh X.Y.Z ─► App Store review (auto-release) + Play production
deploy the Go server to apps01 by hand (below)
```

## 1. Land changes on main

- Use Conventional Commits (`fix(mobile): …`, `feat(api): …`). release-please
  builds the version and changelog from them: `feat` bumps the minor, `fix`
  the patch. `chore`, `docs`, `ci` and `test` don't appear in the changelog.
- Ship from `main` only. If the checkout is on a feature branch with
  unreleased work, do the fix in a worktree:
  `git worktree add -b fix/x /tmp/rfs-x origin/main`.

## 2. Merge the release PR

release-please keeps one open PR, `chore(main): release X.Y.Z`, updated on
every push to main.

```sh
gh pr list --search "release in:title" --state open
gh pr checks <n> --watch            # all five must pass
gh pr merge <n> --squash
```

If `server (go)` fails on **govulncheck**, a new advisory was published. Bump
the Go version (go.mod, `.github/workflows/ci.yml`, both Dockerfiles) or the
module it names, re-run `govulncheck ./...` locally, and push. Don't skip the
check: these are the production server's HTTP and TLS stacks.

## 3. Builds

Merging tags `vX.Y.Z`. The tag builds the server, web and receiver images, and
release-please dispatches `mobile-release.yml` **only if `apps/mobile`
changed** since the previous release. To build by hand:

```sh
gh workflow run mobile-release.yml --ref main -f tag=vX.Y.Z
gh run watch $(gh run list -w mobile-release.yml -L 1 --json databaseId -q '.[0].databaseId')
```

- iOS takes about 12 minutes and ends with "Successfully uploaded the new
  binary" (TestFlight).
- Android takes about 40 minutes and ends with "Successfully finished the
  upload to Google Play" (internal track), then the 16 KB alignment check.
- **Build number** = 500000000 + major×10⁷ + minor×10⁵ + patch×10³ + revision
  (5.4.0 → 550400000). Re-running the same tag needs `build_revision` bumped
  (both stores refuse a reused build number).
- The workflow inputs are `tag`, `platform` (`ios` / `android` / `both`),
  `upload`, `play_track` and `build_revision`.

## 4. Submit to the stores

Write the notes, then run the script. It takes the builds already uploaded in
step 3. It doesn't rebuild.

```sh
$EDITOR apps/mobile/fastlane/release-notes/X.Y.Z.txt   # plain text, ≤ 500 chars, commit it
apps/mobile/scripts/submit-to-stores.sh X.Y.Z          # or: … X.Y.Z ios|android
```

What the script does:

- **iOS** (`fastlane ios submit_for_review`): creates the App Store version,
  attaches the build, and sets the notes for `default` and `en-AU`. Then it
  submits for review with **automatic release** once Apple approves, with no
  phased release.
- **Android** (`fastlane android promote_to_production`): downloads the AAB CI
  built and refuses to continue unless it has x86_64 libraries and passes the
  16 KB alignment check. Then it promotes the build from internal to
  **production at 100%**, with the notes as the en-AU changelog.

Credentials are never in the repo. The script reads them from:

| What | Default location | Override |
|---|---|---|
| App Store Connect API key `4R8T7V2Z9H` (issuer `69a6de73-…`) | `~/code/boringapps/.secrets/AuthKey_4R8T7V2Z9H.p8` | `ASC_KEY_PATH` |
| Play service account | copied from `apps01:/opt/rfspager-go/google-play.json` into a temp dir, deleted on exit | `PLAY_JSON_KEY_PATH` |

CI's own signing secrets are listed in [ci-secrets.md](ci-secrets.md).

### Checking store state

```sh
ruby -e 'require "spaceship"
Spaceship::ConnectAPI.token = Spaceship::ConnectAPI::Token.create(key_id: "4R8T7V2Z9H",
  issuer_id: "69a6de73-b975-47e3-e053-5b8c7c11a4d1",
  filepath: File.expand_path("~/code/boringapps/.secrets/AuthKey_4R8T7V2Z9H.p8"))
app = Spaceship::ConnectAPI::App.find("io.devlan.rfspager")
app.get_app_store_versions(filter: { platform: "IOS" }).first(3).each { |v|
  puts "#{v.version_string}: #{v.app_store_state} (#{v.release_type})" }'

# Play: version codes on each track (needs the key, as above)
fastlane run google_play_track_version_codes package_name:io.devlan.rfspager \
  track:production json_key:/path/to/play.json
```

iOS goes `WAITING_FOR_REVIEW` → `IN_REVIEW` → `READY_FOR_SALE`, released
automatically on approval. Review usually takes under a day.

## 5. Deploy the server

Production runs the Go server as systemd unit `rfspager-go` on apps01
(`$RFSPAGER_PROD_HOST`, arm64), not from the CI image. Build from the tagged
`main`:

```sh
V=$(git describe --tags --always) C=$(git rev-parse --short HEAD)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath \
  -ldflags="-s -w -X main.version=$V -X main.commit=$C" -o /tmp/server ./cmd/server
scp /tmp/server "$RFSPAGER_PROD_HOST":/tmp/server.new
ssh "$RFSPAGER_PROD_HOST" 'cp -p /opt/rfspager-go/server /opt/rfspager-go/server.prev &&
  install -m 755 /tmp/server.new /opt/rfspager-go/server && systemctl restart rfspager-go &&
  sleep 3 && curl -s http://127.0.0.1:8080/health'
```

Migrations run on start-up. To roll back, put `server.prev` back and restart.
Check `journalctl -u rfspager-go` for `level":"ERROR` after the restart. A
FRNSW GeoRSS 403 is a known, unrelated error.

## Troubleshooting

Problems already hit, and what fixed them:

- **App Store submission refused: missing age-rating answers.** Apple added
  questions to the age-rating questionnaire. Answer them in App Store Connect
  (App Information → Age Rating): advertising **yes**, everything else
  none/no. Then re-run the iOS half. To see Apple's exact reasons, create a
  `reviewSubmissionItems` entry through the App Store Connect API and read
  `errors[].meta.associatedErrors`.
- **Encryption/IDFA questions** are answered by the lane
  (`export_compliance_uses_encryption: false`, `add_id_info_uses_idfa: false`).
- **Release tags didn't trigger workflows.** Tags pushed with `GITHUB_TOKEN`
  start no workflows. release-please runs as the GitHub App
  (`BOT_APP_ID` / `BOT_APP_PRIVATE_KEY` org secrets) for that reason.
- **Android: `SoLoaderDSONotFoundError` on Chromebooks and Google's test
  devices.** The bundle was ARM-only. Keep x86_64 in `ANDROID_ABIS` (the
  script refuses to promote without it).
- **Play rejected the bundle: 16 KB page size.** A native library wasn't 16 KB
  aligned (`libconceal.so` from react-native-keychain, since removed). The
  check runs in CI and again in the script:
  `python3 apps/mobile/scripts/check-16kb-alignment.py app-release.aab`.
- **Android build failures in CI.** Each had its own fix:
  - Corrupt NDK download: the NDK is now cached and retried.
  - Gradle Metaspace OOM: the JVM args were raised.
  - Runner out of disk: a cleanup step removes unused toolchains.
  - `gradle.properties` missing a trailing newline: an echo goes first.
  - All of these live in `mobile-release.yml`, so read the failing step there
    before re-running.
- **Re-running a release.** Bump `build_revision` and pass the same revision as
  the script's third argument, e.g. `submit-to-stores.sh 5.4.0 both 1`.
