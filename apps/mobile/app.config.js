// Expo reads this in preference to app.json.
//
// app.json is static, so it cannot point `googleServicesFile` at anything but a
// committed path — and both Firebase config files are gitignored. EAS Build
// only uploads git-tracked files, so a cloud build fails with
// "GoogleService-Info.plist is missing".
//
// EAS file-type environment variables (GOOGLE_SERVICE_INFO_PLIST,
// GOOGLE_SERVICES_JSON) are materialised on the builder and exposed as absolute
// paths. Prefer those when present; fall back to the local files so Metro and
// local builds keep working unchanged.
const appJson = require("./app.json");

module.exports = () => {
  const cfg = appJson.expo;

  const iosPlist = process.env.GOOGLE_SERVICE_INFO_PLIST;
  const androidJson = process.env.GOOGLE_SERVICES_JSON;

  return {
    ...cfg,
    ios: {
      ...cfg.ios,
      ...(iosPlist ? { googleServicesFile: iosPlist } : {}),
    },
    android: {
      ...cfg.android,
      ...(androidJson ? { googleServicesFile: androidJson } : {}),
    },
  };
};
