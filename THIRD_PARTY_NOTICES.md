# Third-party notices

RFSPager's AGPL-3.0-only license covers project-authored code. It does not
replace licenses on third-party components or grant rights to others' marks.

- Bundled **Lato** and **Poppins** fonts use SIL Open Font License 1.1. See
  [font copyright/source notices](apps/mobile/assets/fonts/FONT-NOTICES.md)
  and the [complete OFL text](apps/mobile/assets/fonts/OFL-1.1.txt).
- The embedded **Apple Root CA – G3** is a public verification certificate
  from [Apple PKI](https://www.apple.com/certificateauthority/), not a private
  signing key. It is used to verify App Store transaction signatures.
- Go module dependencies are pinned in go.mod/go.sum; JavaScript dependencies
  in the web npm and mobile pnpm lockfiles. Their package LICENSE/NOTICE files
  remain applicable and must accompany redistributed components as required.
- App Store/Google Play badges and third-party service names are trademarks
  of their respective owners. They identify services; they do not imply
  affiliation or endorsement. Forks must use their own app listings and avoid
  presenting themselves as the official RFSPager deployment.
- Previously bundled, unused Lottie animations with unknown redistribution
  provenance were removed. They are not offered under the project license.

When adding external assets, record the original source, author, license and
required attribution in the same change. Do not assume downloadable assets
are licensed for redistribution. Review existing project graphics against
original design/source records before publishing a branded fork.
